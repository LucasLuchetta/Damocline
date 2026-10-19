// Package campaign runs bounded multi-phase capacity observations.
package campaign

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const CurrentSchemaVersion = 10

const (
	DefaultWallTimeLimit = 15 * time.Minute
	LongRunWallTimeLimit = 2 * time.Hour
	LongRunPhaseLimit    = 30 * time.Minute
	MaxCampaignWindows   = 120
)

// StabilityPolicy sets bounded thresholds for the campaign window drift
// screen. It is a coarse screening policy, not a stationarity test.
type StabilityPolicy struct {
	MinimumWindows           int     `json:"minimum_windows"`
	MinimumSamplesPerWindow  int     `json:"minimum_samples_per_window"`
	MaxP95IncreasePercent    float64 `json:"max_p95_increase_percent"`
	MaxP95IncreaseMS         float64 `json:"max_p95_increase_ms"`
	MaxThroughputDropPercent float64 `json:"max_throughput_drop_percent"`
}

func DefaultStabilityPolicy() StabilityPolicy {
	return StabilityPolicy{
		MinimumWindows: 3, MinimumSamplesPerWindow: 20,
		MaxP95IncreasePercent: 20, MaxP95IncreaseMS: 5,
		MaxThroughputDropPercent: 20,
	}
}

func ValidateStabilityPolicy(policy StabilityPolicy) error {
	if policy.MinimumWindows < 3 || policy.MinimumWindows > 20 {
		return fmt.Errorf("stability minimum-windows must be between 3 and 20")
	}
	if policy.MinimumSamplesPerWindow < 20 || policy.MinimumSamplesPerWindow > 100000 {
		return fmt.Errorf("stability minimum-samples must be between 20 and 100000")
	}
	if math.IsNaN(policy.MaxP95IncreasePercent) || math.IsInf(policy.MaxP95IncreasePercent, 0) || policy.MaxP95IncreasePercent <= 0 || policy.MaxP95IncreasePercent > 100 {
		return fmt.Errorf("stability max-p95-increase-percent must be greater than 0 and at most 100")
	}
	if math.IsNaN(policy.MaxP95IncreaseMS) || math.IsInf(policy.MaxP95IncreaseMS, 0) || policy.MaxP95IncreaseMS < 0 || policy.MaxP95IncreaseMS > 10000 {
		return fmt.Errorf("stability max-p95-increase-ms must be between 0 and 10000")
	}
	if math.IsNaN(policy.MaxThroughputDropPercent) || math.IsInf(policy.MaxThroughputDropPercent, 0) || policy.MaxThroughputDropPercent <= 0 || policy.MaxThroughputDropPercent > 100 {
		return fmt.Errorf("stability max-throughput-drop-percent must be greater than 0 and at most 100")
	}
	return nil
}

func effectiveStabilityPolicy(policy StabilityPolicy) StabilityPolicy {
	if policy == (StabilityPolicy{}) {
		return DefaultStabilityPolicy()
	}
	return policy
}

type Options struct {
	Warmup           time.Duration
	Observation      time.Duration
	SpikeUsers       int
	SpikeDuration    time.Duration
	Recovery         time.Duration
	Soak             time.Duration
	Cooldown         time.Duration
	Repeats          int
	Budget           time.Duration
	Stability        StabilityPolicy
	Guardrails       generatorhealth.Guardrails
	LongRun          bool
	StagingConfirmed bool
}

// PlannedPhase is one bounded stage in each campaign trial.
type PlannedPhase struct {
	Name     string
	Duration time.Duration
	Users    int
	Required bool
}

// PlannedPhases returns the exact stage order used by every trial.
func PlannedPhases(options Options, baseUsers int) []PlannedPhase {
	phases := make([]PlannedPhase, 0, 4)
	if options.Warmup > 0 {
		phases = append(phases, PlannedPhase{Name: "warmup", Duration: options.Warmup, Users: baseUsers})
	}
	phases = append(phases, PlannedPhase{Name: "observation", Duration: options.Observation, Users: baseUsers, Required: true})
	if options.SpikeDuration > 0 {
		phases = append(phases, PlannedPhase{Name: "spike", Duration: options.SpikeDuration, Users: options.SpikeUsers, Required: true})
	}
	if options.Recovery > 0 {
		phases = append(phases, PlannedPhase{Name: "recovery", Duration: options.Recovery, Users: baseUsers, Required: true})
	}
	return phases
}

