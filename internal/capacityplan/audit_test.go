package capacityplan

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditFixture deliberately models a passing synthetic report with LIMITED
// measurement quality. No test helper can manufacture remote generator proof.
func auditFixture() campaign.Result {
	phaseRun := func() report.Result {
		r := report.Result{
			SchemaVersion: 1, Outcome: "PASS", Target: "http://127.0.0.1:18080",
			ElapsedSeconds: 10, ObservationSeconds: 10,
			Config: report.RunConfig{Users: 2, Duration: "10s", WindowDuration: "2.5s", MaxRPS: 40, P95LimitMS: 1000, MaxErrorRate: 0.1},
			Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100},
		}
		for i := 0; i < 4; i++ {
			r.Windows = append(r.Windows, report.WindowResult{StartSeconds: float64(i) * 2.5, EndSeconds: float64(i+1) * 2.5,
				Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}})
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
