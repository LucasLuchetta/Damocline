package capacityplan

import (
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
)

func TestCampaignLevelScheduleValidatesDeclaredLongRunEnvelope(t *testing.T) {
	policy := campaign.DefaultStabilityPolicy()
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, Scenario: "synthetic", Target: "https://app.example.com", BaseUsers: 10,
		Warmup: "0s", Stabilization: "0s", Observation: "30m", SpikeDuration: "0s", Recovery: "45s", Cooldown: "0s", Budget: "70m", Repeats: 2,
		LongRun: true, DeclaredEnvironment: "staging", StabilityPolicy: &policy,
	}
	options, phases, err := campaignLevelSchedule(evidence)
	if err != nil || !options.LongRun || options.Budget != 70*time.Minute || len(phases) != 2 {
		t.Fatalf("valid long-run schedule rejected: options=%+v phases=%+v err=%v", options, phases, err)
	}
	evidence.DeclaredEnvironment = "production"
	if _, _, err := campaignLevelSchedule(evidence); err == nil {
		t.Fatal("long-run campaign without staging declaration was accepted")
	}
}

func TestCampaignCollectionAggregatesOnlyMatchedLevels(t *testing.T) {
	lower := auditFixtureAtUsers(2)
	higher := auditFixtureAtUsers(4)
	markCampaignFailed(&higher)
	assessment, err := BuildCampaignCollectionAssessment([]campaign.Result{higher, lower}, &capacitygoal.Goal{PeakUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "OBSERVED_LEVELS_ONLY" || assessment.ObservationPattern != "PASS_BELOW_FAIL" || assessment.Scenario != "synthetic" || assessment.Target != lower.Target || assessment.WorkloadFingerprint == "" {
		t.Fatalf("matching campaign levels did not aggregate: %+v", assessment)
	}
	if len(assessment.Levels) != 2 || assessment.Levels[0].TestedUsers != 2 || assessment.Levels[1].TestedUsers != 4 {
		t.Fatalf("campaign levels were not sorted: %+v", assessment.Levels)
	}
	if len(assessment.PassingLevels) != 1 || assessment.PassingLevels[0] != 2 || len(assessment.FailingLevels) != 1 || assessment.FailingLevels[0] != 4 {
		t.Fatalf("observed passing levels are incomplete: %+v", assessment.PassingLevels)
	}
	if assessment.HighestPassingLevel == nil || *assessment.HighestPassingLevel != 2 || assessment.FirstFailingLevel == nil || *assessment.FirstFailingLevel != 4 || assessment.UpperBound != "BRACKETED_BY_TESTED_LEVELS" {
		t.Fatalf("tested pass/fail bracket was not made explicit: %+v", assessment)
	}
	if assessment.Demand == nil || assessment.Demand.TargetUsers != 4 || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" || assessment.DemandEvidenceID != assessment.Levels[1].Assessment.Audit.EvidenceID {
		t.Fatalf("exact level demand should retain its quality blocker: %+v", assessment.Demand)
	}
	if strings.Contains(strings.Join(assessment.Demand.Findings, " "), "nearby levels are not interpolated") {
		t.Fatalf("tested exact demand was treated as untested: %+v", assessment.Demand)
	}
}

func TestCampaignCollectionNeverInterpolatesAndDetectsNonMonotonicLevels(t *testing.T) {
	lower := auditFixtureAtUsers(2)
	markCampaignFailed(&lower)
	higher := auditFixtureAtUsers(4)
	assessment, err := BuildCampaignCollectionAssessment([]campaign.Result{lower, higher}, &capacitygoal.Goal{PeakUsers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "OBSERVED_LEVELS_ONLY" || assessment.ObservationPattern != "NON_MONOTONIC" || len(assessment.FailingLevels) != 1 || assessment.FailingLevels[0] != 2 || len(assessment.PassingLevels) != 1 || assessment.PassingLevels[0] != 4 {
		t.Fatalf("non-monotonic observations were not reported: %+v", assessment)
	}
	if assessment.HighestPassingLevel == nil || *assessment.HighestPassingLevel != 4 || assessment.FirstFailingLevel == nil || *assessment.FirstFailingLevel != 2 || assessment.UpperBound != "NON_MONOTONIC_UNKNOWN" {
		t.Fatalf("non-monotonic tested levels were presented as a bracket: %+v", assessment)
	}
	if assessment.Demand == nil || assessment.Demand.TargetUsers != 3 || len(assessment.Demand.Findings) != 1 || !strings.Contains(assessment.Demand.Findings[0], "exact calculated workload level") {
		t.Fatalf("untested demand level was interpolated: %+v", assessment.Demand)
	}
}

func TestCampaignCollectionPassingOnlyReportsUnknownUpperBound(t *testing.T) {
	first := auditFixtureAtUsers(2)
	second := auditFixtureAtUsers(4)
	assessment, err := BuildCampaignCollectionAssessment([]campaign.Result{first, second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.HighestPassingLevel == nil || *assessment.HighestPassingLevel != 4 || assessment.FirstFailingLevel != nil || assessment.UpperBound != "UNKNOWN" {
		t.Fatalf("all-pass observations must retain an unknown upper bound: %+v", assessment)
	}
}

func markCampaignFailed(e *campaign.Result) {
	e.Outcome = "FAIL"
	e.Trials = e.Trials[:1]
	e.Trials[0].Outcome = "FAIL"
	for j := range e.Trials[0].Phases {
		phase := &e.Trials[0].Phases[j]
		if phase.Name == "observation" {
			phase.Run.Outcome = "FAIL"
			phase.EvidenceState, _ = campaign.ClassifyEvidence(phase.Run)
			e.Trials[0].Phases = e.Trials[0].Phases[:j+1]
			break
		}
	}
	_, e.ObservationVariability = recomputeObservationEvidence(*e)
}

func TestCampaignCollectionRejectsMismatchesInvalidEvidenceAndDuplicateLevels(t *testing.T) {
	first := auditFixtureAtUsers(2)
	second := auditFixtureAtUsers(4)
	second.Trials[0].Phases[0].Run.Config.MaxRPS++
	assessment, err := BuildCampaignCollectionAssessment([]campaign.Result{first, second}, nil)
	if err != nil || assessment.State != "NOT_COMPARABLE" || len(assessment.PassingLevels) != 0 || len(assessment.Levels) != 2 {
		t.Fatalf("mismatched controls must remain visible per level but out of aggregate: %+v %v", assessment, err)
	}
	second = auditFixtureAtUsers(4)
	stricterPolicy := *second.StabilityPolicy
	stricterPolicy.MaxP95IncreasePercent = 15
	second.StabilityPolicy = &stricterPolicy
	assessment, err = BuildCampaignCollectionAssessment([]campaign.Result{first, second}, nil)
	if err != nil || assessment.State != "NOT_COMPARABLE" {
		t.Fatalf("different saved drift policies were combined: %+v %v", assessment, err)
	}
	second = auditFixtureAtUsers(4)
	second.Trials[0].Phases[0].Run.SchemaVersion = 10
	assessment, err = BuildCampaignCollectionAssessment([]campaign.Result{first, second}, nil)
	if err != nil || assessment.State != "NOT_COMPARABLE" {
		t.Fatalf("pre-fingerprint schema was treated as comparable: %+v %v", assessment, err)
	}
	second = auditFixtureAtUsers(4)
	second.Trials[0].Phases[0].EvidenceState = "ALTERED"
	assessment, err = BuildCampaignCollectionAssessment([]campaign.Result{first, second}, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil || assessment.State != "INVALID_EVIDENCE" || len(assessment.PassingLevels) != 0 {
		t.Fatalf("invalid evidence must suppress combined conclusions: %+v %v", assessment, err)
	}
	if assessment.Demand == nil || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" || assessment.DemandEvidenceID != "" {
		t.Fatalf("invalid collection produced an exact-level demand conclusion: %+v", assessment)
	}
	first = auditFixtureAtUsers(2)
	second = auditFixtureAtUsers(4)
	second.ObservationVariability.P95MaxMS++
	assessment, err = BuildCampaignCollectionAssessment([]campaign.Result{first, second}, nil)
	if err != nil || assessment.State != "INVALID_EVIDENCE" || len(assessment.PassingLevels) != 0 || len(assessment.FailingLevels) != 0 {
		t.Fatalf("altered per-level observation spread was aggregated: %+v %v", assessment, err)
	}
	if _, err := BuildCampaignCollectionAssessment([]campaign.Result{first, first}, nil); err == nil || !strings.Contains(err.Error(), "duplicate tested level") {
		t.Fatalf("duplicate levels were accepted: %v", err)
	}
}

func TestCampaignCollectionRequiresTwoToTwentyLevels(t *testing.T) {
	if _, err := BuildCampaignCollectionAssessment([]campaign.Result{auditFixture()}, nil); err == nil {
		t.Fatal("single campaign was accepted as a level collection")
	}
	tooMany := make([]campaign.Result, MaxCampaignCollectionSize+1)
	if _, err := BuildCampaignCollectionAssessment(tooMany, nil); err == nil {
		t.Fatal("oversized campaign collection was accepted")
	}
}