// PlannedTrialPhases returns the regular trial schedule and appends a single
// bounded soak after recovery on the final independent trial.
func PlannedTrialPhases(options Options, baseUsers, trial int) []PlannedPhase {
	phases := PlannedPhases(options, baseUsers)
	if options.Soak > 0 && trial == options.Repeats {
		phases = append(phases, PlannedPhase{Name: "soak", Duration: options.Soak, Users: baseUsers, Required: true})
	}
	return phases
}

type Phase struct {
	Name          string        `json:"name"`
	EvidenceState string        `json:"evidence_state,omitempty"`
	Reason        string        `json:"reason,omitempty"`
	Stability     string        `json:"stability,omitempty"`
	Run           report.Result `json:"run"`
}

type Trial struct {
	RunID       string  `json:"run_id,omitempty"`
	Number      int     `json:"number"`
	Phases      []Phase `json:"phases"`
	Outcome     string  `json:"outcome"`
	AbortReason string  `json:"abort_reason,omitempty"`
}

// ProgressFunc durably records a campaign snapshot. The snapshot may carry a
// pending trial whose terminal outcome is not yet known.
type ProgressFunc func(Result) error

type Result struct {
	SchemaVersion          int                         `json:"schema_version"`
	ExecutionState         string                      `json:"execution_state,omitempty"`
	Scenario               string                      `json:"scenario"`
	BaseUsers              int                         `json:"base_users"`
	Target                 string                      `json:"target"`
	StartedAt              string                      `json:"started_at"`
	Warmup                 string                      `json:"warmup"`
	Observation            string                      `json:"observation"`
	SpikeUsers             int                         `json:"spike_users,omitempty"`
	SpikeDuration          string                      `json:"spike_duration,omitempty"`
	Recovery               string                      `json:"recovery"`
	Soak                   string                      `json:"soak,omitempty"`
	Cooldown               string                      `json:"cooldown,omitempty"`
	LongRun                bool                        `json:"long_run,omitempty"`
	DeclaredEnvironment    string                      `json:"declared_environment,omitempty"`
	StabilityPolicy        *StabilityPolicy            `json:"stability_policy,omitempty"`
	GeneratorGuardrails    *generatorhealth.Guardrails `json:"generator_guardrails,omitempty"`
	Budget                 string                      `json:"budget,omitempty"`
	Repeats                int                         `json:"repeats"`
	Trials                 []Trial                     `json:"trials"`
	PendingTrial           *Trial                      `json:"pending_trial,omitempty"`
	ObservationVariability *Variability                `json:"observation_variability,omitempty"`
	Outcome                string                      `json:"outcome"`
	Notes                  []string                    `json:"notes"`
}

// EffectiveStabilityPolicy returns the policy recorded in current campaign
// evidence or the historical default for campaign schemas before v5.
func (r Result) EffectiveStabilityPolicy() StabilityPolicy {
	if r.SchemaVersion >= 5 {
		if r.StabilityPolicy != nil {
			return *r.StabilityPolicy
		}
		return StabilityPolicy{}
	}
	return DefaultStabilityPolicy()
}

// Variability summarizes the spread observed across independent observation
// phases. It is descriptive and does not claim a confidence interval.
type Variability struct {
	Runs                        int     `json:"runs"`
	P95MinMS                    float64 `json:"p95_min_ms"`
	P95MaxMS                    float64 `json:"p95_max_ms"`
	ErrorRateMin                float64 `json:"error_rate_min"`
	ErrorRateMax                float64 `json:"error_rate_max"`
	ThroughputMinRPS            float64 `json:"throughput_min_rps"`
	ThroughputMaxRPS            float64 `json:"throughput_max_rps"`
	SuccessfulJourneysMinPerSec float64 `json:"successful_journeys_min_per_second"`
	SuccessfulJourneysMaxPerSec float64 `json:"successful_journeys_max_per_second"`
}

