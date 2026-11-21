package cli

import (
	"strconv"
	"strings"

	"fmt"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
)

// warmupSettlingFromFlags builds the optional settling policy. A window that
// is not a whole number of seconds is kept invalid so validation rejects it.
func warmupSettlingFromFlags(window time.Duration, percent, ms float64, consecutive, samples int) campaign.WarmupSettlingPolicy {
	if window == 0 {
		return campaign.WarmupSettlingPolicy{}
	}
	seconds := int(window / time.Second)
	if window%time.Second != 0 {
		seconds = -1
	}
	return campaign.WarmupSettlingPolicy{WindowSeconds: seconds, TolerancePercent: percent, ToleranceMS: ms, ConsecutiveStable: consecutive, MinimumSamples: samples}
}

func warmupSettlingPlanLine(options campaign.Options) string {
	p := options.WarmupSettling
	return fmt.Sprintf("Warm-up settling: %s warm-up in %s windows; the last %d consecutive window comparisons must change request, journey and step p95 by at most %.0f%% (or %.0fms), otherwise the trial ends INCONCLUSIVE (NON_STATIONARY) before any judged phase. Every observation window is still judged against the SLO.",
		options.Warmup, p.Window(), p.ConsecutiveStable, p.TolerancePercent, p.ToleranceMS)
}

// capacityWarmupCycles is how many complete per-VU journey cycles the sized
// capacity warm-up holds after the ramp.
const capacityWarmupCycles = 3

// capacityWarmupCeiling bounds the sized capacity warm-up per trial.
const capacityWarmupCeiling = 90 * time.Second

// capacityWarmup sizes a capacity campaign warm-up from the level's measured
// explore journey cycle: ramp plus three journey cycles, rounded up to whole
// settling windows, at least three settling windows (and the protocol floor)
// and at most 90s. It returns the warm-up, its floor, the measured per-VU
// journey cycle in seconds (0 when unmeasured) and the settling policy.
func capacityWarmup(result explorer.Result, level int, ramp, window time.Duration) (time.Duration, time.Duration, float64, campaign.WarmupSettlingPolicy) {
	settle := min(max((window/3).Round(time.Second), capacityTiming.SettleWindowMin), 15*time.Second)
	policy := campaign.DefaultWarmupSettlingPolicy(settle)
	cycle := capacityJourneyCycle(result, level)
	floor := roundUpTo(max(capacityTiming.Warmup, policy.MinimumSettlingWarmup()), settle)
	warmup := roundUpTo(ramp+time.Duration(capacityWarmupCycles*cycle*float64(time.Second)), settle)
	ceiling := capacityWarmupCeiling / settle * settle
	warmup = min(max(warmup, floor), max(ceiling, floor))
	return warmup, floor, cycle, policy
}

// capacityJourneyCycle is the mean per-VU journey cycle (users x elapsed /
// completed journeys) over the level's passing explore runs.
func capacityJourneyCycle(result explorer.Result, level int) float64 {
	total, runs := 0.0, 0
	for _, probe := range result.Probes {
		if probe.Users != level || probe.Status != "PASS" {
			continue
		}
		for _, run := range probe.Runs {
			if run.Journeys.Total > 0 && run.ElapsedSeconds > 0 {
				total += float64(level) * run.ElapsedSeconds / float64(run.Journeys.Total)
				runs++
			}
		}
	}
	if runs == 0 {
		return 0
	}
	return total / float64(runs)
}

func roundUpTo(d, step time.Duration) time.Duration {
	if step <= 0 {
		return d
	}
	return (d + step - 1) / step * step
}

func capacityWarmupPlanLine(options campaign.Options, ramp time.Duration, cycle float64, floor time.Duration) string {
	basis := "no measured journey cycle, so the settling floor applies"
	if cycle > 0 {
		basis = fmt.Sprintf("ramp %s + %d journey cycles of %.1fs measured per VU in explore", ramp, capacityWarmupCycles, cycle)
	}
	return fmt.Sprintf("Warm-up per trial: %s (%s; bounds %s-%s). %s", options.Warmup, basis, floor, max(capacityWarmupCeiling, floor), warmupSettlingPlanLine(options))
}

func warmupSettlingArgs(p campaign.WarmupSettlingPolicy) []string {
	if !p.Enabled() {
		return nil
	}
	return []string{
		"--warmup-settle-window", p.Window().String(),
		"--warmup-settle-tolerance-percent", strconv.FormatFloat(p.TolerancePercent, 'f', -1, 64),
		"--warmup-settle-tolerance-ms", strconv.FormatFloat(p.ToleranceMS, 'f', -1, 64),
		"--warmup-settle-consecutive", strconv.Itoa(p.ConsecutiveStable),
		"--stability-min-samples", strconv.Itoa(p.MinimumSamples),
	}
}

func sameWarmupSettling(saved *campaign.WarmupSettlingPolicy, planned campaign.WarmupSettlingPolicy) bool {
	if saved == nil {
		return !planned.Enabled()
	}
	return *saved == planned
}

// warmupSettlingSentences explains each trial's warm-up settling verdict in
// plain language for capacity.md and capacity.html.
func warmupSettlingSentences(evidence campaign.Result) []string {
	p := evidence.WarmupSettling
	if p == nil {
		return nil
	}
	lines := []string{fmt.Sprintf("Warm-up settling rule: the %s warm-up was split into %ds windows and had to end with %d consecutive window-to-window p95 changes of at most %.0f%% (or %.0fms) for requests, complete journeys and every step with at least %d samples. Warm-up windows are never judged against the SLO; every observation window still is.",
		evidence.Warmup, p.WindowSeconds, p.ConsecutiveStable, p.TolerancePercent, p.ToleranceMS, p.MinimumSamples)}
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "warmup" || phase.Settling == nil {
				continue
			}
			s := phase.Settling
			series := make([]string, 0, len(s.Windows))
			for _, w := range s.Windows {
				series = append(series, fmt.Sprintf("%.0fms", w.RequestP95MS))
			}
			switch s.State {
			case campaign.SettlingStabilized:
				lines = append(lines, fmt.Sprintf("Trial %d: the warm-up settled after about %.0fs of %s (request p95 per window: %s), so observation started after a measured stable warm-up.", trial.Number, *s.StabilizedAfterSeconds, evidence.Warmup, strings.Join(series, ", ")))
			default:
				lines = append(lines, fmt.Sprintf("Trial %d: the warm-up did NOT settle (%s; request p95 per window: %s). The trial ended INCONCLUSIVE before observation; this is not a pass and not a capacity measurement. %s", trial.Number, s.State, strings.Join(series, ", "), s.Reason))
			}
		}
	}
	return lines
}
