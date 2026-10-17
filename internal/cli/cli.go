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

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/correlate"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

var Version = "dev"

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return welcome(os.Stdin, stdout, stderr)
	}
	switch args[0] {
	case "help", "--help", "-h":
		help(stdout)
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "damocline %s (advanced HTTP pre-alpha)\n", Version)
		return 0
	case "update":
		return updateCmd(args[1:], stdout, stderr)
	case "start":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: damocline start")
			return 2
		}
		return start(os.Stdin, stdout, stderr)
	case "/start":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: damocline /start")
			return 2
		}
		return startWithMode(os.Stdin, stdout, stderr, "new")
	case "/history":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: damocline /history")
			return 2
		}
		return startWithMode(os.Stdin, stdout, stderr, "history")
	case "report":
		return reportCmd(args[1:], stdout, stderr)
	case "goal":
		return goalCmd(args[1:], stdout, stderr)
	case "run":
		return run(args[1:], stdout, stderr)
	case "explore":
		return explore(args[1:], stdout, stderr)
	case "campaign":
		return campaignCmd(args[1:], stdout, stderr)
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
  damocline          # branded welcome screen with guided menu
  damocline /start   # create a new saved scenario (archives the previous one)
  damocline /history # select and repeat a previous saved scenario
  damocline start    # original guided wizard
  damocline report --file run.json [--compare previous.json] [--host host.json]
  damocline goal --file capacity.json --peak-users 20 [--growth-percent 25] [--reserve-percent 20]
  damocline run --config scenario.json --authorized [--out reports/run.json]
  damocline run --config scenario.json --dry-run
  damocline explore --config scenario.json --min-users 2 --max-users 20 --stage-duration 20s --repeats 2 --authorized
	  damocline campaign --config scenario.json --warmup 30s --observation 2m --recovery 30s --repeats 2 --authorized
  damocline explore --config scenario.json --peak-users 10 --growth-percent 25 --reserve-percent 20 --authorized
  damocline monitor --duration 3m --interval 2s --out reports/host.json
  damocline correlate --load reports/load.json --host reports/host.json --out reports/correlated.json
  damocline update           # check and install source updates
  damocline update --auto    # enable automatic updates
  damocline update --off     # disable automatic updates
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
	if len(validated.Scenario.Profiles) > 0 {
		for _, profile := range validated.Profiles {
			fmt.Fprintf(stdout, "  Profile %s: weight=%d, think time=%s\n", profile.Name, profile.Weight, profile.ThinkTime)
		}
	}
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
	fmt.Fprintf(stdout, "Result: %s | requests: %d (%d passed, %d failed, %d cancelled) | p95: %.1f ms | error rate: %.2f%% | %.1f req/s\n", result.Outcome, result.Metrics.Total, result.Metrics.Success, result.Metrics.Failures, result.Metrics.Cancelled, result.Metrics.P95MS, result.ErrorsRate*100, float64(result.Metrics.Total)/result.WindowSeconds())
	for _, st := range result.Steps {
		fmt.Fprintf(stdout, "  - %s/%s: total=%d failures=%d p95=%.1f ms\n", st.Profile, st.Name, st.Metrics.Total, st.Metrics.Failures, st.Metrics.P95MS)
	}
	for _, profile := range result.Profiles {
		started := profile.Journeys.Total + profile.Journeys.Cancelled
		if result.Config.Mode == "closed" {
			fmt.Fprintf(stdout, "  Profile %s: configured weight=%d, assigned VUs=%d, started journeys=%d, completed=%d, cancelled=%d\n", profile.Name, profile.Weight, profile.AssignedVUs, started, profile.Journeys.Total, profile.Journeys.Cancelled)
			continue
		}
		fmt.Fprintf(stdout, "  Profile %s: configured weight=%d, started journeys=%d, completed=%d, cancelled=%d\n", profile.Name, profile.Weight, started, profile.Journeys.Total, profile.Journeys.Cancelled)
	}
	fmt.Fprintf(stdout, "Iterations: %d started / %d completed; arrivals dropped: %d\n", result.IterationsStarted, result.IterationsCompleted, result.ArrivalsDropped)
	for _, w := range result.Warnings {
		fmt.Fprintf(stdout, "WARNING: %s\n", w)
	}
	fmt.Fprintln(stdout, "Note: PASS means thresholds passed in this run, not a validated sustainable capacity ceiling.")
	insights.Write(stdout, result, nil, nil)
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

func campaignCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("campaign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "scenario JSON")
	warmup := fs.Duration("warmup", 30*time.Second, "warm-up duration (0s-3m)")
	observation := fs.Duration("observation", 2*time.Minute, "steady observation duration (10s-3m)")
	spikeUsers := fs.Int("spike-users", 0, "optional peak virtual users above the base scenario")
	spikeDuration := fs.Duration("spike-duration", 0, "optional spike duration (10s-3m)")
	recovery := fs.Duration("recovery", 30*time.Second, "recovery duration after observation (0s-3m)")
	repeats := fs.Int("repeats", 2, "independent trials (2-5)")
	budget := fs.Duration("budget", 0, "maximum campaign wall time (up to 15m)")
	authorized := fs.Bool("authorized", false, "confirm authorization to benchmark the target")
	dry := fs.Bool("dry-run", false, "validate campaign settings without sending traffic")
	writes := fs.Bool("allow-writes", false, "explicitly opt in to HTTP mutation methods")
	insecure := fs.Bool("allow-insecure-http", false, "permit intentional plaintext HTTP to a non-loopback host")
	out := fs.String("out", "", "JSON campaign report output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config is required and positional arguments are not accepted")
		return 2
	}
	options := campaign.Options{Warmup: *warmup, Observation: *observation, SpikeUsers: *spikeUsers, SpikeDuration: *spikeDuration, Recovery: *recovery, Repeats: *repeats, Budget: *budget}
	if err := campaign.Validate(options); err != nil {
		fmt.Fprintf(stderr, "invalid campaign: %v\n", err)
		return 2
	}
	scenario, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: *authorized || *dry, AllowWrites: *writes, AllowInsecureHTTP: *insecure})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Campaign: %s | %s://%s | warm-up %s | observation %s | spike %d VUs for %s | recovery %s | repeats %d\n", validated.Scenario.Name, validated.Base.Scheme, validated.Base.Host, options.Warmup, options.Observation, options.SpikeUsers, options.SpikeDuration, options.Recovery, options.Repeats)
	if *dry {
		fmt.Fprintln(stdout, "DRY RUN: no traffic generated. Warm-up is excluded from the conclusion; observation and recovery must pass in every trial.")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := campaign.Run(ctx, validated, options)
	if err != nil {
		fmt.Fprintf(stderr, "campaign error: %v\n", err)
		return 2
	}
	for _, trial := range result.Trials {
		fmt.Fprintf(stdout, "Trial %d: %s\n", trial.Number, trial.Outcome)
		for _, phase := range trial.Phases {
			fmt.Fprintf(stdout, "  %s: %s | p95 %.0fms | errors %.2f%% | requests %d\n", phase.Name, phase.Run.Outcome, phase.Run.Metrics.P95MS, phase.Run.ErrorsRate*100, phase.Run.Metrics.Total)
		}
	}
	if variability := result.ObservationVariability; variability != nil {
		fmt.Fprintf(stdout, "Observation spread across %d trials: p95 %.0f-%.0fms | errors %.2f%%-%.2f%% | throughput %.2f-%.2f req/s\n", variability.Runs, variability.P95MinMS, variability.P95MaxMS, variability.ErrorRateMin*100, variability.ErrorRateMax*100, variability.ThroughputMinRPS, variability.ThroughputMaxRPS)
		fmt.Fprintln(stdout, "This is observed run-to-run spread, not a statistical confidence interval.")
	}
	fmt.Fprintf(stdout, "Campaign outcome: %s\n", result.Outcome)
	fmt.Fprintln(stdout, "This is repeated observed evidence under the configured workload. It is not a universal capacity certification or infrastructure recommendation.")
	if *out != "" {
		if err := report.WriteAnyJSON(*out, result); err != nil {
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
	peak := fs.Int("peak-users", 0, "planned peak active-equivalent virtual-user demand")
	growth := fs.Float64("growth-percent", 0, "projected demand growth, percent")
	reserve := fs.Float64("reserve-percent", 0, "reserve on grown demand, percent")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config is required and positional arguments are not accepted")
		return 2
	}
	options := explorer.Options{MinUsers: *min, MaxUsers: *max, Repeats: *repeats, StageDuration: *stage, Budget: *budget}
	goalRequested := *peak != 0 || *growth != 0 || *reserve != 0
	var goal capacitygoal.Goal
	var requiredUsers int
	if goalRequested {
		goal = capacitygoal.Goal{PeakUsers: *peak, GrowthPercent: *growth, ReservePercent: *reserve}
		var goalErr error
		requiredUsers, goalErr = capacitygoal.EvaluatedUsers(goal)
		if goalErr != nil {
			fmt.Fprintf(stderr, "invalid business capacity goal: %v\n", goalErr)
			return 2
		}
		if requiredUsers >= *min && requiredUsers <= *max {
			options.TargetUsers = requiredUsers
		}
	}
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
	if goalRequested {
		fmt.Fprintf(stdout, "Business workload target: %d active-equivalent VUs (peak %d, growth %.1f%%, reserve %.1f%%).\n", requiredUsers, *peak, *growth, *reserve)
		if requiredUsers > *max || requiredUsers < *min {
			fmt.Fprintln(stdout, "WARNING: target is outside configured search bounds; this command will NOT lift safe test limits.")
		} else {
			fmt.Fprintln(stdout, "The explorer will attempt the exact target as a separate stage if its normal bounded search reaches that workload.")
		}
	}
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
	fmt.Fprintln(stdout, "Server upgrade recommendation: NOT ESTABLISHED. Explore measures workload-specific passing levels, not server headroom; collect host/container/database metrics before sizing infrastructure.")
	if result.HighestPassingUsers != nil {
		fmt.Fprintf(stdout, "Highest repeatedly passing tested level: %d VUs (not a universal server limit)\n", *result.HighestPassingUsers)
	}
	if result.FirstFailingUsers != nil {
		fmt.Fprintf(stdout, "First failing tested level: %d VUs\n", *result.FirstFailingUsers)
	}
	for _, note := range result.Notes {
		fmt.Fprintf(stdout, "Note: %s\n", note)
	}
	if goalRequested {
		assessment, assessmentErr := capacitygoal.Evaluate(result, goal)
		if assessmentErr != nil {
			fmt.Fprintf(stdout, "BUSINESS GOAL: INCONCLUSIVE (%v). No server sizing decision is justified.\n", assessmentErr)
		} else {
			fmt.Fprintf(stdout, "BUSINESS GOAL: %s | required %d active-equivalent VUs.\n", assessment.State, assessment.TargetUsers)
			for _, finding := range assessment.Findings {
				fmt.Fprintf(stdout, "  - %s\n", finding)
			}
		}
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