type Runner func(context.Context, config.Validated) report.Result

func Validate(options Options) error {
	if err := validateGeneratorGuardrails(effectiveGeneratorGuardrails(options.Guardrails)); err != nil {
		return err
	}
	if options.LongRun != options.StagingConfirmed {
		return fmt.Errorf("long-run campaigns require both --allow-long-run and --staging-confirmed")
	}
	observationLimit, soakLimit, budgetLimit := 10*time.Minute, 3*time.Minute, DefaultWallTimeLimit
	if options.LongRun {
		observationLimit, soakLimit, budgetLimit = LongRunPhaseLimit, LongRunPhaseLimit, LongRunWallTimeLimit
		if options.Budget == 0 {
			return fmt.Errorf("long-run campaigns require an explicit --budget (maximum 2h)")
		}
	}
	if err := ValidateStabilityPolicy(effectiveStabilityPolicy(options.Stability)); err != nil {
		return err
	}
	if options.Warmup < 0 || options.Warmup > 3*time.Minute || options.Recovery < 0 || options.Recovery > 3*time.Minute {
		return fmt.Errorf("warmup and recovery must be between 0 and 3m")
	}
	if options.Observation < 10*time.Second || options.Observation > observationLimit {
		return fmt.Errorf("observation must be between 10s and %s", observationLimit)
	}
	if options.Soak < 0 || options.Soak > soakLimit || (options.Soak > 0 && options.Soak < 10*time.Second) {
		return fmt.Errorf("soak must be between 10s and %s when enabled", soakLimit)
	}
	if options.Cooldown < 0 || options.Cooldown > 5*time.Minute {
		return fmt.Errorf("cooldown must be between 0 and 5m")
	}
	if (options.SpikeUsers == 0) != (options.SpikeDuration == 0) {
		return fmt.Errorf("spike-users and spike-duration must be set together")
	}
	if options.SpikeUsers < 0 || options.SpikeUsers > 50 || options.SpikeDuration < 0 || options.SpikeDuration > 3*time.Minute || (options.SpikeUsers > 0 && options.SpikeDuration < 10*time.Second) {
		return fmt.Errorf("spike-users must be 1-50 and spike-duration must be 10s-3m")
	}
	if options.SpikeDuration > 0 && options.Recovery == 0 {
		return fmt.Errorf("a spike requires a recovery phase so post-spike behavior is retained")
	}
	if options.Repeats < 2 || options.Repeats > 5 {
		return fmt.Errorf("repeats must be between 2 and 5")
	}
	minimum := time.Duration(options.Repeats)*(options.Warmup+options.Observation+options.SpikeDuration+options.Recovery) + options.Soak + time.Duration(options.Repeats-1)*options.Cooldown
	if options.Budget != 0 && (options.Budget < minimum || options.Budget > budgetLimit) {
		return fmt.Errorf("budget must cover every phase and be at most %s", budgetLimit)
	}
	if options.Budget != 0 && options.Soak > 0 && options.Budget < defaultBudget(options) {
		return fmt.Errorf("budget must reserve bounded drain allowance for every phase")
	}
	if options.LongRun && options.Budget < defaultBudget(options) {
		return fmt.Errorf("long-run budget must reserve bounded drain allowance for every phase")
	}
	if minimum > budgetLimit {
		return fmt.Errorf("configured phases exceed the %s campaign safety budget", budgetLimit)
	}
	if options.Budget == 0 && defaultBudget(options) > budgetLimit {
		return fmt.Errorf("configured phases and bounded drain allowance exceed the %s campaign safety budget", budgetLimit)
	}
	return nil
}

func effectiveGeneratorGuardrails(policy generatorhealth.Guardrails) generatorhealth.Guardrails {
	if policy.Enabled() && policy.ConsecutiveSamples == 0 {
		policy.ConsecutiveSamples = 2
	}
	return policy
}

