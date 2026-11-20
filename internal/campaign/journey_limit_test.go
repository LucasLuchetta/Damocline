package campaign

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func journeyLimitRun(journeyLimitMS float64, outcome string) report.Result {
	journey := report.MetricSet{Total: 20, Success: 20, P95MS: 8200}
	return report.Result{SchemaVersion: 22, Outcome: outcome,
		Config:   report.RunConfig{P95LimitMS: 500, JourneyP95LimitMS: journeyLimitMS, MaxErrorRate: 0.01, MinSamplesPerStep: 1, Duration: "10s", WindowDuration: "5s"},
		Journeys: journey, Windows: []report.WindowResult{{Journeys: journey}}}
}

func TestClassifyEvidenceUsesSavedJourneyLimit(t *testing.T) {
	if state, reason := ClassifyEvidence(journeyLimitRun(10000, "PASS")); state != "PASS_OBSERVED" {
		t.Fatalf("journey within journey limit: %s %s", state, reason)
	}
	if state, _ := ClassifyEvidence(journeyLimitRun(8000, "PASS")); state != "FAIL_OBSERVED" {
		t.Fatalf("journey above journey limit was %s", state)
	}
	if state, _ := ClassifyEvidence(journeyLimitRun(0, "PASS")); state != "FAIL_OBSERVED" {
		t.Fatalf("absent journey limit must keep p95_limit_ms, got %s", state)
	}
}

func TestObjectiveContractUsesScenarioJourneyLimit(t *testing.T) {
	base := config.Validated{Scenario: config.Scenario{P95LimitMS: 500, JourneyP95LimitMS: 10000, MaxErrorRate: 0.01, MinSamplesPerStep: 1}}
	o := NewObjectiveContract(base, StabilityPolicy{MinimumWindows: 3, MinimumSamplesPerWindow: 20})
	if o.JourneyP95LimitMS != 10000 || !o.MatchesRun(journeyLimitRun(10000, "PASS")) || o.MatchesRun(journeyLimitRun(0, "PASS")) {
		t.Fatalf("objective contract did not bind the journey limit: %+v", o)
	}
	base.Scenario.JourneyP95LimitMS = 0
	if legacy := NewObjectiveContract(base, StabilityPolicy{}); legacy.JourneyP95LimitMS != 500 {
		t.Fatalf("legacy contract must keep p95_limit_ms: %+v", legacy)
	}
}
