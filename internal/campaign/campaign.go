// Package campaign runs bounded multi-phase capacity observations.
package campaign

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const CurrentSchemaVersion = 2

type Options struct {
	Warmup        time.Duration
	Observation   time.Duration
	SpikeUsers    int
	SpikeDuration time.Duration
	Recovery      time.Duration
	Repeats       int
	Budget        time.Duration
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

type Phase struct {
	Name          string        `json:"name"`
	EvidenceState string        `json:"evidence_state,omitempty"`
	Reason        string        `json:"reason,omitempty"`
	Stability     string        `json:"stability,omitempty"`
	Run           report.Result `json:"run"`
}

type Trial struct {
	Number      int     `json:"number"`
	Phases      []Phase `json:"phases"`
	Outcome     string  `json:"outcome"`
	AbortReason string  `json:"abort_reason,omitempty"`
}

type Result struct {
	SchemaVersion          int          `json:"schema_version"`
	Scenario               string       `json:"scenario"`
	BaseUsers              int          `json:"base_users"`
	Target                 string       `json:"target"`
	StartedAt              string       `json:"started_at"`
	Warmup                 string       `json:"warmup"`
	Observation            string       `json:"observation"`
	SpikeUsers             int          `json:"spike_users,omitempty"`
	SpikeDuration          string       `json:"spike_duration,omitempty"`
	Recovery               string       `json:"recovery"`
	Budget                 string       `json:"budget,omitempty"`
	Repeats                int          `json:"repeats"`
	Trials                 []Trial      `json:"trials"`
	ObservationVariability *Variability `json:"observation_variability,omitempty"`
	Outcome                string       `json:"outcome"`
	Notes                  []string     `json:"notes"`
}

// Variability summarizes the spread observed across independent observation
// phases. It is descriptive and does not claim a confidence interval.
type Variability struct {
	Runs             int     `json:"runs"`
	P95MinMS         float64 `json:"p95_min_ms"`
	P95MaxMS         float64 `json:"p95_max_ms"`
	ErrorRateMin     float64 `json:"error_rate_min"`
	ErrorRateMax     float64 `json:"error_rate_max"`
	ThroughputMinRPS float64 `json:"throughput_min_rps"`
	ThroughputMaxRPS float64 `json:"throughput_max_rps"`
}

type Runner func(context.Context, config.Validated) report.Result

func Validate(options Options) error {
	if options.Warmup < 0 || options.Warmup > 3*time.Minute || options.Recovery < 0 || options.Recovery > 3*time.Minute {
		return fmt.Errorf("warmup and recovery must be between 0 and 3m")
	}
	if options.Observation < 10*time.Second || options.Observation > 10*time.Minute {
		return fmt.Errorf("observation must be between 10s and 10m")
	}
	if (options.SpikeUsers == 0) != (options.SpikeDuration == 0) {
		return fmt.Errorf("spike-users and spike-duration must be set together")
	}
	if options.SpikeUsers < 0 || options.SpikeUsers > 50 || options.SpikeDuration < 0 || options.SpikeDuration > 3*time.Minute || (options.SpikeUsers > 0 && options.SpikeDuration < 10*time.Second) {
		return fmt.Errorf("spike-users must be 1-50 and spike-duration must be 10s-3m")
	}
	if options.Repeats < 2 || options.Repeats > 5 {
		return fmt.Errorf("repeats must be between 2 and 5")
	}
	minimum := time.Duration(options.Repeats) * (options.Warmup + options.Observation + options.SpikeDuration + options.Recovery)
	if options.Budget != 0 && (options.Budget < minimum || options.Budget > 15*time.Minute) {
		return fmt.Errorf("budget must cover every phase and be at most 15m")
	}
	if minimum > 15*time.Minute {
		return fmt.Errorf("configured phases exceed the 15m campaign safety budget")
	}
	if options.Budget == 0 && defaultBudget(options) > 15*time.Minute {
		return fmt.Errorf("configured phases and bounded drain allowance exceed the 15m campaign safety budget")
	}
	return nil
}

func defaultBudget(options Options) time.Duration {
	return time.Duration(options.Repeats) * (options.Warmup + options.Observation + options.SpikeDuration + options.Recovery + time.Duration(len(PlannedPhases(options, 0)))*30*time.Second)
}

// PlannedBudget returns the explicit wall-time cap or the bounded default.
func PlannedBudget(options Options) time.Duration {
	if options.Budget > 0 {
		return options.Budget
	}
	return defaultBudget(options)
}

func Run(ctx context.Context, base config.Validated, options Options) (Result, error) {
	return WithRunner(ctx, base, options, engine.Run)
}

func WithRunner(ctx context.Context, base config.Validated, options Options, runner Runner) (Result, error) {
	if err := Validate(options); err != nil {
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
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()
	result := Result{
		SchemaVersion: CurrentSchemaVersion,
		Scenario:      base.Scenario.Name,
		BaseUsers:     base.Scenario.Users,
		Target:        base.Base.Scheme + "://" + base.Base.Host,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Warmup:        options.Warmup.String(),
		Observation:   options.Observation.String(),
		SpikeUsers:    options.SpikeUsers,
		SpikeDuration: options.SpikeDuration.String(),
		Recovery:      options.Recovery.String(),
		Budget:        options.Budget.String(),
		Repeats:       options.Repeats,
		Trials:        []Trial{},
		Outcome:       "INCONCLUSIVE",
		Notes: []string{
			"Warm-up runs establish workload state but are excluded from capacity conclusions.",
			"Observation, configured spike, and recovery all require PASS in every independent trial; this is evidence under the configured workload, not a universal capacity certification.",
		},
	}
	for number := 1; number <= options.Repeats; number++ {
		trial := Trial{Number: number, Phases: []Phase{}, Outcome: "PASS"}
		for _, phase := range PlannedPhases(options, base.Scenario.Users) {
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				trial.AbortReason = abortReason(ctx.Err(), parent.Err())
				break
			}
			v := base
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
			if phase.Name == "observation" {
				stability = AssessStability(run)
			}
			trial.Phases = append(trial.Phases, Phase{Name: phase.Name, EvidenceState: state, Reason: reason, Stability: stability, Run: run})
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				trial.AbortReason = abortReason(ctx.Err(), parent.Err())
				break
			}
			if state == "INVALID_DELIVERY" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if phase.Name == "observation" && state == "PASS_OBSERVED" && stability != "NO_LARGE_DRIFT_DETECTED" {
				trial.Outcome = "INCONCLUSIVE"
				break
			}
			if run.Outcome == "ABORTED" || run.Outcome == "INCONCLUSIVE" {
				trial.Outcome = run.Outcome
				if run.Outcome == "ABORTED" {
					trial.AbortReason = "The native phase run was aborted."
				}
				break
			}
			if phase.Required && run.Outcome != "PASS" {
				trial.Outcome = "FAIL"
				break
			}
		}
		result.Trials = append(result.Trials, trial)
		if trial.Outcome != "PASS" {
			result.Outcome = trial.Outcome
			return result, nil
		}
	}
	result.Outcome = "PASS"
	result.ObservationVariability = summarizeObservationVariability(result.Trials)
	return result, nil
}

