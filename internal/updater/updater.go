// Package updater checks for safe fast-forward updates from a local Git checkout.
// It never downloads binaries or overwrites local work.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	checkTimeout = 12 * time.Second
	applyTimeout = 30 * time.Second
	prefsFile = ".damocline/updates.json"
)

type State struct { Auto bool `json:"auto_update"` }
type Available struct { From, To, Root string }
type CheckResult struct {
	Available *Available
	Branch string
	Skipped string
}

func ReadState(root string) State {
	var state State
	data, err := os.ReadFile(filepath.Join(root, prefsFile))
	if err == nil { _ = json.Unmarshal(data, &state) }
	return state
}

func WriteState(root string, state State) error {
	path := filepath.Join(root, prefsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { return err }
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil { return err }
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func IsCheckout(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil { return false }
	_, err := os.Stat(filepath.Join(root, "go.mod"))
	return err == nil
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	output, err := cmd.CombinedOutput()
	if err != nil { return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(output))) }
	return strings.TrimSpace(string(output)), nil
}

// Check only fetches origin/master; it never modifies checked-out source files.
func Check(root string) (CheckResult, error) {
	var result CheckResult
	if !IsCheckout(root) {
		result.Skipped = "not a source checkout"
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	current, err := git(ctx, root, "branch", "--show-current")
	if err != nil { return result, err }
	result.Branch = current
	if current != "master" {
		result.Skipped = "source updates are only offered on master"
		return result, nil
	}
	if _, err := git(ctx, root, "remote", "get-url", "origin"); err != nil { return result, err }
	if _, err := git(ctx, root, "fetch", "--quiet", "--no-tags", "origin", "master"); err != nil { return result, err }
	from, err := git(ctx, root, "rev-parse", "HEAD")
	if err != nil { return result, err }
	to, err := git(ctx, root, "rev-parse", "FETCH_HEAD")
	if err != nil { return result, err }
	if from == to { return result, nil }
	if _, err := git(ctx, root, "merge-base", "--is-ancestor", from, to); err != nil {
		result.Skipped = "the local branch diverged; manual Git review required"
		return result, nil
	}
	result.Available = &Available{From: from, To: to, Root: root}
	return result, nil
}

// Apply revalidates branch, commit and working tree before a fast-forward merge.
func Apply(info Available) error {
	if info.Root == "" || info.From == "" || info.To == "" { return errors.New("invalid update metadata") }
	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()
	current, err := git(ctx, info.Root, "branch", "--show-current")
	if err != nil || current != "master" { return errors.New("checkout is no longer on master; update cancelled") }
	head, err := git(ctx, info.Root, "rev-parse", "HEAD")
	if err != nil || head != info.From { return errors.New("checkout changed since update check; retry") }
	status, err := git(ctx, info.Root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil { return err }
	if status != "" { return errors.New("local changes or untracked files found; review them before updating") }
	if _, err := git(ctx, info.Root, "merge-base", "--is-ancestor", info.From, info.To); err != nil {
		return errors.New("update is not a fast-forward; manual review required")
	}
	_, err = git(ctx, info.Root, "merge", "--ff-only", info.To)
	return err
}
