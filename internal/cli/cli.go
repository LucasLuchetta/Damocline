// Package cli is the command-line interface for the Damocline load-testing engine.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/correlate"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

var Version = "dev"

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return start(os.Stdin,stdout,stderr)
	}
	switch args[0] {
	case "help", "--help", "-h":
		help(stdout)
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "damocline %s (advanced HTTP pre-alpha)\n", Version)
		return 0
	case "start":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: damocline start")
			return 2
		}
		return start(os.Stdin, stdout, stderr)
	case "run":
		return run(args[1:], stdout, stderr)
	case "explore":
		return explore(args[1:], stdout, stderr)
	case "monitor":
		return monitor(args[1:], stdout, stderr)
	case "correlate":
		return correlateCmd(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %q (try 'damocline help')\n", args[0])
		return 2
	}
}
func help(w io.Writer) {
	fmt.Fprint(w, `Damocline — find the limit before it finds you.

Usage:
  damocline        # guided setup and execution
  damocline start  # same guided wizard
  damocline run --config scenario.json --authorized [--out reports/run.json]
  damocline run --config scenario.json --dry-run
  damocline explore --config scenario.json --min-users 2 --max-users 20 --stage-duration 20s --repeats 2 --authorized
  damocline monitor --duration 3m --interval 2s --out reports/host.json
  damocline correlate --load reports/load.json --host reports/host.json --out reports/correlated.json
  damocline version

For read/write steps, add --allow-writes. For non-local plaintext HTTP,
add --allow-insecure-http only when you intentionally accept that risk.
Only test applications you own or are expressly authorized to assess.
Start with low concurrency and request rates. Run 'damocline run --help' for flags.

Damocline runs its own bounded HTTP engine with closed-loop VUs or arrival-rate
mode. Explore repeats candidate VU levels; it does not establish a universal
server limit or include infrastructure telemetry.
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := engine.Run(ctx, validated)
	fmt.Fprintf(stdout, "Result: %s | requests: %d (%d passed, %d failed, %d cancelled) | p95: %.1f ms | error rate: %.2f%% | %.1f req/s\n", result.Outcome, result.Metrics.Total, result.Metrics.Success, result.Metrics.Failures, result.Metrics.Cancelled, result.Metrics.P95MS, result.ErrorsRate*100, float64(result.Metrics.Total)/result.ElapsedSeconds)
	for _, st := range result.Steps {
		fmt.Fprintf(stdout, "  - %s: total=%d failures=%d p95=%.1f ms\n", st.Name, st.Metrics.Total, st.Metrics.Failures, st.Metrics.P95MS)
	}
	fmt.Fprintf(stdout, "Iterations: %d started / %d completed; arrivals dropped: %d\n", result.IterationsStarted, result.IterationsCompleted, result.ArrivalsDropped)
	for _, w := range result.Warnings {
		fmt.Fprintf(stdout, "WARNING: %s\n", w)
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

// explore runs a bounded doubling-and-bisection search over concurrent virtual users.
func explore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "scenario JSON")
	min := fs.Int("min-users", 2, "first virtual-user level")
	max := fs.Int("max-users", 20, "maximum permitted virtual-user level")
	repeats := fs.Int("repeats", 2, "confirmation runs for each passing level (2–4)")
	stage := fs.Duration("stage-duration", 20*time.Second, "duration of each repeated stage (10s–3m)")
	budget := fs.Duration("budget", 10*time.Minute, "maximum total exploration wall time (up to 15m)")
	authorized := fs.Bool("authorized", false, "confirm authorization to benchmark the target")
	dry := fs.Bool("dry-run", false, "validate and explain the plan; send zero requests")
	writes := fs.Bool("allow-writes", false, "explicitly opt in to HTTP mutation methods")
	insecure := fs.Bool("allow-insecure-http", false, "permit intentional plaintext HTTP to a non-loopback host")
	out := fs.String("out", "", "JSON report output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config is required and positional arguments are not accepted")
		return 2
	}
	options := explorer.Options{MinUsers: *min, MaxUsers: *max, Repeats: *repeats, StageDuration: *stage, Budget: *budget}
	if err := explorer.Validate(options); err != nil {
		fmt.Fprintf(stderr, "invalid exploration: %v\n", err)
		return 2
	}
	scenario, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	scenario.Users = *min
	validated, err := config.Parse(scenario, config.Options{Authorized: *authorized || *dry, AllowWrites: *writes, AllowInsecureHTTP: *insecure})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if scenario.Mode == "arrival" {
		fmt.Fprintln(stderr, "explore requires closed-loop mode")
		return 2
	}
	if scenario.PerVUCookieEnvPrefix != "" {
		if _, err := config.ResolveSessions(scenario.PerVUCookieEnvPrefix, *max); err != nil {
			fmt.Fprintf(stderr, "session validation error: %v\n", err)
			return 2
		}
	}
	fmt.Fprintf(stdout, "Explore: %s | %s://%s | %d–%d virtual users | %s per trial | %d repetitions | rate cap: %d RPS\n", scenario.Name, validated.Base.Scheme, validated.Base.Host, *min, *max, stage.String(), *repeats, scenario.MaxRPS)
	if *dry {
		fmt.Fprintln(stdout, "DRY RUN: no traffic generated. Tests will increase geometrically and refine a failing bracket if found.")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := explorer.Run(ctx, validated, options)
	if err != nil {
		fmt.Fprintf(stderr, "exploration error: %v\n", err)
		return 2
	}
	for _, p := range result.Probes {
		fmt.Fprintf(stdout, "VUs %-2d: %-12s", p.Users, p.Status)
		for _, r := range p.Runs {
			fmt.Fprintf(stdout, "  [p95 %.0fms, errors %.2f%%, requests %d]", r.Metrics.P95MS, r.ErrorsRate*100, r.Metrics.Total)
		}
		fmt.Fprintln(stdout)
	}
	fmt.Fprintf(stdout, "Outcome: %s\n", result.Outcome)
	if result.HighestPassingUsers != nil {
		fmt.Fprintf(stdout, "Highest repeatedly passing tested level: %d VUs (not a universal server limit)\n", *result.HighestPassingUsers)
	}
	if result.FirstFailingUsers != nil {
		fmt.Fprintf(stdout, "First failing tested level: %d VUs\n", *result.FirstFailingUsers)
	}
	for _, note := range result.Notes {
		fmt.Fprintf(stdout, "Note: %s\n", note)
	}
	if *out != "" {
		if err := report.WriteAnyJSON(*out, result); err != nil {
			fmt.Fprintf(stderr, "report error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Report: %s\n", *out)
	}
	if result.Outcome == "BRACKETED" || result.Outcome == "PASSED_TO_CONFIGURED_CEILING" {
		return 0
	}
	return 1
}

// monitor reads host-level CPU and RAM without opening any network listener.
func monitor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	duration := fs.Duration("duration", 2*time.Minute, "sampling duration, up to 15m")
	interval := fs.Duration("interval", 2*time.Second, "sampling interval, 1s–30s")
	out := fs.String("out", "", "restricted JSON metrics file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--out is required and positional arguments are forbidden")
		return 2
	}
	fmt.Fprintf(stdout, "Sampling Linux host CPU and RAM for %s, every %s...\n", duration.String(), interval.String())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := hostmon.Collect(ctx, *duration, *interval)
	if len(result.Samples) > 0 {
		if writeErr := report.WriteAnyJSON(*out, result); writeErr != nil {
			fmt.Fprintf(stderr, "report error: %v\n", writeErr)
			return 2
		}
		fmt.Fprintf(stdout, "Captured %d samples at %s\n", len(result.Samples), *out)
	}
	if err != nil {
		fmt.Fprintf(stderr, "monitor ended: %v\n", err)
		return 1
	}
	return 0
}

func correlateCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("correlate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	load := fs.String("load", "", "explore JSON report")
	host := fs.String("host", "", "Linux monitor JSON report")
	out := fs.String("out", "", "output correlation JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *load == "" || *host == "" || *out == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--load, --host, and --out are required")
		return 2
	}
	result, err := correlate.LoadFiles(*load, *host)
	if err != nil {
		fmt.Fprintf(stderr, "correlation failed: %v\n", err)
		return 2
	}
	if err := report.WriteAnyJSON(*out, result); err != nil {
		fmt.Fprintf(stderr, "write report: %v\n", err)
		return 2
	}
	for _, w := range result.Windows {
		fmt.Fprintf(stdout, "%d VUs (trial %d) %s: %d host samples, mean CPU %.1f%%, peak CPU %.1f%%\n", w.Users, w.RunIndex, w.LoadOutcome, w.Samples, w.CPUAvgPercent, w.CPUPeakPercent)
	}
	fmt.Fprintf(stdout, "Correlation saved to %s. This does not prove a bottleneck root cause.\n", *out)
	return 0
}
