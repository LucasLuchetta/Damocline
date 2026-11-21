package campaign

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

// WarmupSettlingPolicy declares the bounded evidence rule that a windowed
// warm-up must satisfy before its trial may start judged phases (ADR 0184).
// The zero value disables the check.
type WarmupSettlingPolicy struct {
	// WindowSeconds is the length of each warm-up settling window.
	WindowSeconds int `json:"window_seconds"`
	// TolerancePercent and ToleranceMS bound the change of every compared
	// p95 between consecutive windows: |current-previous| must not exceed
	// max(previous*TolerancePercent/100, ToleranceMS).
	TolerancePercent float64 `json:"tolerance_percent"`
	ToleranceMS      float64 `json:"tolerance_ms"`
	// ConsecutiveStable is the number of consecutive within-tolerance
	// comparisons that must end the warm-up.
	ConsecutiveStable int `json:"consecutive_stable"`
	// MinimumSamples is the completed-sample floor for a series to be
	// compared in a window. Request p95 must always be comparable.
	MinimumSamples int `json:"minimum_samples"`
}

// Enabled reports whether the policy requests warm-up settling evidence.
func (p WarmupSettlingPolicy) Enabled() bool { return p != (WarmupSettlingPolicy{}) }

// Window returns the settling window length.
func (p WarmupSettlingPolicy) Window() time.Duration {
	return time.Duration(p.WindowSeconds) * time.Second
}

// DefaultWarmupSettlingPolicy returns the policy used by `damocline capacity`.
func DefaultWarmupSettlingPolicy(window time.Duration) WarmupSettlingPolicy {
	return WarmupSettlingPolicy{WindowSeconds: int(window / time.Second), TolerancePercent: 25, ToleranceMS: 50, ConsecutiveStable: 2, MinimumSamples: 20}
}

// MinimumSettlingWarmup is the shortest warm-up that can hold the windows a
// stabilized verdict needs.
func (p WarmupSettlingPolicy) MinimumSettlingWarmup() time.Duration {
	return time.Duration(p.ConsecutiveStable+1) * p.Window()
}

// ValidateWarmupSettlingPolicy bounds an enabled policy against the warm-up.
func ValidateWarmupSettlingPolicy(p WarmupSettlingPolicy, warmup time.Duration) error {
	if !p.Enabled() {
		return nil
	}
	if p.WindowSeconds < 2 || p.WindowSeconds > 30 {
		return fmt.Errorf("warm-up settling window must be between 2s and 30s")
	}
	if math.IsNaN(p.TolerancePercent) || math.IsInf(p.TolerancePercent, 0) || p.TolerancePercent <= 0 || p.TolerancePercent > 100 {
		return fmt.Errorf("warm-up settling tolerance percent must be greater than 0 and at most 100")
	}
	if math.IsNaN(p.ToleranceMS) || math.IsInf(p.ToleranceMS, 0) || p.ToleranceMS < 0 || p.ToleranceMS > 10000 {
		return fmt.Errorf("warm-up settling tolerance must be between 0ms and 10000ms")
	}
	if p.ConsecutiveStable < 1 || p.ConsecutiveStable > 10 {
		return fmt.Errorf("warm-up settling consecutive-stable must be between 1 and 10")
	}
	if p.MinimumSamples < 1 || p.MinimumSamples > 100000 {
		return fmt.Errorf("warm-up settling minimum samples must be between 1 and 100000")
	}
	if warmup < p.MinimumSettlingWarmup() {
		return fmt.Errorf("warm-up %s is shorter than the %s needed for %d settling comparisons of %s windows", warmup, p.MinimumSettlingWarmup(), p.ConsecutiveStable, p.Window())
	}
	return nil
}

// Settling verdicts.
const (
	SettlingStabilized    = "STABILIZED"
	SettlingNonStationary = "NON_STATIONARY"
	SettlingInsufficient  = "INSUFFICIENT_WINDOWS"
)

// SettlingWindow is one warm-up window's p95 summary.
type SettlingWindow struct {
	StartSeconds float64            `json:"start_seconds"`
	EndSeconds   float64            `json:"end_seconds"`
	Requests     int                `json:"requests"`
	RequestP95MS float64            `json:"request_p95_ms"`
	JourneyP95MS *float64           `json:"journey_p95_ms,omitempty"`
	StepP95MS    map[string]float64 `json:"step_p95_ms,omitempty"`
	// Change is the largest relative p95 change versus the previous window,
	// in percent; nil for the first window or an incomparable pair.
	ChangePercent *float64 `json:"change_percent,omitempty"`
	// WithinTolerance is true when every compared p95 stayed in tolerance.
	WithinTolerance bool `json:"within_tolerance"`
}

// SettlingAssessment is the recomputable warm-up settling verdict.
type SettlingAssessment struct {
	State   string           `json:"state"`
	Windows []SettlingWindow `json:"windows"`
	// StabilizedAfterSeconds is the warm-up time at which the stable tail
	// began; only set when State is STABILIZED.
	StabilizedAfterSeconds *float64 `json:"stabilized_after_seconds,omitempty"`
	Reason                 string   `json:"reason"`
}

