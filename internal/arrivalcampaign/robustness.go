package arrivalcampaign

import (
	"fmt"
	"math"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

// FirstP95RobustnessSchema is the first open-arrival campaign schema that
// persists the p95 interval verdict and the repetition-independence screen.
const FirstP95RobustnessSchema = 11

// Robust p95 verdict states. They qualify a campaign PASS; they never turn a
// failing campaign into a passing one.
const (
	P95RobustPass     = "ROBUST_PASS"
	P95Limited        = "LIMITED"
	P95Inconclusive   = "INCONCLUSIVE"
	P95NotApplicable  = "NOT_APPLICABLE"
	TrendNotAssessed  = "NOT_ASSESSABLE"
	TrendNotDetected  = "NO_MONOTONIC_TREND_DETECTED"
	TrendDetected     = "MONOTONIC_TREND_DETECTED"
	TrendMethod       = "MANN_KENDALL_PERFECT_MONOTONIC_SCREEN"
	minTrendTrials    = 3
	minTrendRelChange = 0.10
)

// P95WindowBound is the distribution-free 95% order-statistic interval for
// the journey p95 of one observation window of one repetition.
type P95WindowBound struct {
	Trial    int     `json:"trial"`
	Phase    string  `json:"phase"`
	Window   int     `json:"window"`
	Samples  int     `json:"samples"`
	P95MS    float64 `json:"p95_ms"`
	LowerMS  float64 `json:"p95_lower_ms,omitempty"`
	UpperMS  float64 `json:"p95_upper_ms,omitempty"`
	Interval bool    `json:"interval_available"`
}

// RepetitionIndependence screens repetition p95s for monotonic drift that
// suggests warm-up or carry-over between repetitions. It is a screen, not a
// proof of independence.
type RepetitionIndependence struct {
	Method              string    `json:"method"`
	RepetitionP95MS     []float64 `json:"repetition_p95_ms"`
	MannKendallS        int       `json:"mann_kendall_s"`
	RelativeChange      float64   `json:"relative_change"`
	MinimumRelativeDiff float64   `json:"minimum_relative_change"`
	State               string    `json:"state"`
	Reason              string    `json:"reason,omitempty"`
}

// P95Robustness is the campaign-level p95 SLO verdict. A PASS is robust only
// when every judged window's upper bound is strictly below the limit and no
// monotonic trend across repetitions is detected.
type P95Robustness struct {
	Method       string                 `json:"method"`
	Confidence   float64                `json:"confidence"`
	Assumption   string                 `json:"assumption"`
	LimitMS      float64                `json:"p95_limit_ms"`
	State        string                 `json:"state"`
	Reasons      []string               `json:"reasons"`
	Windows      []P95WindowBound       `json:"windows"`
	Independence RepetitionIndependence `json:"repetition_independence"`
}

func judgedPhase(name string) bool {
	return name == "stabilization" || name == "observation" || name == "soak"
}

// AssessP95Robustness derives the robust p95 verdict from native phase
// evidence only, so offline auditors can recompute and compare it.
func AssessP95Robustness(result Result) P95Robustness {
	limit := result.P95LimitMS
	if result.Objectives != nil && result.Objectives.JourneyP95LimitMS > 0 {
		limit = result.Objectives.JourneyP95LimitMS
	}
	out := P95Robustness{Method: report.P95IntervalMethodExactBinomialRanks, Confidence: 0.95,
		Assumption: report.P95IntervalAssumptionIIDStationarity, LimitMS: limit, Reasons: []string{}, Windows: []P95WindowBound{}}
	series := []float64{}
	for _, trial := range result.Trials {
		for _, phase := range trial.Phases {
			if !judgedPhase(phase.Name) {
				continue
			}
			if phase.Name == "observation" && phase.Run.Journeys.Total > 0 {
				series = append(series, phase.Run.Journeys.P95MS)
			}
			if phase.EvidenceState != "PASS_OBSERVED" {
				continue
			}
			sets := make([]report.MetricSet, 0, len(phase.Run.Windows))
			for _, window := range phase.Run.Windows {
				sets = append(sets, window.Journeys)
			}
			if len(sets) == 0 {
				sets = append(sets, phase.Run.Journeys)
			}
			for index, m := range sets {
				if m.Total <= 0 {
					continue
				}
				bound := P95WindowBound{Trial: trial.Number, Phase: phase.Name, Window: index, Samples: m.Total, P95MS: m.P95MS}
				if m.P95IID95Exact {
					bound.Interval, bound.LowerMS, bound.UpperMS = true, m.P95IID95LowerMS, m.P95IID95UpperMS
				}
				out.Windows = append(out.Windows, bound)
			}
		}
	}
	out.Independence = assessIndependence(series)
	if result.Outcome != "PASS" {
		out.State = P95NotApplicable
		out.Reasons = append(out.Reasons, "The campaign did not pass; the robust p95 verdict only qualifies a passing campaign.")
		return out
	}
	out.State = P95RobustPass
	missing, above := 0, 0
	for _, bound := range out.Windows {
		switch {
		case !bound.Interval:
			missing++
		case bound.UpperMS >= limit:
			above++
		}
	}
	if len(out.Windows) == 0 {
		out.State = P95Inconclusive
		out.Reasons = append(out.Reasons, "No judged observation window carried journey latency samples.")
	}
	if missing > 0 {
		out.State = P95Inconclusive
		out.Reasons = append(out.Reasons, fmt.Sprintf("%d judged window(s) had too few samples for an exact 95%% p95 order-statistic interval.", missing))
	}
	if above > 0 {
		if out.State == P95RobustPass {
			out.State = P95Limited
		}
		out.Reasons = append(out.Reasons, fmt.Sprintf("%d judged window(s) have a p95 upper 95%% bound at or above the %.3f ms limit; the point estimate passes but the SLO margin is not statistically established.", above, limit))
	}
	if out.Independence.State == TrendDetected {
		if out.State == P95RobustPass {
			out.State = P95Limited
		}
		out.Reasons = append(out.Reasons, out.Independence.Reason)
	}
	return out
}

func assessIndependence(series []float64) RepetitionIndependence {
	out := RepetitionIndependence{Method: TrendMethod, RepetitionP95MS: append([]float64{}, series...), MinimumRelativeDiff: minTrendRelChange}
	n := len(series)
	if n < minTrendTrials {
		out.State = TrendNotAssessed
		out.Reason = fmt.Sprintf("At least %d repetitions with observation evidence are required to screen for monotonic drift.", minTrendTrials)
		return out
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			switch {
			case series[j] > series[i]:
				out.MannKendallS++
			case series[j] < series[i]:
				out.MannKendallS--
			}
		}
	}
	if series[0] > 0 {
		out.RelativeChange = math.Round((series[n-1]-series[0])/series[0]*1e6) / 1e6
	}
	pairs := n * (n - 1) / 2
	if (out.MannKendallS == pairs || out.MannKendallS == -pairs) && math.Abs(out.RelativeChange) >= minTrendRelChange {
		out.State = TrendDetected
		direction := "increased"
		if out.MannKendallS < 0 {
			direction = "decreased"
		}
		out.Reason = fmt.Sprintf("Observation p95 %s monotonically across all %d repetitions (relative change %.1f%%), suggesting warm-up or carry-over; repetitions may not be independent.", direction, n, out.RelativeChange*100)
		return out
	}
	out.State = TrendNotDetected
	return out
}
