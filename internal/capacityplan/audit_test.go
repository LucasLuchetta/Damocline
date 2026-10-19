package capacityplan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditFixture deliberately models a passing synthetic report with LIMITED
// measurement quality. No test helper can manufacture remote generator proof.
func auditFixture() campaign.Result {
	return auditFixtureAtUsers(2)
}

func auditFixtureAtUsers(users int) campaign.Result {
	stabilityPolicy := campaign.DefaultStabilityPolicy()
	phaseRun := func() report.Result {
		metricSet := func(total int) report.MetricSet {
			return report.MetricSet{
				Total: total, Success: total, P50MS: 100, P95MS: 100, P99MS: 100,
				HTTP:         report.HTTPDiagnostics{FailureClassificationVersion: 1, StatusCodes: map[int]int{200: total}, Status2xx: total},
				ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: int64(total), MinBytes: 1, MaxBytes: 1, TotalBytes: int64(total)},
			}
		}
		metrics := metricSet(80)
		trace := report.HTTPTrace{RateWaitCount: 80, RequestAttemptCount: 80, CompleteCount: 80, FirstByteCount: 80, BodyCount: 80, WriteCount: 80}
		r := report.Result{
			SchemaVersion: 14, Outcome: "PASS", Target: "http://127.0.0.1:18080",
			ElapsedSeconds: 10, ObservationSeconds: 10,
			Config: report.RunConfig{Users: users, Duration: "10s", WindowDuration: "2.5s", MaxRPS: 40, P95LimitMS: 1000, MaxErrorRate: 0.1,
				Mode: "closed", ConnectionPolicy: "reuse", MinSamplesPerStep: 1, WorkloadFingerprint: "sha256:" + strings.Repeat("a", 64),
				WorkloadRevision: "workload-v1", ApplicationRevision: "app-v1", Environment: "synthetic"},
			Metrics: metrics, Journeys: report.MetricSet{Total: 80, Success: 80, P95MS: 100},
			Steps: []report.StepResult{{Name: "request", Method: "GET", Path: "/", Metrics: metrics, HTTPTrace: trace}},
		}
		for i := 0; i < 4; i++ {
			r.Windows = append(r.Windows, report.WindowResult{StartSeconds: float64(i) * 2.5, EndSeconds: float64(i+1) * 2.5,
				Metrics: metricSet(20), Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 100},
				Steps: []report.WindowMetric{{Name: "request", Metrics: metricSet(20)}}})
		}
		r.Quality = report.AssessQuality(r)
		return r
	}
	a, b := phaseRun(), phaseRun()
	e := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, Scenario: "synthetic", Target: a.Target,
		BaseUsers: users, Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s",
		Budget: "3m20s", Repeats: 2, Outcome: "PASS", Soak: "10s", StabilityPolicy: &stabilityPolicy,
	}
	for n := 1; n <= 2; n++ {
		phases := []campaign.Phase{
			{Name: "observation", EvidenceState: "PASS_OBSERVED", Stability: campaign.AssessStability(a), Run: a},
			{Name: "recovery", EvidenceState: "PASS_OBSERVED", Run: b},
		}
		if n == 2 {
			phases = append(phases, campaign.Phase{Name: "soak", EvidenceState: "PASS_OBSERVED", Stability: campaign.AssessStability(a), Run: a})
		}
		e.Trials = append(e.Trials, campaign.Trial{Number: n, Outcome: "PASS", Phases: phases})
	}
	return e
}

func TestAuditCampaignObservationalPassDoesNotCertifyCapacity(t *testing.T) {
	e := auditFixture()
	a := AuditCampaign(e)
	if a.State != "OBSERVED_PASS_ONLY" || a.MeasurementQuality != report.QualityLimited || a.RequiredPhases != 5 {
		t.Fatalf("unexpected conservative verdict: %+v", a)
	}
	if !strings.Contains(strings.Join(a.Findings, " "), "not a validated capacity ceiling") {
		t.Fatalf("missing explicit certification limitation: %+v", a)
	}
}

