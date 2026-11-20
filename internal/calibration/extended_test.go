package calibration

import (
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestAssessExtendedEnvelopeUsesExactMatchingInterface(t *testing.T) {
	a := passingArtifact(t)
	record := a.Preflight
	local := testRuntime()
	record.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	record.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	record.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1", Users: 5, Duration: "2s", Mode: "closed", MaxRPS: 100, Timeout: "1s", ConnectionPolicy: "reuse", RampUp: "0s", ThinkTime: "0s", P95LimitMS: 500, MaxErrorRate: 0.1, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	if got := AssessExtendedEnvelope(scenario, record, local, now); got.Status != "READY" {
		t.Fatalf("exact calibrated level should be READY: %+v", got)
	}
	higher := scenario
	higher.Users = 60
	if got := AssessExtendedEnvelope(higher, record, local, now); got.Status == "READY" {
		t.Fatalf("uncalibrated extended level was interpolated: %+v", got)
	}
	if got := AssessExtendedEnvelope(scenario, record, local, now.Add(48*time.Hour)); got.Status == "READY" {
		t.Fatalf("stale calibration covered an extended level: %+v", got)
	}

	previous := ExtendedCoverageAssessors
	t.Cleanup(func() { ExtendedCoverageAssessors = previous })
	ExtendedCoverageAssessors = nil
	if got := AssessExtendedEnvelope(scenario, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("no registered assessor must block: %+v", got)
	}
	ExtendedCoverageAssessors = []ExtendedCoverageAssessor{
		func(config.Scenario, Record, Runtime, time.Time) ExtendedCoverage {
			return ExtendedCoverage{Status: "BLOCKED", Reason: "first"}
		},
		func(s config.Scenario, _ Record, _ Runtime, _ time.Time) ExtendedCoverage {
			if s.Users == 60 {
				return ExtendedCoverage{Status: "READY", Reason: "workload-specific artifact"}
			}
			return ExtendedCoverage{Status: "BLOCKED"}
		},
	}
	if got := AssessExtendedEnvelope(higher, record, local, now); got.Status != "READY" || got.Reason != "workload-specific artifact" {
		t.Fatalf("registered assessor did not compose: %+v", got)
	}
}
