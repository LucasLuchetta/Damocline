package calibration

import (
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestAssessmentMatchesOnlyDeclaredFixtureDimensionsAndKeepsUnknowns(t *testing.T) {
	a := passingArtifact(t)
	local := CurrentRuntime()
	a.Preflight.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	a.Preflight.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	s := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 10, Users: 10,
		MaxRPS: 100, Duration: "5s", Timeout: "1s", Steps: []config.Step{{Name: "read", Method: "GET", Path: "/synthetic"}}}
	c := AssessScenario(s, a.Preflight, local, now)
	if c.Status != "INCONCLUSIVE" {
		t.Fatalf("unverified target evidence must remain inconclusive: %+v", c)
	}
	statuses := map[string]string{}
	for _, d := range c.Dimensions {
		statuses[d.Name] = d.Status
	}
	for _, name := range []string{"workload_model", "arrival_rate", "worker_budget", "request_mix", "request_rate_ceiling", "duration", "timing", "declared_transport"} {
		if statuses[name] != "MATCHED" {
			t.Fatalf("%s was not matched: %+v", name, c.Dimensions)
		}
	}
	if statuses["response_shape"] != "UNVERIFIED" || statuses["fixture_identity"] != "UNVERIFIED" {
		t.Fatalf("response and identity uncertainty must be explicit: %+v", c.Dimensions)
	}
}

func TestAssessmentRejectsUnmeasuredRateAndStaleRuntime(t *testing.T) {
	a := passingArtifact(t)
	local := CurrentRuntime()
	a.Preflight.Generator.RunnerVersion = local.RunnerVersion
	a.Preflight.StartedAt = time.Now().Add(-26 * time.Hour).UTC().Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	s := config.Scenario{BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 3, Users: 10, MaxRPS: 100,
		Duration: "5s", Timeout: "1s", Steps: []config.Step{{Method: "GET", Path: "/"}}}
	c := AssessScenario(s, a.Preflight, local, time.Now().UTC())
	for _, d := range c.Dimensions {
		if d.Name == "arrival_rate" && d.Status != "OUTSIDE_REFERENCE" {
			t.Fatalf("unmeasured rates must not interpolate: %+v", d)
		}
		if d.Name == "calibration_age" && d.Status != "OUTSIDE_REFERENCE" {
			t.Fatalf("stale evidence accepted: %+v", d)
		}
	}
	if strings.Contains(strings.Join([]string{c.Dimensions[0].Reason}, " "), s.BaseURL) {
		t.Fatal("assessment leaked scenario URL")
	}
}

func TestAssessmentDoesNotCallMissingRevisionAMismatch(t *testing.T) {
	a := passingArtifact(t)
	local := CurrentRuntime()
	a.Preflight.Generator.RunnerVersion = "source-development"
	now := time.Now().UTC()
	a.Preflight.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	s := config.Scenario{BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 10, Users: 10, MaxRPS: 100,
		Duration: "5s", Timeout: "1s", Steps: []config.Step{{Method: "GET", Path: "/"}}}
	c := AssessScenario(s, a.Preflight, local, now)
	for _, d := range c.Dimensions {
		if d.Name == "generator_runtime" && d.Status != "UNVERIFIED" {
			t.Fatalf("unavailable revision is unknown, not a known mismatch: %+v", d)
		}
	}
}
