// Package cli is the command-line interface for the Damocline load-testing engine.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

var Version = "dev"

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		help(stdout)
		return 0
	}
	switch args[0] {
	case "help", "--help", "-h":
		help(stdout)
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "damocline %s (MVP, closed-loop HTTP)\n", Version)
		return 0
	case "run":
		return run(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %q (try 'damocline help')\n", args[0])
		return 2
	}
}
func help(w io.Writer) {
	fmt.Fprint(w, `Damocline — find the limit before it finds you.

Usage:
  damocline run --config scenario.json --authorized [--out reports/run.json]
  damocline run --config scenario.json --dry-run
  damocline version

For read/write steps, add --allow-writes. For non-local plaintext HTTP,
add --allow-insecure-http only when you intentionally accept that risk.
Only test applications you own or are expressly authorized to assess.
Start with low concurrency and request rates. Run 'damocline run --help' for flags.

The MVP runs bounded closed-loop HTTP virtual users. A single run does not
establish your application's maximum sustainable user count.
`)
}
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "path to scenario JSON")
	authorized := fs.Bool("authorized", false, "confirm explicit permission to test the target")
	writes := fs.Bool("allow-writes", false, "allow potentially destructive HTTP methods")
	insecure := fs.Bool("allow-insecure-http", false, "allow plaintext HTTP outside loopback")
	dry := fs.Bool("dry-run", false, "validate and display a sanitized plan without sending traffic")
	out := fs.String("out", "", "write a JSON report (file permissions 0600)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if *filename == "" {
		fmt.Fprintln(stderr, "--config is required")
		return 2
	}
	raw, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	validated, err := config.Parse(raw, config.Options{Authorized: *authorized || *dry, AllowWrites: *writes, AllowInsecureHTTP: *insecure})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Scenario: %s | target: %s://%s | users: %d | duration: %s | max RPS: %d\n", validated.Scenario.Name, validated.Base.Scheme, validated.Base.Host, validated.Scenario.Users, validated.Duration, validated.Scenario.MaxRPS)
	for _, step := range validated.Steps {
		path := step.Path
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		fmt.Fprintf(stdout, "  - %s: %s %s\n", step.Name, step.Method, path)
	}
	if *dry {
		fmt.Fprintln(stdout, "DRY RUN: no requests were sent.")
		return 0
	}
	fmt.Fprintln(stdout, "Running bounded HTTP workload (no redirects followed)...")
	result := engine.Run(context.Background(), validated)
	fmt.Fprintf(stdout, "Result: %s | requests: %d (%d passed, %d failed, %d cancelled) | p95: %.1f ms | error rate: %.2f%% | %.1f req/s\n", result.Outcome, result.Metrics.Total, result.Metrics.Success, result.Metrics.Failures, result.Metrics.Cancelled, result.Metrics.P95MS, result.ErrorsRate*100, float64(result.Metrics.Total)/result.ElapsedSeconds)
	for _, st := range result.Steps {
		fmt.Fprintf(stdout, "  - %s: total=%d failures=%d p95=%.1f ms\n", st.Name, st.Metrics.Total, st.Metrics.Failures, st.Metrics.P95MS)
	}
	fmt.Fprintln(stdout, "Note: PASS means thresholds passed in this run, not a validated sustainable capacity ceiling.")
	if *out != "" {
		if err := report.WriteJSON(*out, result); err != nil {
			fmt.Fprintf(stderr, "report error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Report: %s\n", *out)
	}
	if result.Outcome != "PASS" {
		return 1
	}
	return 0
}
