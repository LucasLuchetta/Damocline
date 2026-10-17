package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/updater"
)

// Interactive starts check for updates; scripted load jobs never contact Git.
func promptUpdate(w *wizard, stderr io.Writer) (bool, int) {
	root, err := os.Getwd()
	if err != nil || !updater.IsCheckout(root) { return false, 0 }
	info, err := updater.Check(root)
	if err != nil {
		fmt.Fprintln(w.out, "Update check unavailable (offline or Git problem); continuing with local source.")
		return false, 0
	}
	if info.Skipped != "" {
		if info.Branch != "" && info.Branch != "master" {
			fmt.Fprintf(w.out, "Development checkout: %s. Automatic updates are available on master.\n", info.Branch)
		}
		return false, 0
	}
	if info.Available == nil { return false, 0 }
	available := *info.Available
	fmt.Fprintf(w.out, "\nDamocline update available: %s -> %s (origin/master)\n", available.From[:8], available.To[:8])
	choice := "y"
	if updater.ReadState(root).Auto {
		fmt.Fprintln(w.out, "Automatic source updates are enabled.")
	} else {
		choice, err = w.choose("Update and restart? (y=once, a=always, n=skip)", "y", "y", "a", "n")
		if err != nil {
			fmt.Fprintln(w.out, "Update skipped. Continuing with the current version.")
			return false, 0
		}
	}
	if choice == "n" { return false, 0 }
	if choice == "a" {
		if err := updater.WriteState(root, updater.State{Auto:true}); err != nil {
			fmt.Fprintf(stderr, "Could not save auto-update preference: %v\n", err)
			return false, 0
		}
	}
	fmt.Fprintln(w.out, "Applying fast-forward source update...")
	if err := updater.Apply(available); err != nil {
		fmt.Fprintf(stderr, "Update safely skipped: %v\n", err)
		return false, 0
	}
	fmt.Fprint(w.out, "Source updated. Starting the new version...\n\n")
	cmd := exec.Command("go", "run", "./cmd/damocline")
	cmd.Dir = filepath.Clean(root)
	cmd.Stdin = os.Stdin
	cmd.Stdout = w.out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) { return true, exit.ExitCode() }
		fmt.Fprintf(stderr, "Could not restart updated source: %v\nRun 'damocline' again.\n", err)
		return true, 2
	}
	return true, 0
}

func updateCmd(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	checkOnly := flags.Bool("check", false, "check without installing")
	auto := flags.Bool("auto", false, "opt in to automatic future updates")
	off := flags.Bool("off", false, "disable automatic updates")
	if err := flags.Parse(args); err != nil { return 2 }
	if len(flags.Args()) != 0 || (*off && (*auto || *checkOnly)) || (*auto && *checkOnly) {
		fmt.Fprintln(stderr, "usage: damocline update [--check | --auto | --off]")
		return 2
	}
	root, err := os.Getwd()
	if err != nil || !updater.IsCheckout(root) {
		fmt.Fprintln(stderr, "Source updates require a cloned Damocline checkout.")
		return 2
	}
	if *off {
		if err := updater.WriteState(root, updater.State{Auto:false}); err != nil {
			fmt.Fprintf(stderr, "Cannot save update preference: %v\n", err); return 2
		}
		fmt.Fprintln(stdout, "Automatic source updates disabled.")
		return 0
	}
	if *auto {
		if err := updater.WriteState(root, updater.State{Auto:true}); err != nil {
			fmt.Fprintf(stderr, "Cannot save update preference: %v\n", err); return 2
		}
		fmt.Fprintln(stdout, "Automatic source updates enabled for clean fast-forward master changes.")
	}
	info, err := updater.Check(root)
	if err != nil {
		fmt.Fprintf(stderr, "Update check failed: %v\n", err); return 2
	}
	if info.Skipped != "" {
		fmt.Fprintf(stdout, "Update skipped: %s (current branch: %s).\n", info.Skipped, info.Branch)
		return 0
	}
	if info.Available == nil {
		fmt.Fprintln(stdout, "Damocline is up to date with origin/master.")
		return 0
	}
	if *checkOnly {
		fmt.Fprintf(stdout, "Update available: %s -> %s\n", info.Available.From[:8], info.Available.To[:8])
		return 0
	}
	if !updater.ReadState(root).Auto {
		fmt.Fprintf(stdout, "Update available: %s -> %s\nInstall now? [Y/n]: ", info.Available.From[:8], info.Available.To[:8])
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Update cancelled."); return 0
		}
	}
	if err := updater.Apply(*info.Available); err != nil {
		fmt.Fprintf(stderr, "Update safely skipped: %v\n", err); return 2
	}
	fmt.Fprintln(stdout, "Damocline source updated successfully. Run 'damocline' to start the new version.")
	return 0
}
