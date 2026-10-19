package capacityplan

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditFixture deliberately models a passing synthetic report with LIMITED
// measurement quality. No test helper can manufacture remote generator proof.
func auditFixture() campaign.Result {
	phaseRun := func() report.Result {
		r := report.Result{
			SchemaVersion: 1, Outcome: "PASS", Target: "http://127.0.0.1:18080",
			ElapsedSeconds: 10, ObservationSeconds: 10,
			Config:   report.RunConfig{Users: 2, Duration: "10s", WindowDuration: "2.5s", MaxRPS: 40, P95LimitMS: 1000, MaxErrorRate: 0.1},
			Metrics:  report.MetricSet{Total: 80, Success: 80, P95MS: 100},
			Journeys: report.MetricSet{Total: 80, Success: 80, P95MS: 100},
		}
		for i := 0; i < 4; i++ {
			r.Windows = append(r.Windows, report.WindowResult{StartSeconds: float64(i) * 2.5, EndSeconds: float64(i+1) * 2.5,
				Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 100}})
		}
		r.Quality = report.AssessQuality(r)
		return r
	}
	a, b := phaseRun(), phaseRun()
	e := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, Scenario: "synthetic", Target: a.Target,
		BaseUsers: 2, Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s",
		Budget: "2m20s", Repeats: 2, Outcome: "PASS",
	}
	for n := 1; n <= 2; n++ {
		e.Trials = append(e.Trials, campaign.Trial{Number: n, Outcome: "PASS", Phases: []campaign.Phase{
			{Name: "observation", EvidenceState: "PASS_OBSERVED", Stability: campaign.AssessStability(a), Run: a},
			{Name: "recovery", EvidenceState: "PASS_OBSERVED", Run: b},
		}})
	}
	return e
}

func TestAuditCampaignObservationalPassDoesNotCertifyCapacity(t *testing.T) {
	e := auditFixture()
	a := AuditCampaign(e)
	if a.State != "OBSERVED_PASS_ONLY" || a.MeasurementQuality != report.QualityLimited || a.RequiredPhases != 4 {
		t.Fatalf("unexpected conservative verdict: %+v", a)
	}
	if !strings.Contains(strings.Join(a.Findings, " "), "not a validated capacity ceiling") {
		t.Fatalf("missing explicit certification limitation: %+v", a)
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
	if first.Audit.TestedDuration != "40s" || first.Audit.Confidence != "NOT_ESTABLISHED" || first.Demand == nil || first.Audit.DemandTargetUsers != 2 {
		t.Fatalf("assessment is missing campaign and demand summary: %+v", first)
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

func TestAuditListsOnlyObservedFailingWorkflow(t *testing.T) {
	e := auditFixture()
	for i := range e.Trials {
		for j := range e.Trials[i].Phases {
			phase := &e.Trials[i].Phases[j]
			if phase.Name == "observation" {
				phase.Run.Profiles = []report.ProfileResult{{Name: "checkout", Journeys: report.MetricSet{Total: 10, Success: 8, Failures: 2}}}
			}
		}
	}
	a := AuditCampaign(e)
	if len(a.LimitingWorkflows) != 1 || a.LimitingWorkflows[0] != "checkout" {
		t.Fatalf("expected named observed workflow failures: %+v", a.LimitingWorkflows)
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
	if assessment.State != "INVALID_EVIDENCE" || assessment.Audit.State != "INVALID_EVIDENCE" || assessment.Demand == nil || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" {
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
	e.Trials[0].Outcome = "INCONCLUSIVE"
	e.Trials[0].Phases[0].Run.ArrivalsDropped = 1
	e.Trials[0].Phases[0].Run.Outcome = "FAIL"
	e.Trials[0].Phases[0].EvidenceState = "INVALID_DELIVERY"
	if got := AuditCampaign(e); got.State != "INCONCLUSIVE" {
		t.Fatalf("invalid delivery became a server limit: %+v", got)
	}
}