func validateGeneratorGuardrails(policy generatorhealth.Guardrails) error {
	if !policy.Validate() {
		return fmt.Errorf("generator guardrails require a CPU limit from 1-100%% and/or an RSS limit, with 2-20 consecutive samples")
	}
	return nil
}

// ValidateForScenario rejects a policy that cannot receive its configured
// minimum number of windows from the observation and optional soak phases.
func ValidateForScenario(base config.Validated, options Options) error {
	if err := Validate(options); err != nil {
		return err
	}
	if options.LongRun && base.Scenario.Environment != "staging" {
		return fmt.Errorf("long-run campaigns require scenario environment to be explicitly declared as staging")
	}
	policy := effectiveStabilityPolicy(options.Stability)
	for _, duration := range []time.Duration{options.Observation, options.Soak} {
		if duration == 0 {
			continue
		}
		window := base.Window
		if window == 0 {
			window = min(30*time.Second, duration/4)
		}
		if window <= 0 || int(math.Ceil(duration.Seconds()/window.Seconds())) < policy.MinimumWindows {
			return fmt.Errorf("stability minimum-windows exceeds the observation or soak window coverage")
		}
		if options.LongRun && int(math.Ceil(duration.Seconds()/window.Seconds())) > MaxCampaignWindows {
			return fmt.Errorf("long-run observation and soak phases are limited to %d windows; increase window_duration", MaxCampaignWindows)
		}
	}
	return nil
}

func defaultBudget(options Options) time.Duration {
	phases := len(PlannedPhases(options, 0))
	soakDrain := time.Duration(0)
	if options.Soak > 0 {
		soakDrain = 30 * time.Second
	}
	return time.Duration(options.Repeats)*(options.Warmup+options.Observation+options.SpikeDuration+options.Recovery+time.Duration(phases)*30*time.Second) + options.Soak + soakDrain + time.Duration(options.Repeats-1)*options.Cooldown
}

// PlannedBudget returns the explicit wall-time cap or the bounded default.
func PlannedBudget(options Options) time.Duration {
	if options.Budget > 0 {
		return options.Budget
	}
	return defaultBudget(options)
}

func Run(ctx context.Context, base config.Validated, options Options) (Result, error) {
	return RunWithProgress(ctx, base, options, nil)
}

func WithRunner(ctx context.Context, base config.Validated, options Options, runner Runner) (Result, error) {
	return WithRunnerProgress(ctx, base, options, runner, nil)
}

// RunWithProgress executes native phases and reports durable snapshots after
// each completed phase, so a forced process stop does not erase prior evidence.
func RunWithProgress(ctx context.Context, base config.Validated, options Options, progress ProgressFunc) (Result, error) {
	return WithRunnerProgress(ctx, base, options, engine.Run, progress)
}

