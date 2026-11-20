package capacityplan

import (
	"slices"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCalibrationBlockersNameAffectedLevelsAndRemediation(t *testing.T) {
	missing := validStatisticalRun()
	missing.Config.Users = 10
	missing.Quality = &report.Quality{State: report.QualityLimited}
	failed := missing
	failed.Config.Users = 20
	failed.ScenarioCalibration = &calibration.ScenarioRecord{}
	limited := missing
	limited.Config.Users = 30
	limited.Envelope = &report.Envelope{Mode: report.EnvelopeModeExtended}
	evidence := campaign.Result{Outcome: "PASS", BaseUsers: 10, Trials: []campaign.Trial{{Number: 1, Phases: []campaign.Phase{
		{Name: "warmup", Run: missing},
		{Name: "observation", Run: missing},
		{Name: "recovery", Run: missing},
		{Name: "spike", Run: failed},
		{Name: "observation", Run: limited},
	}}}}
	result, err := evaluateWithDeclaredTestQuality(evidence, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected result: %+v (%v)", result, err)
	}
	find := func(part string) DecisionBlocker {
		i := slices.IndexFunc(result.Blockers, func(b DecisionBlocker) bool { return strings.Contains(b.Finding, part) })
		if i < 0 {
			t.Fatalf("missing blocker %q: %+v", part, result.Blockers)
		}
		return result.Blockers[i]
	}
	b := find("carry no generator calibration")
	if !strings.Contains(b.Finding, "10 VUs") || !strings.Contains(b.Action, "`damocline calibrate` for these levels (10 VUs") || strings.Count(b.Finding, "10 VUs") != 1 {
		t.Fatalf("missing-calibration blocker did not name its level once: %+v", b)
	}
	if b := find("malformed or schema-disallowed"); !strings.Contains(b.Finding, "20 VUs") {
		t.Fatalf("malformed self-calibration blocker: %+v", b)
	}
	if b := find("load-envelope"); b.Category != BlockerDamocline || !strings.Contains(b.Finding, "30 VUs") {
		t.Fatalf("envelope blocker: %+v", b)
	}
}

func TestCalibrationBlockersSkipValidPhases(t *testing.T) {
	run := validStatisticalRun()
	evidence := campaign.Result{Trials: []campaign.Trial{{Number: 1, Phases: []campaign.Phase{{Name: "observation", Run: run}}}}}
	var result Result
	collectCalibrationBlockers(evidence, func(report.Result) *report.Quality { return &report.Quality{State: report.QualityValid} }, &result)
	if len(result.Blockers) != 0 {
		t.Fatalf("valid phase produced calibration blockers: %+v", result.Blockers)
	}
}
