package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func arrivalCampaignCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("arrival-campaign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "arrival-mode scenario JSON")
	warmup := fs.Duration("warmup", 30*time.Second, "warm-up duration (0s-3m)")
	observation := fs.Duration("observation", 2*time.Minute, "steady observation duration (10s-10m; long-run up to 30m)")
	spikeRate := fs.Int("spike-journeys-per-second", 0, "optional higher scheduled journey-start rate")
	spikeDuration := fs.Duration("spike-duration", 0, "optional spike duration (10s-3m)")
	recovery := fs.Duration("recovery", 30*time.Second, "recovery duration (10s-3m)")
	soak := fs.Duration("soak", 0, "optional final soak duration (10s-3m; long-run up to 30m)")
	cooldown := fs.Duration("cooldown", 0, "optional bounded wait between independent trials (0-5m)")
	longRun := fs.Bool("allow-long-run", false, "opt in to observation/soak up to 30m and a wall budget up to 2h")
	stagingConfirmed := fs.Bool("staging-confirmed", false, "confirm the scenario environment is staging for a long-run campaign")
	maxGeneratorCPU := fs.Float64("max-generator-cpu-percent", 0, "abort when sampled generator process CPU exceeds this percent of detected capacity (0 disables)")
	maxGeneratorRSSMB := fs.Uint64("max-generator-rss-mb", 0, "abort when sampled generator process RSS exceeds this MiB limit (0 disables)")
	guardrailSamples := fs.Int("resource-guardrail-samples", 2, "consecutive process samples required to trigger resource aborts (2-20)")
	repeats := fs.Int("repeats", 2, "independent trials (2-5)")
	budget := fs.Duration("budget", 0, "maximum wall-time cap including drains (15m; long-run mode up to 2h)")
	peak := fs.Int("peak-journeys-per-second", 0, "business peak scheduled journey-start demand/s")
	growth := fs.Float64("growth-percent", 0, "projected demand growth percent")
	reserve := fs.Float64("reserve-percent", 0, "additional safety reserve percent")
	authorized := fs.Bool("authorized", false, "confirm authorization to benchmark the target")
	dry := fs.Bool("dry-run", false, "validate and preview phases without sending requests")
	writes := fs.Bool("allow-writes", false, "explicitly opt in to HTTP mutation methods")
	insecure := fs.Bool("allow-insecure-http", false, "permit intentional plaintext HTTP to a non-loopback host")
	out := fs.String("out", "", "JSON campaign report output path")
	calibrationFile := fs.String("calibration", "", "require exact local generator coverage for every planned arrival rate")
	applicationMetricsEndpoint := fs.String("app-metrics-endpoint", "", "optionally sample an authorized Prometheus-text endpoint during every phase")
	applicationMetricsInterval := fs.Duration("app-metrics-interval", 10*time.Second, "application-metrics sampling interval (1s-1m)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config is required and positional arguments are not accepted")
		return 2
	}
	if err := validateApplicationMetrics(*applicationMetricsEndpoint, *applicationMetricsInterval); err != nil {
		fmt.Fprintf(stderr, "application metrics configuration error: %v\n", err)
		return 2
	}
	if !*dry && *out == "" {
		fmt.Fprintln(stderr, "campaigns require --out to preserve every attempted phase and trial")
		return 2
	}
	scenario, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if !*dry {
		if err := rejectCampaignOutputAlias(*filename, *out); err != nil {
			fmt.Fprintf(stderr, "invalid campaign output: %v\n", err)
			return 2
		}
	}
	if scenario.Mode != "arrival" {
		fmt.Fprintln(stderr, "arrival-campaign requires mode=arrival; closed-loop workloads use campaign")
		return 2
	}
	if *peak < 0 || ((*growth != 0 || *reserve != 0) && *peak == 0) {
		fmt.Fprintln(stderr, "growth and reserve require a positive --peak-journeys-per-second")
		return 2
	}
	if *peak > 0 {
		rate, err := arrivalcampaign.RequiredStartsPerSecond(*peak, *growth, *reserve)
		if err != nil {
			fmt.Fprintf(stderr, "invalid journey-start demand: %v\n", err)
			return 2
		}
		scenario.ArrivalRate = rate
	}
	if *maxGeneratorRSSMB > (1<<64-1)/(1024*1024) {
		fmt.Fprintln(stderr, "--max-generator-rss-mb is too large")
		return 2
	}
	guardrails := generatorhealth.Guardrails{MaxProcessCPUPercent: *maxGeneratorCPU, MaxProcessRSSBytes: *maxGeneratorRSSMB * 1024 * 1024}
	if guardrails.Enabled() {
		guardrails.ConsecutiveSamples = *guardrailSamples
	}
	options := arrivalcampaign.Options{Warmup: *warmup, Observation: *observation, SpikeRate: *spikeRate, SpikeDuration: *spikeDuration,
		Recovery: *recovery, Soak: *soak, Cooldown: *cooldown, Repeats: *repeats, Budget: *budget, PeakRate: *peak, GrowthPercent: *growth, ReservePercent: *reserve,
		Guardrails: guardrails,
		LongRun:    *longRun, StagingConfirmed: *stagingConfirmed}
	if err := arrivalcampaign.Validate(config.Validated{Scenario: scenario}, options); err != nil {
		fmt.Fprintf(stderr, "invalid arrival campaign: %v\n", err)
		return 2
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: *authorized || *dry, AllowWrites: *writes, AllowInsecureHTTP: *insecure})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if err := arrivalcampaign.Validate(validated, options); err != nil {
		fmt.Fprintf(stderr, "invalid arrival campaign: %v\n", err)
		return 2
	}
	if scenario.WindowDuration == "" {
		shortest := min(*observation, *recovery)
		if *soak > 0 {
			shortest = min(shortest, *soak)
		}
		validated.Window = min(30*time.Second, shortest/4)
		if options.LongRun {
			longestEvaluated := max(*observation, *soak)
			minimumWindow := (longestEvaluated + arrivalcampaign.MaxCampaignWindows - 1) / arrivalcampaign.MaxCampaignWindows
			validated.Window = max(validated.Window, minimumWindow)
		}
		validated.Scenario.WindowDuration = validated.Window.String()
	}
	fmt.Fprintf(stdout, "Arrival campaign: %s | %s://%s | base %d scheduled starts/s | worker limit %d | observation %s | repeats %d | cooldown %s | wall-time cap %s\n",
		validated.Scenario.Name, validated.Base.Scheme, validated.Base.Host, validated.Scenario.ArrivalRate, validated.Scenario.Users, options.Observation, options.Repeats, options.Cooldown, arrivalcampaign.PlannedBudget(options))
	if options.LongRun {
		fmt.Fprintln(stdout, "LONG-RUN MODE: explicit opt-in; environment declared staging (user-provided, not independently verified).")
	}
	if options.Guardrails.Enabled() {
		fmt.Fprintf(stdout, "Generator abort guardrails: CPU %.1f%% | RSS %.1f MiB | %d consecutive samples. These limits apply to the local generator, not the target application.\n", guardrails.MaxProcessCPUPercent, float64(guardrails.MaxProcessRSSBytes)/(1024*1024), guardrails.ConsecutiveSamples)
	}
	if options.PeakRate > 0 {
		rate, _ := capacitygoal.EvaluatedJourneyStarts(capacitygoal.JourneyStartGoal{PeakStartsPerSecond: options.PeakRate, GrowthPercent: options.GrowthPercent, ReservePercent: options.ReservePercent})
		fmt.Fprintf(stdout, "Demand: peak %d/s, growth %.1f%%, reserve %.1f%%; exact required base rate %d/s.\n", options.PeakRate, options.GrowthPercent, options.ReservePercent, rate)
	}
	if *dry {
		if *applicationMetricsEndpoint != "" {
			fmt.Fprintf(stdout, "Application metrics endpoint validated; dry run will not scrape it (interval %s).\n", *applicationMetricsInterval)
		}
		if *calibrationFile != "" {
			record, loadErr := loadCalibration(*calibrationFile)
			if loadErr != nil {
				fmt.Fprintf(stderr, "calibration error: %v\n", loadErr)
				return 2
			}
			envelope := assessArrivalCampaignEnvelope(validated.Scenario, options, record, time.Now().UTC())
			printArrivalCampaignEnvelope(stdout, envelope, options)
			if envelope.Status != "READY" {
				fmt.Fprintln(stderr, "DRY RUN BLOCKED: calibration does not cover every planned rate; no traffic would be sent.")
				return 2
			}
		}
		for trial := 1; trial <= options.Repeats; trial++ {
			for _, phase := range arrivalcampaign.PlannedPhases(options, validated.Scenario.ArrivalRate, trial) {
				fmt.Fprintf(stdout, "  Trial %d %s: %s at %d scheduled starts/s\n", trial, phase.Name, phase.Duration, phase.Rate)
			}
			if trial < options.Repeats && options.Cooldown > 0 {
				fmt.Fprintf(stdout, "  Cool-down between independent trials: %s\n", options.Cooldown)
			}
		}
		fmt.Fprintln(stdout, "DRY RUN: no traffic generated. Rates are scheduled journey starts/s; completed journeys/s and HTTP requests/s are separate measurements.")
		return 0
	}
	var calibrationRecord *calibration.Record
	if *calibrationFile != "" {
		record, loadErr := loadCalibration(*calibrationFile)
		if loadErr != nil {
			fmt.Fprintf(stderr, "calibration error: %v\n", loadErr)
			return 2
		}
		envelope := assessArrivalCampaignEnvelope(validated.Scenario, options, record, time.Now().UTC())
		printArrivalCampaignEnvelope(stdout, envelope, options)
		if envelope.Status != "READY" {
			fmt.Fprintln(stderr, "CAMPAIGN BLOCKED: calibration does not cover every planned rate; no traffic was sent.")
			return 2
		}
		calibrationRecord = &record
	} else {
		fmt.Fprintln(stdout, "No generator calibration envelope supplied; generator readiness was not checked before the campaign.")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := arrivalcampaign.Runner(engine.Run)
	if calibrationRecord != nil {
		record := *calibrationRecord
		runner = func(ctx context.Context, scenario config.Validated) report.Result {
			run := engine.Run(ctx, scenario)
			attachCalibrationRecord(&run, scenario.Scenario, record, time.Now().UTC())
			return run
		}
	}
	if *applicationMetricsEndpoint != "" {
		fmt.Fprintf(stdout, "Sampling application metrics from the explicitly configured endpoint every %s; scrapes are additional traffic outside max_rps.\n", *applicationMetricsInterval)
		runner = arrivalcampaign.Runner(withApplicationMetrics(*applicationMetricsEndpoint, *applicationMetricsInterval,
			appmetrics.NewBudget(appmetrics.MaxTimelineSamples), phaseRunner(runner)))
	}
	result, err := arrivalcampaign.WithRunnerProgress(ctx, validated, options, runner, func(snapshot arrivalcampaign.Result) error {
		return report.WriteAnyJSON(*out, snapshot)
	})
	if err != nil {
		fmt.Fprintf(stderr, "arrival campaign error: %v\n", err)
		return 2
	}
	for _, trial := range result.Trials {
		fmt.Fprintf(stdout, "Trial %d: %s\n", trial.Number, trial.Outcome)
		for _, phase := range trial.Phases {
			run := phase.Run
			fmt.Fprintf(stdout, "  %s: %s (%s) | scheduled %d | dropped %d | request p95 %.0fms | errors %.2f%% | requests %.2f/s | successful journeys %.2f/s | stability %s\n", phase.Name, run.Outcome, phase.EvidenceState, run.ArrivalsScheduled, run.ArrivalsDropped, run.Metrics.P95MS, run.ErrorsRate*100, float64(run.Metrics.Total)/max(run.WindowSeconds(), 0.001), report.SuccessfulJourneysPerSecond(run), phase.Stability)
			if phase.Reason != "" {
				fmt.Fprintf(stdout, "    %s\n", phase.Reason)
			}
			printApplicationMetricsSummary(stdout, run.ApplicationMetrics)
		}
		if trial.AbortReason != "" {
			fmt.Fprintf(stdout, "  %s\n", trial.AbortReason)
		}
	}
	fmt.Fprintf(stdout, "Campaign outcome: %s\n", result.Outcome)
	fmt.Fprintln(stdout, "Passing means observed evidence for this workload and schedule; it is not a universal capacity certification.")
	if options.PeakRate > 0 {
		assessment, assessErr := capacitygoal.EvaluateJourneyCampaign(result, capacitygoal.JourneyStartGoal{PeakStartsPerSecond: options.PeakRate, GrowthPercent: options.GrowthPercent, ReservePercent: options.ReservePercent})
		if assessErr != nil {
			fmt.Fprintf(stderr, "campaign assessment error: %v\n", assessErr)
		} else {
			fmt.Fprintf(stdout, "BUSINESS GOAL: %s at %d scheduled starts/s\n", assessment.State, assessment.TargetStartsPerSecond)
			for _, finding := range assessment.Findings {
				fmt.Fprintf(stdout, "  - %s\n", finding)
			}
		}
	}
	if *out != "" {
		fmt.Fprintf(stdout, "Report: %s\n", *out)
	}
	if result.Outcome != "PASS" {
		return 1
	}
	return 0
}

func assessArrivalCampaignEnvelope(scenario config.Scenario, options arrivalcampaign.Options, record calibration.Record, now time.Time) calibration.ArrivalEnvelope {
	rates := []int{scenario.ArrivalRate}
	if options.SpikeDuration > 0 {
		rates = append(rates, options.SpikeRate)
	}
	return calibration.AssessArrivalEnvelope(scenario, rates, record, currentCalibrationRuntime(), now)
}

func printArrivalCampaignEnvelope(stdout io.Writer, envelope calibration.ArrivalEnvelope, options arrivalcampaign.Options) {
	fmt.Fprintf(stdout, "Local generator preflight: %s; this is fixture evidence, not target capacity or duration certification.\n", envelope.Status)
	for _, rate := range envelope.Rates {
		fmt.Fprintf(stdout, "  %d scheduled starts/s: %s — %s\n", rate.ArrivalRate, rate.Status, rate.Reason)
	}
	for _, duration := range []time.Duration{options.Warmup, options.Observation, options.SpikeDuration, options.Recovery, options.Soak} {
		if duration > 5*time.Second {
			fmt.Fprintf(stdout, "  Planned %s phase exceeds the 5s reference profile; sustained generator headroom is unverified.\n", duration)
		}
	}
}