func abortReason(err, parentErr error) string {
	if parentErr == nil && err == context.DeadlineExceeded {
		return "The campaign wall-time budget expired."
	}
	return "The campaign was interrupted by its caller."
}

// AssessStability screens complete observation windows for a large first-to-last
// latency increase or throughput decrease. Passing this heuristic does not
// establish statistical stationarity or independent observations.
func AssessStability(run report.Result) string {
	if len(run.Windows) < 3 || run.ObservationSeconds <= 0 || math.IsNaN(run.ObservationSeconds) || math.IsInf(run.ObservationSeconds, 0) {
		return "INSUFFICIENT_WINDOWS"
	}
	expectedStart := 0.0
	for _, window := range run.Windows {
		if math.IsNaN(window.StartSeconds) || math.IsInf(window.StartSeconds, 0) || math.IsNaN(window.EndSeconds) || math.IsInf(window.EndSeconds, 0) || math.Abs(window.StartSeconds-expectedStart) > 0.001 || window.EndSeconds <= window.StartSeconds || window.EndSeconds > run.ObservationSeconds+0.001 || window.Metrics.Total < 20 || window.Metrics.Success <= 0 || window.Metrics.Success > window.Metrics.Total || window.Metrics.Failures != window.Metrics.Total-window.Metrics.Success || math.IsNaN(window.Metrics.P95MS) || math.IsInf(window.Metrics.P95MS, 0) || window.Metrics.P95MS < 0 {
			return "INSUFFICIENT_WINDOWS"
		}
		expectedStart = window.EndSeconds
	}
	if math.Abs(expectedStart-run.ObservationSeconds) > 0.001 {
		return "INSUFFICIENT_WINDOWS"
	}
	first := run.Windows[0]
	last := run.Windows[len(run.Windows)-1]
	firstRate := float64(first.Metrics.Total) / (first.EndSeconds - first.StartSeconds)
	lastRate := float64(last.Metrics.Total) / (last.EndSeconds - last.StartSeconds)
	if last.Metrics.P95MS > first.Metrics.P95MS*1.2 && last.Metrics.P95MS-first.Metrics.P95MS > 5 || lastRate < firstRate*0.8 {
		return "NON_STATIONARY"
	}
	return "NO_LARGE_DRIFT_DETECTED"
}

// ClassifyEvidence separates observed SLO results from invalid delivery and
// incomplete phase evidence. It does not certify sustainable capacity.
func ClassifyEvidence(run report.Result) (string, string) {
	if run.Quality != nil && run.Quality.State == report.QualityInvalid {
		return "INVALID_DELIVERY", "Report accounting or generator evidence is invalid."
	}
	if run.ArrivalsDropped > 0 || run.ArrivalsCancelledBeforeAdmission != nil && *run.ArrivalsCancelledBeforeAdmission > 0 {
		return "INVALID_DELIVERY", "Planned arrivals were dropped or cancelled before admission."
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
	summary := Variability{P95MinMS: math.Inf(1), ErrorRateMin: math.Inf(1), ThroughputMinRPS: math.Inf(1)}
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
		}
	}
	if summary.Runs == 0 {
		return nil
	}
	return &summary
}