// AssessWarmupSettling evaluates the complete settling windows of a warm-up
// run. The warm-up is stabilized only when its final ConsecutiveStable
// consecutive-window comparisons all stay within tolerance. A shorter tail
// is never extrapolated.
func AssessWarmupSettling(run report.Result, p WarmupSettlingPolicy) SettlingAssessment {
	full := float64(p.WindowSeconds)
	windows := make([]SettlingWindow, 0, len(run.Windows))
	for _, w := range run.Windows {
		if w.EndSeconds-w.StartSeconds < 0.9*full {
			continue
		}
		sw := SettlingWindow{StartSeconds: w.StartSeconds, EndSeconds: w.EndSeconds, Requests: w.Metrics.Total, RequestP95MS: w.Metrics.P95MS}
		if w.Journeys.Total >= p.MinimumSamples {
			v := w.Journeys.P95MS
			sw.JourneyP95MS = &v
		}
		for _, step := range w.Steps {
			if step.Metrics.Total >= p.MinimumSamples {
				if sw.StepP95MS == nil {
					sw.StepP95MS = map[string]float64{}
				}
				sw.StepP95MS[step.Name] = step.Metrics.P95MS
			}
		}
		windows = append(windows, sw)
	}
	for i := 1; i < len(windows); i++ {
		change, ok := settlingChange(windows[i-1], windows[i], p)
		if change >= 0 {
			windows[i].ChangePercent = &change
		}
		windows[i].WithinTolerance = ok
	}
	a := SettlingAssessment{Windows: windows}
	if len(windows) < p.ConsecutiveStable+1 {
		a.State = SettlingInsufficient
		a.Reason = fmt.Sprintf("The warm-up recorded %d complete %ds settling windows; %d are needed to compare %d consecutive windows.", len(windows), p.WindowSeconds, p.ConsecutiveStable+1, p.ConsecutiveStable)
		return a
	}
	start := len(windows)
	for start > 1 && windows[start-1].WithinTolerance {
		start--
	}
	tail := len(windows) - start
	if tail < p.ConsecutiveStable {
		a.State = SettlingNonStationary
		a.Reason = fmt.Sprintf("Warm-up p95 latency was still changing by more than %.0f%% (or %.0fms) between consecutive %ds windows at the end of the %s warm-up, so the start-up transient had not settled. Judged phases were not started.", p.TolerancePercent, p.ToleranceMS, p.WindowSeconds, time.Duration(run.ObservationSeconds*float64(time.Second)).Round(time.Second))
		return a
	}
	at := windows[start-1].StartSeconds
	a.State = SettlingStabilized
	a.StabilizedAfterSeconds = &at
	a.Reason = fmt.Sprintf("Warm-up p95 latency settled after about %.0fs: the last %d consecutive %ds windows changed by at most %.0f%% (or %.0fms).", at, tail, p.WindowSeconds, p.TolerancePercent, p.ToleranceMS)
	return a
}

// settlingChange returns the largest relative change and whether every
// compared p95 is within tolerance. Request p95 must be comparable.
func settlingChange(prev, cur SettlingWindow, p WarmupSettlingPolicy) (float64, bool) {
	if prev.Requests < p.MinimumSamples || cur.Requests < p.MinimumSamples {
		return -1, false
	}
	worst, ok := 0.0, true
	compare := func(a, b float64) {
		if math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) || a < 0 || b < 0 {
			ok = false
			return
		}
		delta := math.Abs(b - a)
		if a > 0 {
			worst = max(worst, delta/a*100)
		} else if delta > 0 {
			worst = max(worst, 100)
		}
		if delta > max(a*p.TolerancePercent/100, p.ToleranceMS) {
			ok = false
		}
	}
	compare(prev.RequestP95MS, cur.RequestP95MS)
	if prev.JourneyP95MS != nil && cur.JourneyP95MS != nil {
		compare(*prev.JourneyP95MS, *cur.JourneyP95MS)
	}
	names := make([]string, 0, len(cur.StepP95MS))
	for name := range cur.StepP95MS {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if before, found := prev.StepP95MS[name]; found {
			compare(before, cur.StepP95MS[name])
		}
	}
	return math.Round(worst*10) / 10, ok
}

// SettlingApplies reports whether a warm-up run is complete enough for a
// settling verdict; aborted, inconclusive or invalid-delivery runs end the
// trial through the ordinary phase rules instead.
func SettlingApplies(run report.Result, state string) bool {
	return run.Outcome != "ABORTED" && run.Outcome != "INCONCLUSIVE" && state != "INVALID_DELIVERY"
}

func settlingPolicyRecord(p WarmupSettlingPolicy) *WarmupSettlingPolicy {
	if !p.Enabled() {
		return nil
	}
	return &p
}
