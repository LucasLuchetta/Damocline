package capacityplan

import (
	"strings"
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

func TestEvaluateRequiresValidDataQualityForEveryRequiredPhase(t *testing.T) {
	goal := capacitygoal.Goal{PeakUsers: 10}
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials()}
	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: report.QualityLimited}
	result, err := Evaluate(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "LIMITED measurement quality") {
		t.Fatalf("limited phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = nil
	result, err = Evaluate(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "missing explicit data-quality evidence") {
		t.Fatalf("missing phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: "UNRECOGNIZED"}
	result, err = Evaluate(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "unknown measurement quality") {
		t.Fatalf("unknown phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: report.QualityValid}
	evidence.Trials[0].Phases[1].Run.Quality = &report.Quality{State: report.QualityInvalid}
	result, err = Evaluate(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "INVALID measurement quality") {
		t.Fatalf("invalid recovery quality must block a positive demand result: %+v %v", result, err)
	}

	// Warm-up establishes workload state but does not participate in approval.
	evidence.Trials[0].Phases = append([]campaign.Phase{{Name: "warmup", Run: report.Result{Outcome: "PASS"}}}, evidence.Trials[0].Phases...)
	evidence.Trials[0].Phases[2].Run.Quality = &report.Quality{State: report.QualityValid}
	evidence.Trials[0].Phases[1].Run.Quality = &report.Quality{State: report.QualityValid}
	result, err = Evaluate(evidence, goal)
	if err != nil || result.State != "GOAL_MET_UNDER_TESTED_CONDITIONS" {
		t.Fatalf("unqualified warm-up must not block qualified required phases: %+v %v", result, err)
	}
}

func TestEvaluateRejectsIncompletePassingCampaign(t *testing.T) {
	result, err := Evaluate(campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 20, Repeats: 2}, capacitygoal.Goal{PeakUsers: 20})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
}

func passingTrials() []campaign.Trial {
	quality := &report.Quality{State: report.QualityValid}
	return []campaign.Trial{
		{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: report.Result{Outcome: "PASS", Quality: quality}}, {Name: "recovery", Run: report.Result{Outcome: "PASS", Quality: quality}}}},
		{Number: 2, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: report.Result{Outcome: "PASS", Quality: quality}}, {Name: "recovery", Run: report.Result{Outcome: "PASS", Quality: quality}}}},
	}
}

func containsFinding(findings []string, part string) bool {
	for _, finding := range findings {
		if strings.Contains(finding, part) {
			return true
		}
	}
	return false
}
