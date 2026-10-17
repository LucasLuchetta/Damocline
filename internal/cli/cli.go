// Package cli implements the small pre-alpha Damocline command-line scaffold.
package cli

import (
	"fmt"
	"io"
)

// Version may be overridden during a future release build.
var Version = "dev"

// Run dispatches the supported commands and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "damocline %s (pre-alpha)\n", Version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %q\nRun 'damocline help' for available commands.\n", args[0])
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Damocline — find the limit before it finds you.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Pre-alpha scaffold. The load-testing engine is not implemented.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage: damocline <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  help      Show this message")
	fmt.Fprintln(w, "  version   Show version information")
}
