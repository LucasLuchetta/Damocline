// Package arrivalcampaign runs repeated, bounded phases at open-loop
// scheduled journey-start rates. Rates remain distinct from VUs and requests/s.
package arrivalcampaign

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const CurrentSchemaVersion = 7
const maximumCampaignDuration = 15 * time.Minute
const LongRunWallTimeLimit = 2 * time.Hour
const LongRunPhaseLimit = 30 * time.Minute
const MaxCampaignWindows = 120
const maximumPhaseDrain = 30 * time.Second
const generatorUtilizationGuard = 0.90

type Options struct {
	Warmup           time.Duration
	Stabilization    time.Duration
	Observation      time.Duration
	SpikeRate        int
	SpikeDuration    time.Duration
	Recovery         time.Duration
	Soak             time.Duration
	Cooldown         time.Duration
	Repeats          int
	Budget           time.Duration
	Guardrails       generatorhealth.Guardrails
	PeakRate         int
	GrowthPercent    float64
	ReservePercent   float64
	LongRun          bool
	StagingConfirmed bool
}

type PlannedPhase struct {
	Name     string
	Duration time.Duration
	Rate     int
	Required bool
}

func PlannedPhases(options Options, baseRate int, trial int) []PlannedPhase {
	phases := make([]PlannedPhase, 0, 6)
	if options.Warmup > 0 {
		phases = append(phases, PlannedPhase{Name: "warmup", Duration: options.Warmup, Rate: baseRate})
	}
	if options.Stabilization > 0 {
		phases = append(phases, PlannedPhase{Name: "stabilization", Duration: options.Stabilization, Rate: baseRate, Required: true})
	}
	phases = append(phases, PlannedPhase{Name: "observation", Duration: options.Observation, Rate: baseRate, Required: true})
	if options.SpikeDuration > 0 {
		phases = append(phases, PlannedPhase{Name: "spike", Duration: options.SpikeDuration, Rate: options.SpikeRate, Required: true})
	}
	phases = append(phases, PlannedPhase{Name: "recovery", Duration: options.Recovery, Rate: baseRate, Required: true})
	if options.Soak > 0 && trial == options.Repeats {
		phases = append(phases, PlannedPhase{Name: "soak", Duration: options.Soak, Rate: baseRate, Required: true})
	}
	return phases
}

type Phase struct {
	Name          string        `json:"name"`
	EvidenceState string        `json:"evidence_state"`
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
	SchemaVersion        int                         `json:"arrival_campaign_schema_version"`
	ExecutionState       string                      `json:"execution_state,omitempty"`
	Scenario             string                      `json:"scenario"`
	Target               string                      `json:"target"`
	StartedAt            string                      `json:"started_at"`
	WorkerLimit          int                         `json:"journey_worker_limit"`
	WorkloadFingerprint  string                      `json:"workload_fingerprint"`
	RequestTimeout       string                      `json:"request_timeout"`
	RequestRateCeiling   int                         `json:"max_rps"`
	P95LimitMS           float64                     `json:"p95_limit_ms"`
	MaxErrorRate         float64                     `json:"max_error_rate"`
	MinSamplesPerStep    int                         `json:"min_samples_per_step"`
	WindowDuration       string                      `json:"window_duration"`
	BaseStartsPerSecond  int                         `json:"base_scheduled_journey_starts_per_second"`
	PeakStartsPerSecond  int                         `json:"peak_scheduled_journey_starts_per_second,omitempty"`
	GrowthPercent        float64                     `json:"growth_percent,omitempty"`
	ReservePercent       float64                     `json:"reserve_percent,omitempty"`
	Warmup               string                      `json:"warmup"`
	Stabilization        string                      `json:"stabilization"`
	Observation          string                      `json:"observation"`
	SpikeStartsPerSecond int                         `json:"spike_scheduled_journey_starts_per_second,omitempty"`
	SpikeDuration        string                      `json:"spike_duration,omitempty"`
	Recovery             string                      `json:"recovery"`
	Soak                 string                      `json:"soak,omitempty"`
	Cooldown             string                      `json:"cooldown,omitempty"`
	LongRun              bool                        `json:"long_run,omitempty"`
	DeclaredEnvironment  string                      `json:"declared_environment,omitempty"`
	GeneratorGuardrails  *generatorhealth.Guardrails `json:"generator_guardrails,omitempty"`
	Budget               string                      `json:"budget"`
	Repeats              int                         `json:"repeats"`
	Trials               []Trial                     `json:"trials"`
	PendingTrial         *Trial                      `json:"pending_trial,omitempty"`
	Outcome              string                      `json:"outcome"`
	Notes                []string                    `json:"notes"`
}

