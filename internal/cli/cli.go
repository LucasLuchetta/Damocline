// Package cli is the command-line interface for the Damocline load-testing engine.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/correlate"
	"github.com/LucasLuchetta/Damocline/internal/dockermon"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/reportui"
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
	case "dashboard":
		return dashboardCmd(args[1:], stdout, stderr)
	case "goal":
		return goalCmd(args[1:], stdout, stderr)
	case "plan":
		return planCmd(args[1:], stdout, stderr)
	case "audit":
		return auditCmd(args[1:], stdout, stderr)
	case "run":
		return run(args[1:], stdout, stderr)
	case "explore":
		return explore(args[1:], stdout, stderr)
	case "explore-arrival":
		return exploreArrival(args[1:], stdout, stderr)
	case "campaign":
		return campaignCmd(args[1:], stdout, stderr)
	case "arrival-campaign":
		return arrivalCampaignCmd(args[1:], stdout, stderr)
	case "monitor":
		return monitor(args[1:], stdout, stderr)
	case "metrics":
		return metricsCmd(args[1:], stdout, stderr)
	case "docker":
		return dockerCmd(args[1:], stdout, stderr)
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
	  damocline dashboard --file run.json [--addr 127.0.0.1:8787]
  damocline goal --file capacity.json (--peak-users 20 | --peak-journeys-per-second 5) [--growth-percent 25] [--reserve-percent 20]
	  damocline plan --campaign campaign.json --peak-users 20 [--growth-percent 25] [--reserve-percent 20]
	damocline audit --campaign campaign.json [--campaign higher-level.json] [--peak-users 20] [--growth-percent 25] [--reserve-percent 20] [--out assessment.json]
  damocline run --config scenario.json --authorized [--calibration reports/reference.json] [--out reports/run.json]
  damocline run --config scenario.json --dry-run
  damocline explore --config scenario.json --min-users 2 --max-users 20 --stage-duration 20s --repeats 2 --authorized
  damocline explore-arrival --config arrival.json --min-journeys-per-second 2 --max-journeys-per-second 20 --authorized
	  damocline campaign --config scenario.json --warmup 30s --observation 2m --recovery 30s --repeats 2 --authorized
	damocline arrival-campaign --config arrival.json --observation 2m --recovery 30s --repeats 2 --authorized
  damocline explore --config scenario.json --peak-users 10 --growth-percent 25 --reserve-percent 20 --authorized
  damocline monitor --duration 3m --interval 2s --out reports/host.json
	  damocline metrics --endpoint https://app.example.com/metrics --authorized [--out reports/app-metrics.json]
	  damocline docker --authorized [--out reports/docker.json]
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
mode. 'explore' searches VU levels; 'explore-arrival' searches scheduled open-loop
journey starts per second. Neither is requests/second, successful journeys per
second, or a universal server limit.
`)
}

func exploreArrival(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explore-arrival", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "arrival-mode scenario JSON")
	minRate := fs.Int("min-journeys-per-second", 2, "first scheduled journey-start rate")
	maxRate := fs.Int("max-journeys-per-second", 20, "maximum scheduled journey-start rate")
	targetRate := fs.Int("target-journeys-per-second", 0, "optional exact business demand rate to probe")
	peakDemand := fs.Int("peak-journeys-per-second", 0, "expected peak scheduled journey-start demand/s")
	growth := fs.Float64("growth-percent", 0, "projected demand growth (percent)")
	reserve := fs.Float64("reserve-percent", 0, "additional reserve on projected demand (percent)")
	repeats := fs.Int("repeats", 2, "confirmation runs for each passing rate (2-4)")
	stage := fs.Duration("stage-duration", 20*time.Second, "duration for each repeated probe (10s-3m)")
	budget := fs.Duration("budget", 10*time.Minute, "maximum exploration wall time (up to 15m)")
	authorized := fs.Bool("authorized", false, "confirm permission to benchmark the target")
	dry := fs.Bool("dry-run", false, "validate and explain the rate schedule without sending requests")
	writes := fs.Bool("allow-writes", false, "explicitly opt in to HTTP mutation methods")
	insecure := fs.Bool("allow-insecure-http", false, "permit intentional plaintext HTTP to a non-loopback host")
	out := fs.String("out", "", "JSON exploration report output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config is required and positional arguments are not accepted")
		return 2
	}
	scenario, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if scenario.Mode != "arrival" {
		fmt.Fprintln(stderr, "explore-arrival requires mode=arrival; closed-loop scenarios use damocline explore")
		return 2
	}
	if *peakDemand < 0 || ((*growth != 0 || *reserve != 0) && *peakDemand == 0) {
		fmt.Fprintln(stderr, "--growth-percent and --reserve-percent require a positive --peak-journeys-per-second")
		return 2
	}
	goalRequested := *peakDemand > 0
	if goalRequested {
		required, err := capacitygoal.EvaluatedJourneyStarts(capacitygoal.JourneyStartGoal{
			PeakStartsPerSecond: *peakDemand, GrowthPercent: *growth, ReservePercent: *reserve,
		})
		if err != nil {
			fmt.Fprintf(stderr, "invalid journey-start demand goal: %v\n", err)
			return 2
		}
		if *targetRate > 0 && *targetRate != required {
			fmt.Fprintln(stderr, "--target-journeys-per-second conflicts with the calculated peak, growth, and reserve target")
			return 2
		}
		*targetRate = required
	}
	options := arrivalexplorer.Options{
		MinJourneysPerSecond: *minRate, MaxJourneysPerSecond: *maxRate,
		TargetJourneysPerSecond: *targetRate, Repeats: *repeats,
		StageDuration: *stage, Budget: *budget,
	}
	if err := arrivalexplorer.Validate(options, scenario); err != nil {
		fmt.Fprintf(stderr, "invalid arrival-rate exploration: %v\n", err)
		return 2
	}
	scenario.ArrivalRate = *minRate
	validated, err := config.Parse(scenario, config.Options{
		Authorized: *authorized || *dry, AllowWrites: *writes, AllowInsecureHTTP: *insecure,
	})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Arrival-rate explore: %s | %s://%s | scheduled journey starts %d-%d/s | worker limit %d | stage %s | repeats %d | max_rps %d\n",
		validated.Scenario.Name, validated.Base.Scheme, validated.Base.Host,
		*minRate, *maxRate, validated.Scenario.Users, stage.String(), *repeats, validated.Scenario.MaxRPS)
	if *targetRate > 0 {
		if *targetRate < *minRate || *targetRate > *maxRate {
			fmt.Fprintln(stdout, "WARNING: exact journey-start target is outside the configured range and will not raise the safe ceiling.")
		} else {
			fmt.Fprintf(stdout, "Exact scheduled journey-start target: %d/s (tested only when reached through the bounded search).\n", *targetRate)
		}
	}
	if goalRequested {
		fmt.Fprintf(stdout, "Business demand: peak %d starts/s, growth %.1f%%, reserve %.1f%%; required exact target %d starts/s.\n",
			*peakDemand, *growth, *reserve, *targetRate)
	}
	if *dry {
		fmt.Fprintln(stdout, "DRY RUN: no traffic generated. Rates are scheduled journey starts/s; request/s and completed journeys/s are separate measured quantities.")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := arrivalexplorer.Run(ctx, validated, options)
	if err != nil {
		fmt.Fprintf(stderr, "arrival-rate exploration error: %v\n", err)
		return 2
	}
	for _, probe := range result.Probes {
		fmt.Fprintf(stdout, "Scheduled starts %d/s: %s\n", probe.JourneysPerSecond, probe.Status)
		if probe.Reason != "" {
			fmt.Fprintf(stdout, "  Reason: %s\n", probe.Reason)
		}
		for _, run := range probe.Runs {
			seconds := run.WindowSeconds()
			if seconds <= 0 {
				fmt.Fprintf(stdout, "  no valid observation duration; scheduled %d; admitted %v; dropped %d\n", run.ArrivalsScheduled, run.ArrivalsAdmitted, run.ArrivalsDropped)
				continue
			}
			admitted := int64(0)
			if run.ArrivalsAdmitted != nil {
				admitted = *run.ArrivalsAdmitted
			}
			fmt.Fprintf(stdout, "  offered %.2f starts/s; admitted %.2f/s; dropped %d; request throughput %.2f/s; successful completed journeys %.2f/s; request p95 %.0fms; errors %.2f%%\n",
				float64(run.ArrivalsScheduled)/seconds, float64(admitted)/seconds, run.ArrivalsDropped,
				float64(run.Metrics.Total)/seconds, report.SuccessfulJourneysPerSecond(run), run.Metrics.P95MS, run.ErrorsRate*100)
		}
	}
	fmt.Fprintf(stdout, "Outcome: %s\n", result.Outcome)
	if result.Reason != "" {
		fmt.Fprintf(stdout, "Reason: %s\n", result.Reason)
	}
	if result.HighestPassingJourneysPerSecond != nil {
		fmt.Fprintf(stdout, "Highest repeatedly passing tested scheduled-start rate: %d journeys/s\n", *result.HighestPassingJourneysPerSecond)
	}
	if result.FirstFailingJourneysPerSecond != nil {
		fmt.Fprintf(stdout, "First observed failing tested scheduled-start rate: %d journeys/s\n", *result.FirstFailingJourneysPerSecond)
	}
	for _, note := range result.Notes {
		fmt.Fprintf(stdout, "Note: %s\n", note)
	}
	if goalRequested {
		assessment, assessErr := capacitygoal.EvaluateJourneyStarts(result, capacitygoal.JourneyStartGoal{
			PeakStartsPerSecond: *peakDemand, GrowthPercent: *growth, ReservePercent: *reserve,
		})
		if assessErr != nil {
			fmt.Fprintf(stderr, "arrival demand assessment error: %v\n", assessErr)
		} else {
			fmt.Fprintf(stdout, "BUSINESS GOAL: %s | required %d scheduled starts/s.\n", assessment.State, assessment.TargetStartsPerSecond)
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
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "path to scenario JSON")
	authorized := fs.Bool("authorized", false, "confirm explicit permission to test the target")
	writes := fs.Bool("allow-writes", false, "allow potentially destructive HTTP methods")
	insecure := fs.Bool("allow-insecure-http", false, "allow plaintext HTTP outside loopback")
	dry := fs.Bool("dry-run", false, "validate and display a sanitized plan without sending traffic")
	out := fs.String("out", "", "write a JSON report (file permissions 0600)")
	calibrationFile := fs.String("calibration", "", "attach a validated local generator calibration artifact")
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
			if len(profile.ThinkTimeSamples) > 0 {
				fmt.Fprintf(stdout, "  Profile %s: weight=%d, sampled think time=%v (seed=%d)\n", profile.Name, profile.Weight, profile.ThinkTimeSamples, validated.Scenario.WorkloadSeed)
				continue
			}
			if profile.ThinkTimeMax > 0 {
				fmt.Fprintf(stdout, "  Profile %s: weight=%d, uniform think time=%s..%s (seed=%d)\n", profile.Name, profile.Weight, profile.ThinkTimeMin, profile.ThinkTimeMax, validated.Scenario.WorkloadSeed)
				continue
			}
			fmt.Fprintf(stdout, "  Profile %s: weight=%d, think time=%s\n", profile.Name, profile.Weight, profile.ThinkTime)
		}
	} else if len(validated.Profiles[0].ThinkTimeSamples) > 0 {
		fmt.Fprintf(stdout, "  Sampled think time=%v (seed=%d)\n", validated.Profiles[0].ThinkTimeSamples, validated.Scenario.WorkloadSeed)
	} else if validated.Profiles[0].ThinkTimeMax > 0 {
		fmt.Fprintf(stdout, "  Uniform think time=%s..%s (seed=%d)\n", validated.Profiles[0].ThinkTimeMin, validated.Profiles[0].ThinkTimeMax, validated.Scenario.WorkloadSeed)
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
	if *calibrationFile != "" {
		if calibrationErr := attachCalibration(&result, validated.Scenario, *calibrationFile, time.Now().UTC()); calibrationErr != nil {
			result.Warnings = append(result.Warnings, "The requested local generator calibration could not be validated and was not attached.")
		}
	}
	fmt.Fprintf(stdout, "Result: %s | requests: %d (%d passed, %d failed, %d cancelled) | request p95: %.1f ms | error rate: %.2f%% | %.1f req/s\n", result.Outcome, result.Metrics.Total, result.Metrics.Success, result.Metrics.Failures, result.Metrics.Cancelled, result.Metrics.P95MS, result.ErrorsRate*100, float64(result.Metrics.Total)/result.WindowSeconds())
	if result.ObservationSeconds > 0 {
		fmt.Fprintf(stdout, "Journey throughput: %.2f successful configured journeys/s; %.2f completed journeys/s (%d complete, %d failed, %d cancelled/incomplete); completed journey p95: %s.\n", report.SuccessfulJourneysPerSecond(result), report.CompletedJourneysPerSecond(result), result.Journeys.Total, result.Journeys.Failures, result.Journeys.Cancelled, journeyP95Description(result.Journeys))
	}
	fmt.Fprintf(
		stdout,
		"HTTP responses: 1xx=%d 2xx=%d 3xx=%d 4xx=%d 5xx=%d; redirects=%d, 429=%d, assertions=%d, timeouts=%d, DNS failures=%d, connection failures=%d, TLS failures=%d, transport failures=%d, oversized=%d.\n",
		result.Metrics.HTTP.Status1xx,
		result.Metrics.HTTP.Status2xx,
		result.Metrics.HTTP.Status3xx,
		result.Metrics.HTTP.Status4xx,
		result.Metrics.HTTP.Status5xx,
		result.Metrics.HTTP.RedirectResponses,
		result.Metrics.HTTP.RateLimitedResponses,
		result.Metrics.HTTP.AssertionFailures,
		result.Metrics.HTTP.NetworkTimeouts,
		result.Metrics.HTTP.DNSFailures,
		result.Metrics.HTTP.ConnectionFailures,
		result.Metrics.HTTP.TLSFailures,
		result.Metrics.HTTP.TransportFailures,
		result.Metrics.HTTP.UnexpectedResponseSizes,
	)
	if result.Metrics.P95IID95Approximation {
		fmt.Fprintf(stdout, "P95 IID 95%% rank approximation: %.1f-%.1f ms; this assumes independent, stationary request latencies.\n", result.Metrics.P95IID95LowerMS, result.Metrics.P95IID95UpperMS)
	}
	for _, st := range result.Steps {
		fmt.Fprintf(stdout, "  - %s/%s: total=%d failures=%d request p95=%.1f ms\n", st.Profile, st.Name, st.Metrics.Total, st.Metrics.Failures, st.Metrics.P95MS)
		fmt.Fprintf(stdout, "    client timing totals: rate-wait=%.1f ms, DNS=%.1f ms (%d attempts), connect=%.1f ms (%d attempts), TLS=%.1f ms (%d attempts), request-write=%.1f ms (%d observations), first-byte=%.1f ms, body=%.1f ms, full=%.1f ms; first-byte is not server processing time.\n", st.HTTPTrace.RateWaitMS, st.HTTPTrace.DNSMS, st.HTTPTrace.DNSCount, st.HTTPTrace.ConnectMS, st.HTTPTrace.ConnectCount, st.HTTPTrace.TLSMS, st.HTTPTrace.TLSCount, st.HTTPTrace.WriteMS, st.HTTPTrace.WriteCount, st.HTTPTrace.FirstByteMS, st.HTTPTrace.BodyMS, st.HTTPTrace.CompleteMS)
	}
	for _, profile := range result.Profiles {
		started := profile.Journeys.Total + profile.Journeys.Cancelled
		rate := 0.0
		if result.ObservationSeconds > 0 {
			rate = float64(profile.Journeys.Success) / result.ObservationSeconds
		}
		if result.Config.Mode == "closed" {
			fmt.Fprintf(stdout, "  Profile %s: configured weight=%d, assigned VUs=%d, started=%d, successful=%d (%.2f journeys/s), failed-complete=%d, cancelled-incomplete=%d\n", profile.Name, profile.Weight, profile.AssignedVUs, started, profile.Journeys.Success, rate, profile.Journeys.Failures, profile.Journeys.Cancelled)
			continue
		}
		fmt.Fprintf(stdout, "  Profile %s: configured weight=%d, started=%d, successful=%d (%.2f journeys/s), failed-complete=%d, cancelled-incomplete=%d\n", profile.Name, profile.Weight, started, profile.Journeys.Success, rate, profile.Journeys.Failures, profile.Journeys.Cancelled)
	}
	fmt.Fprintf(stdout, "Iterations: %d started / %d completed; arrivals dropped: %d\n", result.IterationsStarted, result.IterationsCompleted, result.ArrivalsDropped)
	fmt.Fprintf(stdout, "Observed configured think-time waits: %.1f ms across %d intervals; completed journey latency includes rate-token waits and configured between-step pauses.\n", result.ObservedThinkTimeMS, result.ThinkTimeObservations)
	fmt.Fprintln(stdout, "Latency scope: request p95 measures individual client-observed HTTP requests; completed journey p95 covers the configured HTTP workflow. Neither measures browser rendering or full page load.")
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

func journeyP95Description(metrics report.MetricSet) string {
	if metrics.Total == 0 {
		return "unavailable (no completed journeys)"
	}
	return fmt.Sprintf("%.1f ms across completed journeys", metrics.P95MS)
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
	fmt.Fprintf(stdout, "Campaign: %s | %s://%s | warm-up %s | observation %s | spike %d VUs for %s | recovery %s | repeats %d | wall-time cap %s\n", validated.Scenario.Name, validated.Base.Scheme, validated.Base.Host, options.Warmup, options.Observation, options.SpikeUsers, options.SpikeDuration, options.Recovery, options.Repeats, campaign.PlannedBudget(options))
	if *dry {
		for trial := 1; trial <= options.Repeats; trial++ {
			for _, phase := range campaign.PlannedPhases(options, validated.Scenario.Users) {
				fmt.Fprintf(stdout, "  Trial %d %s: %s at %d VUs\n", trial, phase.Name, phase.Duration, phase.Users)
			}
		}
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
		if trial.AbortReason != "" {
			fmt.Fprintf(stdout, "  Abort reason: %s\n", trial.AbortReason)
		}
		for _, phase := range trial.Phases {
			fmt.Fprintf(stdout, "  %s: %s (%s) | p95 %.0fms | errors %.2f%% | requests %d | successful journeys %.2f/s (%d complete, %d failed, %d cancelled)\n", phase.Name, phase.Run.Outcome, phase.EvidenceState, phase.Run.Metrics.P95MS, phase.Run.ErrorsRate*100, phase.Run.Metrics.Total, report.SuccessfulJourneysPerSecond(phase.Run), phase.Run.Journeys.Total, phase.Run.Journeys.Failures, phase.Run.Journeys.Cancelled)
			if phase.Stability != "" {
				fmt.Fprintf(stdout, "    observation drift screen: %s\n", phase.Stability)
			}
			if phase.EvidenceState == "INVALID_DELIVERY" || phase.EvidenceState == "INSUFFICIENT_EVIDENCE" {
				fmt.Fprintf(stdout, "    %s\n", phase.Reason)
			}
		}
	}
	if variability := result.ObservationVariability; variability != nil {
		fmt.Fprintf(stdout, "Observation spread across %d trials: p95 %.0f-%.0fms | errors %.2f%%-%.2f%% | requests %.2f-%.2f/s | successful journeys %.2f-%.2f/s\n", variability.Runs, variability.P95MinMS, variability.P95MaxMS, variability.ErrorRateMin*100, variability.ErrorRateMax*100, variability.ThroughputMinRPS, variability.ThroughputMaxRPS, variability.SuccessfulJourneysMinPerSec, variability.SuccessfulJourneysMaxPerSec)
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

func dashboardCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "Damocline HTTP run report JSON")
	address := fs.String("addr", "127.0.0.1:8787", "loopback listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline dashboard --file run.json [--addr 127.0.0.1:8787]")
		return 2
	}
	if err := reportui.ListenAddress(*address); err != nil {
		fmt.Fprintf(stderr, "invalid dashboard address: %v\n", err)
		return 2
	}
	result, err := insights.ReadRun(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read run: %v\n", err)
		return 2
	}
	server := &http.Server{Addr: *address, Handler: reportui.Handler(result), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(stdout, "Local dashboard: http://%s/ (Ctrl+C to stop)\n", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "dashboard error: %v\n", err)
		return 2
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
func dockerCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	authorized := fs.Bool("authorized", false, "confirm permission to inspect local Docker resources")
	out := fs.String("out", "", "JSON snapshot output path")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 {
		return 2
	}
	if !*authorized {
		fmt.Fprintln(stderr, "explicit --authorized confirmation is required before collecting Docker metrics")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := dockermon.Collect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "docker metrics error: %v\n", err)
		return 2
	}
	for _, container := range snapshot.Containers {
		fmt.Fprintf(stdout, "%s: CPU %.1f%% | memory %d / %d bytes\n", container.Name, container.CPUPercent, container.MemoryBytes, container.MemoryLimit)
	}
	fmt.Fprintln(stdout, snapshot.Note)
	if *out != "" {
		if err := report.WriteAnyJSON(*out, snapshot); err != nil {
			fmt.Fprintf(stderr, "report error: %v\n", err)
			return 2
		}
	}
	return 0
}

func metricsCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("endpoint", "", "explicit authorized application metrics endpoint")
	authorized := fs.Bool("authorized", false, "confirm permission to access the metrics endpoint")
	out := fs.String("out", "", "JSON snapshot output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *endpoint == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--endpoint is required and positional arguments are not accepted")
		return 2
	}
	if !*authorized {
		fmt.Fprintln(stderr, "explicit --authorized confirmation is required before accessing metrics")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := appmetrics.Collect(ctx, *endpoint)
	if err != nil {
		fmt.Fprintf(stderr, "metrics error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Application metrics: %s | %d metric names\n", snapshot.Source, len(snapshot.Samples))
	for _, sample := range snapshot.Samples {
		fmt.Fprintf(stdout, "  %s = %g\n", sample.Name, sample.Value)
	}
	fmt.Fprintln(stdout, snapshot.Note)
	if *out != "" {
		if err := report.WriteAnyJSON(*out, snapshot); err != nil {
			fmt.Fprintf(stderr, "report error: %v\n", err)
			return 2
		}
	}
	return 0
}

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
