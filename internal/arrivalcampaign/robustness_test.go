package arrivalcampaign

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func robustnessFixture(p95 []float64, upper float64, exact bool) Result {
	result := Result{P95LimitMS: 500, Outcome: "PASS"}
	for index, value := range p95 {
		window := report.WindowResult{Journeys: report.MetricSet{Total: 400, P95MS: value, P95IID95LowerMS: value - 10, P95IID95UpperMS: upper, P95IID95Exact: exact}}
		run := report.Result{Journeys: report.MetricSet{Total: 800, P95MS: value}, Windows: []report.WindowResult{window, window}}
		result.Trials = append(result.Trials, Trial{Number: index + 1, Outcome: "PASS", Phases: []Phase{
			{Name: "warmup", EvidenceState: "PASS_OBSERVED", Run: report.Result{Journeys: report.MetricSet{Total: 10, P95MS: 9999}}},
			{Name: "observation", EvidenceState: "PASS_OBSERVED", Run: run},
		}})
	}
	return result
}

func TestP95RobustPassRequiresUpperBoundBelowLimit(t *testing.T) {
	got := AssessP95Robustness(robustnessFixture([]float64{300, 290, 305}, 450, true))
	if got.State != P95RobustPass || len(got.Windows) != 6 || got.Independence.State != TrendNotDetected {
		t.Fatalf("unexpected verdict: %+v", got)
	}
}

func TestP95UpperBoundAtOrAboveLimitIsLimited(t *testing.T) {
	got := AssessP95Robustness(robustnessFixture([]float64{300, 290, 305}, 500, true))
	if got.State != P95Limited || !strings.Contains(strings.Join(got.Reasons, " "), "upper 95% bound") {
		t.Fatalf("upper bound equal to limit must not be robust: %+v", got)
	}
}

func TestP95WithoutIntervalIsInconclusive(t *testing.T) {
	got := AssessP95Robustness(robustnessFixture([]float64{300, 290, 305}, 0, false))
	if got.State != P95Inconclusive {
		t.Fatalf("missing interval must be inconclusive: %+v", got)
	}
}

func TestMonotonicRepetitionDriftIsLimited(t *testing.T) {
	got := AssessP95Robustness(robustnessFixture([]float64{200, 240, 290}, 450, true))
	if got.Independence.State != TrendDetected || got.Independence.MannKendallS != 3 || got.State != P95Limited {
		t.Fatalf("monotonic drift must be flagged: %+v", got)
	}
	small := AssessP95Robustness(robustnessFixture([]float64{300, 301, 302}, 450, true))
	if small.Independence.State != TrendNotDetected || small.State != P95RobustPass {
		t.Fatalf("tiny monotonic change must not be flagged: %+v", small)
	}
	two := AssessP95Robustness(robustnessFixture([]float64{200, 400}, 450, true))
	if two.Independence.State != TrendNotAssessed || two.State != P95Inconclusive || len(two.Reasons) == 0 {
		t.Fatalf("two repetitions cannot establish a robust pass: %+v", two)
	}
}

func TestP95RobustnessNotApplicableToFailure(t *testing.T) {
	result := robustnessFixture([]float64{300}, 450, true)
	result.Outcome = "FAIL"
	if got := AssessP95Robustness(result); got.State != P95NotApplicable {
		t.Fatalf("got %+v", got)
	}
}