func TestSchemaEightRequiresRecoveryAfterObservedSpikeFailure(t *testing.T) {
	evidence := auditFixture()
	evidence.SchemaVersion = 8
	evidence.Outcome = "FAIL"
	evidence.SpikeUsers = 4
	evidence.SpikeDuration = "10s"
	evidence.Soak = ""
	options := campaign.Options{Observation: 10 * time.Second, SpikeUsers: 4, SpikeDuration: 10 * time.Second,
		Recovery: 10 * time.Second, Repeats: 2}
	evidence.Budget = campaign.PlannedBudget(options).String()
	trial := campaign.Trial{Number: 1, Outcome: "FAIL"}
	observationRun := evidence.Trials[0].Phases[0].Run
	observationRun.Config.Duration = "10s"
	observationState, _ := campaign.ClassifyEvidence(observationRun)
	observation := campaign.Phase{Name: "observation", Run: observationRun,
		EvidenceState: observationState, Stability: campaign.AssessStability(observationRun)}
	spikeRun := observationRun
	spikeRun.Config.Users, spikeRun.Outcome = 4, "FAIL"
	spikeState, _ := campaign.ClassifyEvidence(spikeRun)
	spike := campaign.Phase{Name: "spike", Run: spikeRun, EvidenceState: spikeState}
	recoveryRun := observationRun
	recoveryState, _ := campaign.ClassifyEvidence(recoveryRun)
	recovery := campaign.Phase{Name: "recovery", Run: recoveryRun, EvidenceState: recoveryState}
	trial.Phases = []campaign.Phase{observation, spike, recovery}
	evidence.Trials = []campaign.Trial{trial}
	if err := validatePartialStageEvidence(evidence); err != nil {
		t.Fatalf("failed spike with passing recovery should remain a valid failed trial: %v", err)
	}
	evidence.Trials[0].Phases = evidence.Trials[0].Phases[:2]
	if err := validatePartialStageEvidence(evidence); err == nil || !strings.Contains(err.Error(), "missing its recovery observation") {
		t.Fatalf("failed spike without recovery was accepted: %v", err)
	}
}

func TestAssessmentBindsDigestsAndOptionalDemand(t *testing.T) {
	e := auditFixture()
	first, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Audit.EvidenceID == "" || first.Audit.EvidenceID != second.Audit.EvidenceID || first.Audit.Phases[0].EvidenceID != second.Audit.Phases[0].EvidenceID {
		t.Fatalf("evidence digests must be stable and present: %+v", first.Audit)
	}
	if first.Audit.TestedDuration != "50s" || first.Audit.Confidence != "NOT_ESTABLISHED" || first.Demand == nil || first.Audit.DemandTargetUsers != 2 {
		t.Fatalf("assessment is missing campaign and demand summary: %+v", first)
	}
	if first.Confidence.Measurement != "LIMITED" || first.Confidence.WorkloadRepresentativeness != "NOT_ASSESSED" || first.Confidence.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("confidence dimensions were collapsed or overstated: %+v", first.Confidence)
	}
	var roundTrip Assessment
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &roundTrip); err != nil || roundTrip.Audit.EvidenceID != first.Audit.EvidenceID || roundTrip.Demand == nil {
		t.Fatalf("assessment JSON lost evidence links: %v %+v", err, roundTrip)
	}
	e.Trials[0].Phases[0].Run.Metrics.Success--
	changed := AuditCampaign(e)
	if changed.EvidenceID == first.Audit.EvidenceID {
		t.Fatal("changed campaign content must produce a different evidence ID")
	}
}

func TestConfidenceDimensionsDoNotCompensateForEachOther(t *testing.T) {
	e := auditFixture()
	for trialIndex := range e.Trials {
		for phaseIndex := range e.Trials[trialIndex].Phases {
			phase := &e.Trials[trialIndex].Phases[phaseIndex]
			phase.Run.Config.ProfileMixBasis = "sanitized_analytics"
			phase.Run.Config.ProfileMixReference = "fixture-week-v1"
		}
	}
	audit := CampaignAudit{State: "OBSERVED_PASS_ONLY", MeasurementQuality: report.QualityValid}
	dimensions := assessConfidence(e, audit)
	if dimensions.Measurement != "CONTROLLED_ENVELOPE_ONLY" || dimensions.WorkloadRepresentativeness != "DECLARED_UNVERIFIED" || dimensions.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("strong measurement evidence upgraded workload or decision confidence: %+v", dimensions)
	}
	e.Trials[1].Phases[1].Run.Config.ProfileMixReference = "different-reference"
	dimensions = assessConfidence(e, audit)
	if dimensions.WorkloadRepresentativeness != "INCONSISTENT_DECLARATION" || dimensions.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("conflicting declarations were treated as verified: %+v", dimensions)
	}
	audit.MeasurementQuality = report.QualityLimited
	dimensions = assessConfidence(e, audit)
	if dimensions.Measurement != "LIMITED" || dimensions.WorkloadRepresentativeness != "INCONSISTENT_DECLARATION" {
		t.Fatalf("limited measurement state was not kept independent: %+v", dimensions)
	}
}

