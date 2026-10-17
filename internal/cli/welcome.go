package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// The Eclipse is an original, monochrome block-art rendering of Damocline's
// sword-and-horizon mark. Keep it compact for PowerShell/CMD and other terminals.
// The welcome screen stays line-oriented and does not enable raw terminal mode.
const eclipseArt = `             ▄▄▄▄▄▄▄▄▄▄▄▄▄
         ▄███████████████████▄
       ▄██████████▀█▀██████████▄
      ███████████  █  ███████████
      ██████████  ▄█▄  ██████████
      ██████████  ▀█▀  ██████████
      ▀▀▀▀▀▀▀▀▀▀   █   ▀▀▀▀▀▀▀▀▀▀

         ▄████████ █ ████████▄
           ▀██████ █ ██████▀
              ▀███ █ ███▀
                 ▀ █ ▀
                   ▼`

func terminalColors(output io.Writer) bool {
	if output != os.Stdout || os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if runtime.GOOS != "windows" {
		return true
	}
	// Modern Windows Terminal, VS Code and ANSI-capable consoles advertise
	// support. Classic conhost is left uncolored rather than showing escapes.
	return os.Getenv("WT_SESSION") != "" ||
		os.Getenv("ANSICON") != "" ||
		os.Getenv("ConEmuANSI") == "ON" ||
		strings.Contains(strings.ToLower(os.Getenv("TERM")), "xterm") ||
		strings.EqualFold(os.Getenv("TERM_PROGRAM"), "vscode")
}

func welcomeArt(out io.Writer, color bool) {
	bold, muted, reset := "", "", ""
	if color {
		bold = "\x1b[1;97m"
		muted = "\x1b[90m"
		reset = "\x1b[0m"
	}
	fmt.Fprintln(out)
	for _, line := range strings.Split(eclipseArt, "\n") {
		fmt.Fprintf(out, "%s%s%s\n", bold, line, reset)
	}
	fmt.Fprintf(out, "\n  %sD A M O C L I N E%s\n", bold, reset)
	fmt.Fprintf(out, "  %sFind the limit before it finds you.%s\n", muted, reset)
	fmt.Fprintf(out, "  %sLOCAL-FIRST HTTP CAPACITY EXPLORER%s\n", muted, reset)
	fmt.Fprintf(out, "\n  %s%s%s\n\n", muted, strings.Repeat("─", 45), reset)
}

func welcomeMenu(w *wizard, hasCurrent bool, color bool) (string, error) {
	fmt.Fprintln(w.out, "  What would you like to do?")
	fmt.Fprintln(w.out)
	options := []struct{ key, label, detail string }{
		{"1", "New load test", "Create a new scenario"},
		{"2", "Saved scenarios", "Select a previous configuration"},
		{"3", "Continue current", "Reuse the active scenario"},
		{"4", "Help", "Commands and documentation"},
		{"0", "Exit", "Leave without generating traffic"},
	}
	for _, option := range options {
		detail := option.detail
		if option.key == "3" && !hasCurrent {
			detail = "No active scenario yet"
		}
		if color && option.key == "1" {
			fmt.Fprintf(w.out, "  \x1b[1;97m> [%s] %-19s\x1b[0m \x1b[90m%s\x1b[0m\n", option.key, option.label, detail)
		} else if color {
			fmt.Fprintf(w.out, "    [%s] %-19s\x1b[90m%s\x1b[0m\n", option.key, option.label, detail)
		} else {
			fmt.Fprintf(w.out, "    [%s] %-19s%s\n", option.key, option.label, detail)
		}
	}
	fmt.Fprintln(w.out)
	for {
		choice, err := w.ask("  Select an option (0-4)", "1")
		if err != nil {
			return "", err
		}
		switch choice {
		case "1":
			return "new", nil
		case "2":
			return "history", nil
		case "3":
			if hasCurrent {
				return "current", nil
			}
			fmt.Fprintln(w.out, "No current scenario. Choose 1 to create a test.")
		case "4":
			return "help", nil
		case "0":
			return "exit", nil
		default:
			fmt.Fprintln(w.out, "Choose 0, 1, 2, 3 or 4.")
		}
	}
}

// welcome is used only for the no-argument CLI entrypoint. Explicit commands
// (run, explore, /start, /history, update) remain script-friendly and unaltered.
func welcome(input io.Reader, output, errOutput io.Writer) int {
	w := wizard{input: bufio.NewReader(input), out: output}
	welcomeArt(output, terminalColors(output))
	for {
		_, scenarioErr := os.Stat(scenarioPath)
		_, preferencesErr := os.Stat(preferencesPath)
		hasCurrent := scenarioErr == nil && preferencesErr == nil
		selected, err := welcomeMenu(&w, hasCurrent, terminalColors(output))
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(output, "\nGoodbye.")
				return 0
			}
			fmt.Fprintf(errOutput, "Menu input error: %v\n", err)
			return 2
		}
		switch selected {
		case "exit":
			fmt.Fprintln(output, "Goodbye. No traffic sent.")
			return 0
		case "help":
			fmt.Fprintln(output)
			help(output)
			fmt.Fprintln(output)
		case "new", "history", "current":
			return startWithModeNoBanner(w.input, output, errOutput, selected)
		}
	}
}
