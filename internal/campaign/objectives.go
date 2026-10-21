package campaign

import (
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// ObjectiveContract records the acceptance policy fixed before a campaign
// starts. It is evidence of declared criteria, not proof that the workload or
// target declaration is representative.
type ObjectiveContract struct {
	Version                   int     `json:"version"`
	JourneyScope              string  `json:"journey_scope"`
	JourneyP95LimitMS         float64 `json:"journey_p95_limit_ms"`
	MaxJourneyErrorRate       float64 `json:"max_journey_error_rate"`
	MinSamplesPerStep         int     `json:"min_samples_per_step"`
	MinCompletedWorkRatio     float64 `json:"min_completed_work_ratio"`
	MaxDroppedArrivals        int64   `json:"max_dropped_arrivals"`
	MinimumObservationWindows int     `json:"minimum_observation_windows"`
	MinimumSamplesPerWindow   int     `json:"minimum_samples_per_window"`
	WindowDuration            string  `json:"window_duration"`
}

// MatchesRun checks that a recorded phase retained the objectives declared
// before execution. Automatic window sizing is resolved per phase, so its
// recorded duration must still be valid and bounded.
func (o ObjectiveContract) MatchesRun(run report.Result) bool {
	if run.SchemaVersion < 22 || run.Config.P95LimitMS != o.JourneyP95LimitMS || run.Config.MaxErrorRate != o.MaxJourneyErrorRate ||
		run.Config.MinSamplesPerStep != o.MinSamplesPerStep {
		return false
	}
	window, err := time.ParseDuration(run.Config.WindowDuration)
	if err != nil || window < time.Second || window > time.Minute {
		return false
	}
	if o.WindowDuration != "AUTO_PER_PHASE" {
		if run.Config.WindowDuration != o.WindowDuration {
			return false
		}
	} else if run.Config.WindowDuration != "" {
		configuredWindow, err := time.ParseDuration(run.Config.WindowDuration)
		phaseDuration, durationErr := time.ParseDuration(run.Config.Duration)
		if err != nil || durationErr != nil || configuredWindow > phaseDuration {
			return false
		}
	}
	return true
}

// NewObjectiveContract snapshots validated scenario goals and the campaign's
// window and delivery policy before any phase executes.
func NewObjectiveContract(base config.Validated, stability StabilityPolicy) ObjectiveContract {
	windowDuration := base.Window.String()
	if base.Window == 0 {
		windowDuration = "AUTO_PER_PHASE"
	}
	return ObjectiveContract{
		Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW",
		JourneyP95LimitMS: base.Scenario.P95LimitMS, MaxJourneyErrorRate: base.Scenario.MaxErrorRate,
		MinSamplesPerStep: base.Scenario.MinSamplesPerStep, MinCompletedWorkRatio: 1,
		MaxDroppedArrivals: 0, MinimumObservationWindows: stability.MinimumWindows,
		MinimumSamplesPerWindow: stability.MinimumSamplesPerWindow, WindowDuration: windowDuration,
	}
}

// Validate rejects missing or unsupported acceptance criteria before traffic.
func (o ObjectiveContract) Validate() error {
	if o.Version != 1 || o.JourneyScope != "AGGREGATE_AND_EACH_WORKFLOW" {
		return fmt.Errorf("campaign objective contract version or journey scope is unsupported")
	}
	if math.IsNaN(o.JourneyP95LimitMS) || math.IsInf(o.JourneyP95LimitMS, 0) || o.JourneyP95LimitMS <= 0 || o.JourneyP95LimitMS > 120000 {
		return fmt.Errorf("campaign objectives require a finite journey p95 limit in (0,120000] ms")
	}
	if math.IsNaN(o.MaxJourneyErrorRate) || math.IsInf(o.MaxJourneyErrorRate, 0) || o.MaxJourneyErrorRate < 0 || o.MaxJourneyErrorRate >= 1 {
		return fmt.Errorf("campaign objectives require a finite journey error budget in [0,1)")
	}
	if o.MinSamplesPerStep < 1 || o.MinSamplesPerStep > 10000 {
		return fmt.Errorf("campaign objectives require min_samples_per_step from 1 to 10000")
	}
	if o.MinCompletedWorkRatio != 1 || o.MaxDroppedArrivals != 0 {
		return fmt.Errorf("campaign objective contract requires every started journey to complete and zero dropped arrivals")
	}
	if o.MinimumObservationWindows < 3 || o.MinimumObservationWindows > 20 || o.MinimumSamplesPerWindow < 20 || o.MinimumSamplesPerWindow > 100000 {
		return fmt.Errorf("campaign objectives require at least 3 windows and 20 samples per window")
	}
	if o.WindowDuration != "AUTO_PER_PHASE" {
		window, err := time.ParseDuration(o.WindowDuration)
		if err != nil || window < time.Second || window > time.Minute {
			return fmt.Errorf("campaign objective window duration must be AUTO_PER_PHASE or 1s-1m")
		}
	}
	return nil
}
