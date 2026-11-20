package capacityplan

import (
	"slices"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestDecisionListsIndependentBlockers(t *testing.T) {
	evidence, err := ReadCampaign("testdata/campaigns/schema-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	evidence.Outcome = "FAIL"
	evidence.ObservationVariability = nil
	result, err := evaluateWithQuality(evidence, capacitygoal.Goal{PeakUsers: evidence.BaseUsers + 1}, func(report.Result) *report.Quality {
		return &report.Quality{State: report.QualityLimited}
	})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected result: %+v (%v)", result, err)
	}
	for _, category := range []string{BlockerConfiguration, BlockerEvidence, BlockerDamocline} {
		if !slices.ContainsFunc(result.Blockers, func(b DecisionBlocker) bool {
			return b.Category == category && b.Finding != "" && b.Action != ""
		}) {
			t.Fatalf("missing actionable %s blocker: %+v", category, result.Blockers)
		}
	}
}
