package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	bytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, bytes)
	}
	return strings.TrimSpace(string(bytes))
}

func setupLocalGit(t *testing.T) (source, checkout string) {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "upstream.git")
	source = filepath.Join(dir, "source")
	checkout = filepath.Join(dir, "checkout")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "--bare", "--initial-branch=master", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create remote: %v %s", err, out)
	}
	runGit(t, source, "init", "--initial-branch=master")
	runGit(t, source, "config", "user.email", "dev@example.com")
	runGit(t, source, "config", "user.name", "Tester")
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module example.com/project\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "go.mod")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, source, "remote", "add", "origin", remote)
	runGit(t, source, "push", "-u", "origin", "master")
	cmd = exec.Command("git", "clone", "--quiet", remote, checkout)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	return source, checkout
}

func pushChange(t *testing.T, source, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "update")
	runGit(t, source, "push", "origin", "master")
}

func TestCheckApplyAndPreventDirtyOverwrite(t *testing.T) {
	source, clone := setupLocalGit(t)
	up, err := Check(clone)
	if err != nil || up.Available != nil {
		t.Fatalf("unexpected initial state: %+v %v", up, err)
	}
	pushChange(t, source, "version 2\n")
	up, err = Check(clone)
	if err != nil || up.Available == nil {
		t.Fatalf("expected update: %+v %v", up, err)
	}
	if err := Apply(*up.Available); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(clone, "README.md"))
	if string(data) != "version 2\n" {
		t.Fatalf("wrong content: %s", data)
	}
	pushChange(t, source, "version 3\n")
	up, err = Check(clone)
	if err != nil || up.Available == nil {
		t.Fatalf("expected next update: %+v %v", up, err)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("my local edits\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(*up.Available); err == nil {
		t.Fatal("overwrote local edits")
	}
	data, _ = os.ReadFile(filepath.Join(clone, "README.md"))
	if string(data) != "my local edits\n" {
		t.Fatalf("lost local edits: %s", data)
	}
}

func TestFeatureBranchCannotAutoUpdate(t *testing.T) {
	_, clone := setupLocalGit(t)
	runGit(t, clone, "switch", "-c", "feat/custom")
	r, err := Check(clone)
	if err != nil || r.Available != nil || r.Branch != "feat/custom" {
		t.Fatalf("unexpected result: %+v %v", r, err)
	}
}

func TestOptInIsLocalAndReversible(t *testing.T) {
	_, clone := setupLocalGit(t)
	if ReadState(clone).Auto {
		t.Fatal("auto-update should be off by default")
	}
	if err := WriteState(clone, State{Auto: true}); err != nil {
		t.Fatal(err)
	}
	if !ReadState(clone).Auto {
		t.Fatal("auto-update did not activate")
	}
	if err := WriteState(clone, State{Auto: false}); err != nil {
		t.Fatal(err)
	}
	if ReadState(clone).Auto {
		t.Fatal("auto-update did not deactivate")
	}
}

func TestMissingCheckoutSkips(t *testing.T) {
	r, err := Check(t.TempDir())
	if err != nil || r.Available != nil || r.Skipped == "" {
		t.Fatalf("unexpected result: %+v %v", r, err)
	}
}