func TestWorkflowBreachDiagnosisUsesOnlyExceededObjectives(t *testing.T) {
	profiles := []report.ProfileResult{{Name: "checkout", Journeys: report.MetricSet{Total: 10, Success: 8, Failures: 2, P95MS: 900}}}
	breaches := workflowBreaches(2, "observation", 4, profiles, report.MetricSet{}, 1000, 0.1)
	if len(breaches) != 1 || breaches[0].Workflow != "checkout" || breaches[0].Trial != 2 || breaches[0].TestedUsers != 4 || breaches[0].Phase != "observation" || len(breaches[0].Reasons) != 1 || breaches[0].Reasons[0] != "ERROR_RATE_ABOVE_LIMIT" || breaches[0].ErrorRate != 0.2 {
		t.Fatalf("expected traced workflow error-rate breach: %+v", breaches)
	}
	profiles[0].Journeys = report.MetricSet{Total: 100, Success: 99, Failures: 1, P95MS: 900}
	if got := workflowBreaches(1, "observation", 2, profiles, report.MetricSet{}, 1000, 0.1); len(got) != 0 {
		t.Fatalf("an allowed isolated error was mislabeled as a workflow breach: %+v", got)
	}
	profiles[0].Journeys.P95MS = 1200
	got := workflowBreaches(1, "spike", 8, profiles, report.MetricSet{}, 1000, 0.1)
	if len(got) != 1 || len(got[0].Reasons) != 1 || got[0].Reasons[0] != "P95_ABOVE_LIMIT" {
		t.Fatalf("latency-only breach was not identified by phase: %+v", got)
	}
}

func TestAuditCampaignRejectsAlteredQualityAndStageLabels(t *testing.T) {
	e := auditFixture()
	e.Trials[0].Phases[0].Run.Quality = &report.Quality{State: report.QualityValid}
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("self-declared quality promoted evidence: %+v", got)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].EvidenceState = "FAIL_OBSERVED"
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("altered phase classification accepted: %+v", got)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].Run.Windows[1].Metrics.P95MS = 200
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("altered stability evidence accepted: %+v", got)
	}
}

func TestAssessmentPreservesInvalidEvidenceStateWithDemandGoal(t *testing.T) {
	e := auditFixture()
	e.Trials[0].Phases[0].EvidenceState = "ALTERED"
	assessment, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "INVALID_EVIDENCE" || assessment.Audit.State != "INVALID_EVIDENCE" || assessment.Confidence.Measurement != "INVALID" || assessment.Confidence.WorkloadRepresentativeness != "NOT_ASSESSED" || assessment.Confidence.Decision != "INVALID_EVIDENCE" || assessment.Demand == nil || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("demand assessment must not mask invalid source evidence: %+v", assessment)
	}
}

func TestAuditCampaignRejectsMissingTrialsOrInvalidDelivery(t *testing.T) {
	e := auditFixture()
	e.Trials = nil
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("incomplete passing campaign accepted: %+v", got)
	}
	e = auditFixture()
	e.Outcome = "INCONCLUSIVE"
	e.Trials = e.Trials[:1]
	e.Trials[0].Outcome = "INCONCLUSIVE"
	e.Trials[0].Phases = e.Trials[0].Phases[:1]
	e.Trials[0].Phases[0].Run.ArrivalsDropped = 1
	e.Trials[0].Phases[0].Run.Outcome = "FAIL"
	e.Trials[0].Phases[0].EvidenceState = "INVALID_DELIVERY"
	if got := AuditCampaign(e); got.State != "INCONCLUSIVE" {
		t.Fatalf("invalid delivery became a server limit: %+v", got)
	}
}
