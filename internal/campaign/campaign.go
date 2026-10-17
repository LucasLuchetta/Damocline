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

type Options struct {
	Warmup      time.Duration
	Observation time.Duration
	Recovery    time.Duration
	Repeats     int
	Budget      time.Duration
}

type Phase struct {
	Name string        `json:"name"`
	Run  report.Result `json:"run"`
}

type Trial struct {
	Number  int     `json:"number"`
	Phases  []Phase `json:"phases"`
	Outcome string  `json:"outcome"`
}

type Result struct {
	SchemaVersion          int          `json:"schema_version"`
	Scenario               string       `json:"scenario"`
	Target                 string       `json:"target"`
	StartedAt              string       `json:"started_at"`
	Warmup                 string       `json:"warmup"`
	Observation            string       `json:"observation"`
	Recovery               string       `json:"recovery"`
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
	if options.Observation < 10*time.Second || options.Observation > 3*time.Minute {
		return fmt.Errorf("observation must be between 10s and 3m")
	}
	if options.Repeats < 2 || options.Repeats > 5 {
		return fmt.Errorf("repeats must be between 2 and 5")
	}
	minimum := time.Duration(options.Repeats) * (options.Warmup + options.Observation + options.Recovery)
	if options.Budget != 0 && (options.Budget < minimum || options.Budget > 15*time.Minute) {
		return fmt.Errorf("budget must cover every phase and be at most 15m")
	}
	if minimum > 15*time.Minute {
		return fmt.Errorf("configured phases exceed the 15m campaign safety budget")
	}
	return nil
}

func Run(ctx context.Context, base config.Validated, options Options) (Result, error) {
	return WithRunner(ctx, base, options, engine.Run)
}

func WithRunner(ctx context.Context, base config.Validated, options Options, runner Runner) (Result, error) {
	if err := Validate(options); err != nil {
		return Result{}, err
	}
	if options.Budget == 0 {
		options.Budget = time.Duration(options.Repeats) * (options.Warmup + options.Observation + options.Recovery)
	}
	ctx, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()
	result := Result{
		SchemaVersion: 1,
		Scenario:      base.Scenario.Name,
		Target:        base.Base.Scheme + "://" + base.Base.Host,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Warmup:        options.Warmup.String(),
		Observation:   options.Observation.String(),
		Recovery:      options.Recovery.String(),
		Repeats:       options.Repeats,
		Trials:        []Trial{},
		Outcome:       "INCONCLUSIVE",
		Notes: []string{
			"Warm-up runs establish workload state but are excluded from capacity conclusions.",
			"Observation and recovery both require PASS in every independent trial; this is evidence under the configured workload, not a universal capacity certification.",
		},
	}
	for number := 1; number <= options.Repeats; number++ {
		trial := Trial{Number: number, Phases: []Phase{}, Outcome: "PASS"}
		for _, phase := range []struct {
			name     string
			duration time.Duration
			required bool
		}{
			{name: "warmup", duration: options.Warmup},
			{name: "observation", duration: options.Observation, required: true},
			{name: "recovery", duration: options.Recovery, required: true},
		} {
			if phase.duration == 0 {
				continue
			}
			if ctx.Err() != nil {
				trial.Outcome = "ABORTED"
				break
			}
			v := base
			v.Duration = phase.duration
			v.Scenario.Duration = phase.duration.String()
			run := runner(ctx, v)
			trial.Phases = append(trial.Phases, Phase{Name: phase.name, Run: run})
			if run.Outcome == "ABORTED" || run.Outcome == "INCONCLUSIVE" {
				trial.Outcome = run.Outcome
				break
			}
			if phase.required && run.Outcome != "PASS" {
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
