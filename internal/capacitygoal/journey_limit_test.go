package capacitygoal

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func thinkTimeSavedRun(journeyLimitMS float64) report.Result {
	request := report.MetricSet{Total: 100, Success: 100, P95MS: 40}
	journey := report.MetricSet{Total: 20, Success: 20, P95MS: 8200}
	return report.Result{
		SchemaVersion: report.CurrentSchemaVersion,
		Config:        report.RunConfig{P95LimitMS: 500, JourneyP95LimitMS: journeyLimitMS, MaxErrorRate: 0.01, ThinkTime: "2s"},
		Metrics:       request, Journeys: journey,
		Steps:    []report.StepResult{{Name: "a", Metrics: request}},
		Profiles: []report.ProfileResult{{Name: "browse", Journeys: journey}},
		Windows: []report.WindowResult{{Metrics: request, Journeys: journey,
			Steps: []report.WindowMetric{{Name: "a", Metrics: request}}, Profiles: []report.WindowMetric{{Name: "browse", Metrics: journey}}}},
	}
}

func TestOfflineSLOBreachUsesSavedJourneyLimit(t *testing.T) {
	if savedRunSLOBreach(thinkTimeSavedRun(10000)) || campaignSLOBreach(thinkTimeSavedRun(10000)) {
		t.Fatal("journeys within the saved journey_p95_limit_ms were treated as a breach")
	}
	if !savedRunSLOBreach(thinkTimeSavedRun(8000)) || !campaignSLOBreach(thinkTimeSavedRun(8000)) {
		t.Fatal("journeys above the saved journey_p95_limit_ms were not a breach")
	}
	if !savedRunSLOBreach(thinkTimeSavedRun(0)) || !campaignSLOBreach(thinkTimeSavedRun(0)) {
		t.Fatal("absent journey limit must keep comparing journeys with p95_limit_ms")
	}
	slowRequests := thinkTimeSavedRun(10000)
	slowRequests.Windows[0].Steps[0].Metrics.P95MS = 600
	if !savedRunSLOBreach(slowRequests) {
		t.Fatal("request-window steps must keep p95_limit_ms when a journey limit is set")
	}
	slowProfile := thinkTimeSavedRun(10000)
	slowProfile.Windows[0].Profiles[0].Metrics.P95MS = 12000
	if !savedRunSLOBreach(slowProfile) {
		t.Fatal("window profile journeys above the journey limit were not a breach")
	}
}

func TestAssessableRunSchemaKeepsV34Reports(t *testing.T) {
	if !assessableRunSchema(34) || !assessableRunSchema(report.CurrentSchemaVersion) || assessableRunSchema(33) || assessableRunSchema(report.CurrentSchemaVersion+1) {
		t.Fatal("assessable run schema range is wrong")
	}
}
