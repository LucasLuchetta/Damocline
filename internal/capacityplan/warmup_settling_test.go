package capacityplan

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestAuditRecomputesWarmupSettlingVerdict(t *testing.T) {
	policy := campaign.WarmupSettlingPolicy{WindowSeconds: 2, TolerancePercent: 10, ToleranceMS: 5, ConsecutiveStable: 2, MinimumSamples: 20}
	run := report.Result{Outcome: "PASS", ObservationSeconds: 8}
	for i, p95 := range []float64{50, 100, 150, 200} {
		m := report.MetricSet{Total: 40, Success: 40, P95MS: p95}
		run.Windows = append(run.Windows, report.WindowResult{StartSeconds: float64(2 * i), EndSeconds: float64(2*i + 2), Metrics: m, Journeys: m})
	}
	evidence := campaign.Result{WarmupSettling: &policy}
	assessment := campaign.AssessWarmupSettling(run, policy)
	phase := campaign.Phase{Name: "warmup", Run: run, Settling: &assessment}
	unsettled, err := validateWarmupSettling(evidence, 1, phase, "PASS_OBSERVED")
	if err != nil || !unsettled {
		t.Fatalf("a recorded NON_STATIONARY warm-up must be a terminal limitation: %v %v", unsettled, err)
	}
	forged := assessment
	forged.State = campaign.SettlingStabilized
	phase.Settling = &forged
	if _, err := validateWarmupSettling(evidence, 1, phase, "PASS_OBSERVED"); err == nil {
		t.Fatal("an altered settling verdict must be rejected")
	}
	phase.Settling = nil
	if _, err := validateWarmupSettling(evidence, 1, phase, "PASS_OBSERVED"); err == nil {
		t.Fatal("a removed settling verdict must be rejected")
	}
	if _, err := validateWarmupSettling(campaign.Result{}, 1, phase, "PASS_OBSERVED"); err != nil {
		t.Fatalf("campaigns without a settling policy are unchanged: %v", err)
	}
}