// WithRunnerProgress is WithRunner with an optional synchronous checkpoint
// callback. A callback error stops execution before another phase starts.
func WithRunnerProgress(ctx context.Context, base config.Validated, options Options, runner Runner, progress ProgressFunc) (Result, error) {
	if err := ValidateForScenario(base, options); err != nil {
		return Result{}, err
	}
	if options.SpikeUsers > 0 && options.SpikeUsers <= base.Scenario.Users {
		return Result{}, fmt.Errorf("spike-users must exceed the base scenario users")
	}
	var spikeCookies []string
	if options.SpikeUsers > 0 && base.Scenario.PerVUCookieEnvPrefix != "" {
		var err error
		spikeCookies, err = config.ResolveSessions(base.Scenario.PerVUCookieEnvPrefix, options.SpikeUsers)
		if err != nil {
			return Result{}, err
		}
	}
	options.Budget = PlannedBudget(options)
	options.Stability = effectiveStabilityPolicy(options.Stability)
	options.Guardrails = effectiveGeneratorGuardrails(options.Guardrails)
	stabilityPolicy := options.Stability
	var generatorGuardrails *generatorhealth.Guardrails
	if options.Guardrails.Enabled() {
		policy := options.Guardrails
		generatorGuardrails = &policy
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()
	soakDuration := ""
	if options.Soak > 0 {
		soakDuration = options.Soak.String()
	}
	cooldownDuration := options.Cooldown.String()
	result := Result{
		SchemaVersion:       CurrentSchemaVersion,
		ExecutionState:      "RUNNING",
		Scenario:            base.Scenario.Name,
		BaseUsers:           base.Scenario.Users,
		Target:              base.Base.Scheme + "://" + base.Base.Host,
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		Warmup:              options.Warmup.String(),
		Observation:         options.Observation.String(),
		SpikeUsers:          options.SpikeUsers,
		SpikeDuration:       options.SpikeDuration.String(),
		Recovery:            options.Recovery.String(),
		Soak:                soakDuration,
		Cooldown:            cooldownDuration,
		LongRun:             options.LongRun,
		DeclaredEnvironment: base.Scenario.Environment,
		StabilityPolicy:     &stabilityPolicy,
		GeneratorGuardrails: generatorGuardrails,
		Budget:              options.Budget.String(),
		Repeats:             options.Repeats,
		Trials:              []Trial{},
		Outcome:             "INCONCLUSIVE",
		Notes: []string{
			"Warm-up runs establish workload state but are excluded from capacity conclusions.",
			"Observation, configured spike, recovery, and the optional final soak all require PASS; this is evidence under the configured workload, not a universal capacity certification.",
		},
	}
	if options.LongRun {
		result.Notes = append(result.Notes, "Long-run mode was explicitly enabled for a user-declared staging environment with a bounded wall-time budget; Damocline does not verify the environment designation.")
	}
	checkpoint := func(trial Trial) error {
		if progress == nil {
			return nil
		}
		snapshot := result
		snapshot.Outcome = "INCONCLUSIVE"
		snapshot.ExecutionState = "RUNNING"
		trial.Outcome = "IN_PROGRESS"
		snapshot.PendingTrial = &trial
		if err := progress(snapshot); err != nil {
			return fmt.Errorf("persist campaign checkpoint: %w", err)
		}
		return nil
	}
	finish := func() error {
		result.ExecutionState = "COMPLETED"
		result.PendingTrial = nil
		if progress != nil {
			if err := progress(result); err != nil {
				return fmt.Errorf("persist final campaign report: %w", err)
			}
		}
		return nil
	}
	var pending *Trial
	for number := 1; number <= options.Repeats; number++ {
		var trial Trial
		if pending != nil {
			trial = *pending
			trial.Outcome = "PASS"
			pending = nil
		} else {
			runID, err := newRunID()
			if err != nil {
				return result, fmt.Errorf("create trial run ID: %w", err)
			}
			trial = Trial{RunID: runID, Number: number, Phases: []Phase{}, Outcome: "PASS"}
		}
		if err := checkpoint(trial); err != nil {
			return result, err
		}
		spikeFailed := false
		for _, phase := range PlannedTrialPhases(options, base.Scenario.Users, number) {
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				trial.AbortReason = abortReason(ctx.Err(), parent.Err())
				break
			}
			v := base
			v.GeneratorGuardrails = options.Guardrails
			v.Duration = phase.Duration
			v.Scenario.Duration = phase.Duration.String()
			v.Scenario.Users = phase.Users
			if phase.Name != "warmup" && v.Window == 0 && phase.Duration >= 4*time.Second {
				v.Window = min(30*time.Second, phase.Duration/4)
				v.Scenario.WindowDuration = v.Window.String()
			}
			if len(spikeCookies) > 0 && phase.Users == options.SpikeUsers {
				v.Cookies = spikeCookies
			}
			run := runner(ctx, v)
			state, reason := ClassifyEvidence(run)
			stability := ""
			if phase.Name == "observation" || phase.Name == "soak" {
				stability = AssessStabilityWithPolicy(run, options.Stability)
			}
			trial.Phases = append(trial.Phases, Phase{Name: phase.Name, EvidenceState: state, Reason: reason, Stability: stability, Run: run})
			if err := checkpoint(trial); err != nil {
				return result, err
			}
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				trial.AbortReason = abortReason(ctx.Err(), parent.Err())
				break
			}
			if state == "INVALID_DELIVERY" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if phase.Name == "spike" && run.Outcome == "FAIL" && state == "FAIL_OBSERVED" {
				spikeFailed = true
				if options.Recovery == 0 {
					trial.Outcome = "FAIL"
					break
				}
				continue
			}
			if (phase.Name == "observation" || phase.Name == "soak") && state == "PASS_OBSERVED" && stability != "NO_LARGE_DRIFT_DETECTED" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if run.Outcome == "ABORTED" || run.Outcome == "INCONCLUSIVE" {
				trial.Outcome = run.Outcome
				if run.Outcome == "ABORTED" {
					trial.AbortReason = "The native phase run was aborted."
					if run.Generator != nil && run.Generator.GuardrailTrigger != nil {
						trigger := run.Generator.GuardrailTrigger
						if trigger.Resource == "generator_process_cpu" {
							trial.AbortReason = fmt.Sprintf("Generator process CPU reached %.1f%% (limit %.1f%%) for %d consecutive samples.", trigger.ObservedCPUPercent, trigger.LimitCPUPercent, trigger.ConsecutiveSamples)
						} else {
							trial.AbortReason = fmt.Sprintf("Generator process RSS reached %d bytes (limit %d bytes) for %d consecutive samples.", trigger.ObservedRSSBytes, trigger.LimitRSSBytes, trigger.ConsecutiveSamples)
						}
					}
				}
				break
			}
			if phase.Required && run.Outcome != "PASS" {
				trial.Outcome = "FAIL"
				break
			}
			if phase.Name == "recovery" && spikeFailed {
				trial.Outcome = "FAIL"
				break
			}
		}
		result.Trials = append(result.Trials, trial)
		if trial.Outcome != "PASS" {
			result.Outcome = trial.Outcome
			if err := finish(); err != nil {
				return result, err
			}
			return result, nil
		}
		if number < options.Repeats {
			runID, err := newRunID()
			if err != nil {
				return result, fmt.Errorf("create trial run ID: %w", err)
			}
			next := Trial{RunID: runID, Number: number + 1, Phases: []Phase{}, Outcome: "IN_PROGRESS"}
			pending = &next
			if err := checkpoint(next); err != nil {
				return result, err
			}
		}
		if number < options.Repeats && options.Cooldown > 0 {
			timer := time.NewTimer(options.Cooldown)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	result.Outcome = "PASS"
	result.ObservationVariability = summarizeObservationVariability(result.Trials)
	if err := finish(); err != nil {
		return result, err
	}
	return result, nil
}

func newRunID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func abortReason(err, parentErr error) string {
	if parentErr == nil && err == context.DeadlineExceeded {
		return "The campaign wall-time budget expired."
	}
	return "The campaign was interrupted by its caller."
}

// AssessStability screens every complete observation window, including
// named HTTP steps and complete business journeys, for large latency spikes
// or throughput loss relative to the first window. Its deliberately coarse
// threshold is not a test of statistical stationarity.
func AssessStability(run report.Result) string {
	return AssessStabilityWithPolicy(run, DefaultStabilityPolicy())
}

// AssessStabilityWithPolicy applies the saved, bounded policy to every
// observation window and named series in a run.
func AssessStabilityWithPolicy(run report.Result, policy StabilityPolicy) string {
	if ValidateStabilityPolicy(policy) != nil || len(run.Windows) < policy.MinimumWindows || run.ObservationSeconds <= 0 || math.IsNaN(run.ObservationSeconds) || math.IsInf(run.ObservationSeconds, 0) {
		return "INSUFFICIENT_WINDOWS"
	}
	expectedStart := 0.0
	first := run.Windows[0]
	for _, window := range run.Windows {
		if math.IsNaN(window.StartSeconds) || math.IsInf(window.StartSeconds, 0) || math.IsNaN(window.EndSeconds) || math.IsInf(window.EndSeconds, 0) ||
			math.Abs(window.StartSeconds-expectedStart) > 0.001 || window.EndSeconds <= window.StartSeconds ||
			window.EndSeconds > run.ObservationSeconds+0.001 || !stableScreenMetric(window.Metrics, policy.MinimumSamplesPerWindow) || !stableScreenMetric(window.Journeys, policy.MinimumSamplesPerWindow) {
			return "INSUFFICIENT_WINDOWS"
		}
		if len(window.Steps) != len(first.Steps) || len(window.Profiles) != len(first.Profiles) {
			return "INSUFFICIENT_WINDOWS"
		}
		if !stableNamedWindows(first.Steps, window.Steps, first.EndSeconds-first.StartSeconds, window.EndSeconds-window.StartSeconds, policy.MinimumSamplesPerWindow) ||
			!stableNamedWindows(first.Profiles, window.Profiles, first.EndSeconds-first.StartSeconds, window.EndSeconds-window.StartSeconds, policy.MinimumSamplesPerWindow) {
			return "INSUFFICIENT_WINDOWS"
		}
		expectedStart = window.EndSeconds
	}
	if math.Abs(expectedStart-run.ObservationSeconds) > 0.001 {
		return "INSUFFICIENT_WINDOWS"
	}
	firstDuration := first.EndSeconds - first.StartSeconds
	for _, window := range run.Windows[1:] {
		duration := window.EndSeconds - window.StartSeconds
		if largeWindowDrift(first.Metrics, window.Metrics, firstDuration, duration, policy) ||
			largeWindowDrift(first.Journeys, window.Journeys, firstDuration, duration, policy) {
			return "NON_STATIONARY"
		}
		for _, pair := range []struct{ baseline, observed []report.WindowMetric }{
			{first.Steps, window.Steps},
			{first.Profiles, window.Profiles},
		} {
			byName := make(map[string]report.MetricSet, len(pair.baseline))
			for _, entry := range pair.baseline {
				byName[entry.Name] = entry.Metrics
			}
			for _, entry := range pair.observed {
				if largeWindowDrift(byName[entry.Name], entry.Metrics, firstDuration, duration, policy) {
					return "NON_STATIONARY"
				}
			}
		}
	}
	return "NO_LARGE_DRIFT_DETECTED"
}

func stableScreenMetric(m report.MetricSet, minimumSamples int) bool {
	return m.Total >= minimumSamples && m.Success > 0 && m.Success <= m.Total &&
		m.Failures == m.Total-m.Success && m.Cancelled == 0 &&
		!math.IsNaN(m.P95MS) && !math.IsInf(m.P95MS, 0) && m.P95MS >= 0
}

// stableNamedWindows requires name uniqueness and at least 20 completed
// requests/journeys per named series in every compared window. Low-sample
// minority workflows must not silently disappear into an aggregate PASS.
func stableNamedWindows(base, observed []report.WindowMetric, baseDuration, duration float64, minimumSamples int) bool {
	if baseDuration <= 0 || duration <= 0 || len(base) != len(observed) {
		return false
	}
	names := make(map[string]bool, len(base))
	for _, item := range base {
		if item.Name == "" || names[item.Name] || !stableScreenMetric(item.Metrics, minimumSamples) {
			return false
		}
		names[item.Name] = true
	}
	seen := make(map[string]bool, len(observed))
	for _, item := range observed {
		if !names[item.Name] || seen[item.Name] || !stableScreenMetric(item.Metrics, minimumSamples) {
			return false
		}
		seen[item.Name] = true
	}
	return true
}

func largeWindowDrift(first, observed report.MetricSet, firstDuration, duration float64, policy StabilityPolicy) bool {
	firstRate := float64(first.Total) / firstDuration
	currentRate := float64(observed.Total) / duration
	latencyThreshold := first.P95MS * (1 + policy.MaxP95IncreasePercent/100)
	throughputFloor := firstRate * (1 - policy.MaxThroughputDropPercent/100)
	return (observed.P95MS > latencyThreshold && observed.P95MS-first.P95MS > policy.MaxP95IncreaseMS) || currentRate < throughputFloor
}

// ClassifyEvidence separates observed SLO results from invalid delivery and
// incomplete phase evidence. It does not certify sustainable capacity.
func ClassifyEvidence(run report.Result) (string, string) {
	if run.Quality != nil && run.Quality.State == report.QualityInvalid {
		return "INVALID_DELIVERY", "Report accounting or generator evidence is invalid."
	}
	if (run.Outcome == "PASS" || run.Outcome == "FAIL") && run.SchemaVersion >= report.CurrentSchemaVersion {
		if reason := report.GeneratorCapacityIssue(run); reason != "" {
			return "INVALID_DELIVERY", reason
		}
	}
	if run.ArrivalsDropped > 0 || run.ArrivalsCancelledBeforeAdmission != nil && *run.ArrivalsCancelledBeforeAdmission > 0 {
		return "INVALID_DELIVERY", "Planned arrivals were dropped or cancelled before admission."
	}
	if run.SchemaVersion >= 22 && run.Outcome == "PASS" {
		if run.Config.P95LimitMS <= 0 || run.Config.MaxErrorRate < 0 || run.Config.MaxErrorRate >= 1 || run.Journeys.Total <= 0 {
			return "INSUFFICIENT_EVIDENCE", "Completed-journey SLO evidence is absent or not statistically assessable."
		}
		journeyErrorRate := float64(run.Journeys.Failures) / float64(run.Journeys.Total)
		if run.Journeys.Success == 0 || journeyErrorRate > run.Config.MaxErrorRate || run.Journeys.P95MS > run.Config.P95LimitMS {
			return "FAIL_OBSERVED", "The completed-journey latency or error objective was not met."
		}
		for _, window := range run.Windows {
			if window.Journeys.Total <= 0 {
				return "INSUFFICIENT_EVIDENCE", "One or more observation windows have no completed-journey SLO evidence."
			}
			windowErrorRate := float64(window.Journeys.Failures) / float64(window.Journeys.Total)
			if window.Journeys.Success == 0 || windowErrorRate > run.Config.MaxErrorRate || window.Journeys.P95MS > run.Config.P95LimitMS {
				return "FAIL_OBSERVED", "A completed-journey observation-window objective was not met."
			}
		}
	}
	switch run.Outcome {
	case "PASS":
		return "PASS_OBSERVED", "Configured objectives passed in the observed phase."
	case "FAIL":
		return "FAIL_OBSERVED", "A configured objective failed in the observed phase."
	default:
		return "INSUFFICIENT_EVIDENCE", "The phase did not complete with a decisive observed outcome."
	}
}

func summarizeObservationVariability(trials []Trial) *Variability {
	summary := Variability{P95MinMS: math.Inf(1), ErrorRateMin: math.Inf(1), ThroughputMinRPS: math.Inf(1), SuccessfulJourneysMinPerSec: math.Inf(1)}
	for _, trial := range trials {
		for _, phase := range trial.Phases {
			if phase.Name != "observation" || phase.Run.WindowSeconds() <= 0 {
				continue
			}
			summary.Runs++
			summary.P95MinMS = math.Min(summary.P95MinMS, phase.Run.Metrics.P95MS)
			summary.P95MaxMS = math.Max(summary.P95MaxMS, phase.Run.Metrics.P95MS)
			summary.ErrorRateMin = math.Min(summary.ErrorRateMin, phase.Run.ErrorsRate)
			summary.ErrorRateMax = math.Max(summary.ErrorRateMax, phase.Run.ErrorsRate)
			throughput := float64(phase.Run.Metrics.Total) / phase.Run.WindowSeconds()
			summary.ThroughputMinRPS = math.Min(summary.ThroughputMinRPS, throughput)
			summary.ThroughputMaxRPS = math.Max(summary.ThroughputMaxRPS, throughput)
			journeyThroughput := report.SuccessfulJourneysPerSecond(phase.Run)
			summary.SuccessfulJourneysMinPerSec = math.Min(summary.SuccessfulJourneysMinPerSec, journeyThroughput)
			summary.SuccessfulJourneysMaxPerSec = math.Max(summary.SuccessfulJourneysMaxPerSec, journeyThroughput)
		}
	}
	if summary.Runs == 0 {
		return nil
	}
	return &summary
}
