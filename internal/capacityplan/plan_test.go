package capacityplan

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestEvaluateRequiresExactSustainedEvidence(t *testing.T) {
	goal := capacitygoal.Goal{PeakUsers: 100, GrowthPercent: 20, ReservePercent: 25}
	passing := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 100, Repeats: 2, Trials: passingTrials()}
	result, err := Evaluate(passing, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || result.TargetUsers != 150 {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
	passing.BaseUsers = 150
	result, err = Evaluate(passing, goal)
	if err != nil || result.State != "GOAL_MET_UNDER_TESTED_CONDITIONS" {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
	passing.BaseUsers = 200
	result, err = Evaluate(passing, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("different workload level must not be interpolated: %+v %v", result, err)
	}
}

func TestEvaluateRejectsIncompletePassingCampaign(t *testing.T) {
	result, err := Evaluate(campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 20, Repeats: 2}, capacitygoal.Goal{PeakUsers: 20})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
}

func passingTrials() []campaign.Trial {
	return []campaign.Trial{
		{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: report.Result{Outcome: "PASS"}}, {Name: "recovery", Run: report.Result{Outcome: "PASS"}}}},
		{Number: 2, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: report.Result{Outcome: "PASS"}}, {Name: "recovery", Run: report.Result{Outcome: "PASS"}}}},
	}
}