type Runner func(context.Context, config.Validated) report.Result

func RequiredStartsPerSecond(peak int, growth, reserve float64) (int, error) {
	if peak < 1 || peak > 100 || math.IsNaN(growth) || math.IsInf(growth, 0) || growth < 0 || growth > 1000 ||
		math.IsNaN(reserve) || math.IsInf(reserve, 0) || reserve < 0 || reserve > 1000 {
		return 0, fmt.Errorf("peak journey starts/s must be 1-100 and growth/reserve must be finite values from 0 to 1000")
	}
	value := math.Ceil(float64(peak) * (1 + growth/100) * (1 + reserve/100))
	if value > 100 {
		return 0, fmt.Errorf("calculated journey-start demand exceeds the 100 starts/s safety ceiling")
	}
	return int(value), nil
}

func Validate(base config.Validated, options Options) error {
	if err := validateGeneratorGuardrails(effectiveGeneratorGuardrails(options.Guardrails)); err != nil {
		return err
	}
	scenario := base.Scenario
	if options.LongRun != options.StagingConfirmed {
		return fmt.Errorf("long-run campaigns require both --allow-long-run and --staging-confirmed")
	}
	phaseLimit, campaignLimit := 10*time.Minute, maximumCampaignDuration
	soakLimit := 3 * time.Minute
	if options.LongRun {
		phaseLimit, campaignLimit = LongRunPhaseLimit, LongRunWallTimeLimit
		soakLimit = LongRunPhaseLimit
		if options.Budget == 0 {
			return fmt.Errorf("long-run campaigns require an explicit --budget (maximum 2h)")
		}
		if scenario.Environment != "staging" {
			return fmt.Errorf("long-run campaigns require scenario environment to be explicitly declared as staging")
		}
	}
	if scenario.Mode != "arrival" {
		return fmt.Errorf("arrival campaign requires mode=arrival; closed-loop workloads use campaign")
	}
	if scenario.ArrivalRate < 1 || scenario.ArrivalRate > 100 || scenario.Users < 1 || scenario.Users > 50 {
		return fmt.Errorf("arrival campaign base rate must be 1-100 starts/s and worker limit 1-50")
	}
	if options.PeakRate > 0 {
		target, err := RequiredStartsPerSecond(options.PeakRate, options.GrowthPercent, options.ReservePercent)
		if err != nil {
			return err
		}
		if target != scenario.ArrivalRate {
			return fmt.Errorf("scenario arrival_rate %d/s must equal the calculated demand target %d/s", scenario.ArrivalRate, target)
		}
	} else if options.GrowthPercent != 0 || options.ReservePercent != 0 {
		return fmt.Errorf("growth and reserve require a positive peak journey-start demand")
	}
	if options.Warmup < 0 || options.Warmup > 3*time.Minute || options.Recovery < 10*time.Second || options.Recovery > 3*time.Minute {
		return fmt.Errorf("warmup must be 0-3m and recovery must be 10s-3m")
	}
	stabilizationLimit := 3 * time.Minute
	if options.LongRun {
		stabilizationLimit = LongRunPhaseLimit
	}
	if options.Stabilization < 0 || options.Stabilization > stabilizationLimit || options.Stabilization > 0 && options.Stabilization < 10*time.Second {
		return fmt.Errorf("stabilization must be between 10s and %s when enabled", stabilizationLimit)
	}
	if options.Observation < 10*time.Second || options.Observation > phaseLimit {
		return fmt.Errorf("observation must be 10s-%s", phaseLimit)
	}
	if (options.SpikeRate == 0) != (options.SpikeDuration == 0) || options.SpikeRate < 0 || options.SpikeRate > 100 ||
		(options.SpikeDuration > 0 && (options.SpikeDuration < 10*time.Second || options.SpikeDuration > 3*time.Minute)) {
		return fmt.Errorf("spike starts/s and spike-duration must be set together; rate is 1-100 and duration is 10s-3m")
	}
	if options.SpikeRate > 0 && options.SpikeRate <= scenario.ArrivalRate {
		return fmt.Errorf("spike journey-start rate must exceed the base rate")
	}
	if options.Soak < 0 || options.Soak > soakLimit || (options.Soak > 0 && options.Soak < 10*time.Second) {
		return fmt.Errorf("soak must be 10s-%s when enabled", soakLimit)
	}
	if options.Cooldown < 0 || options.Cooldown > 5*time.Minute {
		return fmt.Errorf("cooldown must be between 0 and 5m")
	}
	if options.Repeats < 2 || options.Repeats > 5 {
		return fmt.Errorf("repeats must be 2-5")
	}
	steps := 0
	if len(scenario.Profiles) > 0 {
		for _, profile := range scenario.Profiles {
			steps = max(steps, len(profile.Steps))
		}
	} else {
		steps = len(scenario.Steps)
	}
	if steps < 1 || max(scenario.ArrivalRate, options.SpikeRate)*steps > scenario.MaxRPS {
		return fmt.Errorf("base or spike starts/s multiplied by the longest journey exceeds max_rps")
	}
	window := base.Window
	if scenario.WindowDuration != "" {
		parsed, err := time.ParseDuration(scenario.WindowDuration)
		if err != nil || parsed < time.Second || parsed > time.Minute {
			return fmt.Errorf("window_duration must be between 1s and 1m")
		}
		window = parsed
	}
	if window == 0 {
		shortest := min(options.Observation, options.Recovery)
		if options.Stabilization > 0 {
			shortest = min(shortest, options.Stabilization)
		}
		if options.Soak > 0 {
			shortest = min(shortest, options.Soak)
		}
		window = min(30*time.Second, shortest/4)
		if options.LongRun {
			longestEvaluated := max(options.Stabilization, max(options.Observation, options.Soak))
			minimumWindow := (longestEvaluated + MaxCampaignWindows - 1) / MaxCampaignWindows
			window = max(window, minimumWindow)
		}
	}
	for _, duration := range []time.Duration{options.Stabilization, options.Observation, options.Soak} {
		if duration > 0 && duration/window < 1 {
			return fmt.Errorf("configured observation window must not exceed an evaluated phase duration")
		}
	}
	if options.Observation/window < 3 || options.Stabilization > 0 && options.Stabilization/window < 3 || options.Soak > 0 && options.Soak/window < 3 {
		return fmt.Errorf("stabilization, observation and soak require at least three complete configured windows")
	}
	if options.LongRun && (int(math.Ceil(options.Observation.Seconds()/window.Seconds())) > MaxCampaignWindows || options.Stabilization > 0 && int(math.Ceil(options.Stabilization.Seconds()/window.Seconds())) > MaxCampaignWindows || options.Soak > 0 && int(math.Ceil(options.Soak.Seconds()/window.Seconds())) > MaxCampaignWindows) {
		return fmt.Errorf("long-run stabilization, observation and soak phases are limited to %d windows; increase window_duration", MaxCampaignWindows)
	}
	phases := 2
	if options.Warmup > 0 {
		phases++
	}
	if options.SpikeDuration > 0 {
		phases++
	}
	phases += boolInt(options.Stabilization > 0)
	total := time.Duration(options.Repeats)*(options.Warmup+options.Stabilization+options.Observation+options.SpikeDuration+options.Recovery) + options.Soak + time.Duration(options.Repeats-1)*options.Cooldown
	withDrain := total + time.Duration(options.Repeats*phases+boolInt(options.Soak > 0))*maximumPhaseDrain
	if total > campaignLimit || withDrain > campaignLimit || options.Budget != 0 && (options.Budget < withDrain || options.Budget > campaignLimit) {
		return fmt.Errorf("campaign phases, cooldowns and bounded drains must fit the %s wall-time safety budget", campaignLimit)
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

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func PlannedBudget(options Options) time.Duration {
	requested := options.Budget
	phases := 2 + boolInt(options.Warmup > 0) + boolInt(options.SpikeDuration > 0) + boolInt(options.Stabilization > 0)
	minimum := time.Duration(options.Repeats)*(options.Warmup+options.Stabilization+options.Observation+options.SpikeDuration+options.Recovery) + options.Soak + time.Duration(options.Repeats-1)*options.Cooldown
	withDrain := minimum +
		time.Duration(options.Repeats*phases+boolInt(options.Soak > 0))*maximumPhaseDrain
	if requested > withDrain {
		return requested
	}
	return withDrain
}

func Run(ctx context.Context, base config.Validated, options Options) (Result, error) {
	return RunWithProgress(ctx, base, options, nil)
}

func WithRunner(parent context.Context, base config.Validated, options Options, runner Runner) (Result, error) {
	return WithRunnerProgress(parent, base, options, runner, nil)
}

// RunWithProgress executes native phases and reports durable snapshots after
// each completed phase, so a forced process stop does not erase prior evidence.
func RunWithProgress(ctx context.Context, base config.Validated, options Options, progress ProgressFunc) (Result, error) {
	return WithRunnerProgress(ctx, base, options, engine.Run, progress)
}

// WithRunnerProgress is WithRunner with an optional synchronous checkpoint
// callback. A callback error stops execution before another phase starts.
func WithRunnerProgress(parent context.Context, base config.Validated, options Options, runner Runner, progress ProgressFunc) (Result, error) {
	if err := Validate(base, options); err != nil {
		return Result{}, err
	}
	options.Budget = PlannedBudget(options)
	options.Guardrails = effectiveGeneratorGuardrails(options.Guardrails)
	var generatorGuardrails *generatorhealth.Guardrails
	if options.Guardrails.Enabled() {
		policy := options.Guardrails
		generatorGuardrails = &policy
	}
	ctx, cancel := context.WithTimeout(parent, options.Budget)
	defer cancel()
	result := Result{
		SchemaVersion: CurrentSchemaVersion, ExecutionState: "RUNNING", Scenario: base.Scenario.Name, Target: base.Base.Scheme + "://" + base.Base.Host,
		WorkloadFingerprint: base.WorkloadFingerprint, RequestTimeout: base.Scenario.Timeout, RequestRateCeiling: base.Scenario.MaxRPS,
		P95LimitMS: base.Scenario.P95LimitMS, MaxErrorRate: base.Scenario.MaxErrorRate, MinSamplesPerStep: base.Scenario.MinSamplesPerStep,
		WindowDuration: base.Scenario.WindowDuration, Budget: PlannedBudget(options).String(),
		StartedAt: time.Now().UTC().Format(time.RFC3339), WorkerLimit: base.Scenario.Users, BaseStartsPerSecond: base.Scenario.ArrivalRate,
		PeakStartsPerSecond: options.PeakRate, GrowthPercent: options.GrowthPercent, ReservePercent: options.ReservePercent,
		Warmup: options.Warmup.String(), Stabilization: options.Stabilization.String(), Observation: options.Observation.String(), SpikeStartsPerSecond: options.SpikeRate,
		SpikeDuration: options.SpikeDuration.String(), Recovery: options.Recovery.String(), Soak: options.Soak.String(),
		Cooldown: options.Cooldown.String(),
		LongRun:  options.LongRun, DeclaredEnvironment: base.Scenario.Environment,
		GeneratorGuardrails: generatorGuardrails,
		Repeats:             options.Repeats, Trials: []Trial{}, Outcome: "INCONCLUSIVE",
		Notes: []string{
			"All phase rates are scheduled open-loop journey starts/s. They are not VUs, scheduler admissions, completed journeys/s, or HTTP requests/s.",
			"Stabilization and observation are bounded repeated load stages with configured SLO windows. Passing phases do not establish universal or long-term sustainable capacity.",
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
			return fmt.Errorf("persist arrival campaign checkpoint: %w", err)
		}
		return nil
	}
	finish := func() error {
		result.ExecutionState = "COMPLETED"
		result.PendingTrial = nil
		if progress != nil {
			if err := progress(result); err != nil {
				return fmt.Errorf("persist final arrival campaign report: %w", err)
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
		for _, phase := range PlannedPhases(options, base.Scenario.ArrivalRate, number) {
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				trial.AbortReason = abortReason(ctx.Err(), parent.Err())
				break
			}
			validated := base
			validated.GeneratorGuardrails = options.Guardrails
			validated.Scenario.ArrivalRate = phase.Rate
			validated.Scenario.Duration = phase.Duration.String()
			validated.Scenario.RampUp = "0s"
			validated.Duration = phase.Duration
			validated.RampUp = 0
			if phase.Name != "warmup" {
				if validated.Window == 0 {
					validated.Window = min(30*time.Second, phase.Duration/4)
				}
				if validated.Window > phase.Duration {
					validated.Window = phase.Duration
				}
			}
			if phase.Name != "warmup" {
				validated.Scenario.WindowDuration = validated.Window.String()
				if phase.Name == "observation" {
					result.WindowDuration = validated.Window.String()
				}
			}
			run := runner(ctx, validated)
			state, reason := ClassifyEvidence(run)
			stability := ""
			if phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak" {
				stability = campaign.AssessStability(run)
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
			if state == "INVALID_DELIVERY" || state == "INSUFFICIENT_EVIDENCE" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if phase.Name == "spike" && run.Outcome == "FAIL" && state == "FAIL_OBSERVED" {
				spikeFailed = true
				continue
			}
			if (phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak") && state == "PASS_OBSERVED" && stability != "NO_LARGE_DRIFT_DETECTED" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if (phase.Name == "observation" || phase.Name == "soak") && state == "FAIL_OBSERVED" {
				trial.Outcome = "FAIL"
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
			if trial.Outcome == "FAIL" {
				result.Outcome = "FAIL"
			} else {
				result.Outcome = trial.Outcome
			}
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
		return "The arrival campaign wall-time budget expired."
	}
	return "The arrival campaign was interrupted by its caller."
}

func ClassifyEvidence(run report.Result) (string, string) {
	if run.Outcome == "ABORTED" {
		return "ABORTED", "The native arrival phase was aborted."
	}
	if run.Quality != nil && run.Quality.State == report.QualityInvalid || run.SchemaVersion >= 16 && report.AssessQuality(run).State == report.QualityInvalid {
		return "INVALID_DELIVERY", "Report accounting or generator evidence is invalid."
	}
	if run.ArrivalsDropped > 0 || run.ArrivalsDroppedLate > 0 || run.ArrivalsDroppedBusy > 0 ||
		run.ArrivalsCancelledBeforeAdmission != nil && *run.ArrivalsCancelledBeforeAdmission > 0 || run.Metrics.Cancelled > 0 || run.Journeys.Cancelled > 0 {
		return "INVALID_DELIVERY", "Scheduled arrivals, requests, or journeys were dropped or cancelled."
	}
	if run.Outcome == "INCONCLUSIVE" {
		return "INSUFFICIENT_EVIDENCE", "The native arrival phase did not establish a conclusive SLO result."
	}
	if run.Metrics.Total <= 0 || run.Config.MaxRPS <= 0 || run.WindowSeconds() <= 0 || run.Generator == nil ||
		run.Generator.Samples < 2 || !run.Generator.GoCPUValid || run.Generator.ProcessSamples < 2 || !run.Generator.ProcessCPUValid {
		return "INSUFFICIENT_EVIDENCE", "Arrival delivery or generator health evidence is incomplete."
	}
	if run.Generator.GoCPUUtilizationRatio >= generatorUtilizationGuard || run.Generator.MeanProcessCPUUtilizationRatio >= generatorUtilizationGuard ||
		float64(run.Metrics.Total)/run.WindowSeconds() >= generatorUtilizationGuard*float64(run.Config.MaxRPS) {
		return "INVALID_DELIVERY", "Generator CPU or the configured client request ceiling may limit the offered arrival rate."
	}
	return campaign.ClassifyEvidence(run)
}

func AssessStability(run report.Result) string { return campaign.AssessStability(run) }
