package calibration

import (
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestAssessmentMatchesOnlyDeclaredFixtureDimensionsAndKeepsUnknowns(t *testing.T) {
	a := passingArtifact(t)
	local := testRuntime()
	a.Preflight.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	a.Preflight.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	s := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 10, Users: 10,
		MaxRPS: 50, Duration: "5s", Timeout: "1s", Steps: []config.Step{{Name: "read", Method: "GET", Path: "/synthetic"}}}
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
	if statuses["connection_policy"] != "MATCHED" {
		t.Fatalf("reusable connection policy should match the fixed reference: %+v", c.Dimensions)
	}
	if statuses["response_shape"] != "UNVERIFIED" || statuses["fixture_identity"] != "UNVERIFIED" {
		t.Fatalf("response and identity uncertainty must be explicit: %+v", c.Dimensions)
	}
	if statuses["declared_workload_shape"] != "OUTSIDE_REFERENCE" {
		t.Fatalf("different request path must not be treated as equivalent: %+v", c.Dimensions)
	}
}

func TestMatrixWorkloadShapeMatchesExactPOSTBodySizeWithoutRecordingBody(t *testing.T) {
	a := passingArtifact(t)
	scenario := config.Scenario{
		BaseURL: "http://127.0.0.1", Users: 10, Duration: "5s", Timeout: "1s",
		MaxRPS: 50, Mode: "arrival", ArrivalRate: 10,
		Steps: []config.Step{{Name: "submit", Method: "POST", Path: "/matrix/post-4k", BodyEnv: "DAMOCLINE_TEST_BODY"}},
	}
	t.Setenv("DAMOCLINE_TEST_BODY", strings.Repeat("x", 4<<10))
	matched := AssessDeclaredWorkloadShape(scenario, a.Preflight)
	if matched.Status != "MATCHED" {
		t.Fatalf("exact POST path and body size should match: %+v", matched)
	}
	t.Setenv("DAMOCLINE_TEST_BODY", strings.Repeat("x", (4<<10)-1))
	if got := AssessDeclaredWorkloadShape(scenario, a.Preflight); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("different POST body size must be outside matrix: %+v", got)
	}
	t.Setenv("DAMOCLINE_TEST_BODY", strings.Repeat("x", 4<<10))
	scenario.BaseURL = "https://127.0.0.1"
	if got := AssessDeclaredWorkloadShape(scenario, a.Preflight); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("HTTPS declaration must not match an HTTP-only profile: %+v", got)
	}
	if strings.Contains(matched.Reason, "DAMOCLINE_TEST_BODY") || strings.Contains(matched.Reason, strings.Repeat("x", 32)) {
		t.Fatal("workload assessment leaked request body input")
	}
}

func TestMatrixRateCeilingRequiresExactCalibratedPair(t *testing.T) {
	a := passingArtifact(t)
	local := testRuntime()
	a.Preflight.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	a.Preflight.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	scenario := config.Scenario{BaseURL: "http://127.0.0.1", Mode: "arrival", ArrivalRate: 10, Users: 10,
		MaxRPS: 51, Duration: "5s", Timeout: "1s", Steps: []config.Step{{Method: "GET", Path: "/work"}}}
	coverage := AssessScenario(scenario, a.Preflight, local, now)
	for _, dimension := range coverage.Dimensions {
		if dimension.Name == "request_rate_ceiling" && dimension.Status != "OUTSIDE_REFERENCE" {
			t.Fatalf("untested rate ceiling must not match: %+v", dimension)
		}
	}
}

func TestArrivalEnvelopeRequiresExactHealthyMatrixAndRuntime(t *testing.T) {
	artifactFixture := passingArtifact(t)
	record := artifactFixture.Preflight
	local := testRuntime()
	record.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	record.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	record.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	scenario := config.Scenario{Name: "Synthetic reference workload", BaseURL: "http://127.0.0.1", Mode: "arrival", ArrivalRate: 2, Users: 10,
		MaxRPS: 50, Duration: "2m", Timeout: "1s", P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	fixtureScenario := scenario
	fixtureScenario.Duration = "5s"
	validated, err := config.Parse(fixtureScenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	record.FixtureWorkloadFingerprint = validated.WorkloadFingerprint
	if err := ValidateRecord(record); err != nil {
		t.Fatalf("calibration fixture invalid: %v", err)
	}
	if got := AssessArrivalEnvelope(scenario, []int{2, 5, 10}, record, local, now); got.Status != "READY" {
		t.Fatalf("exact matrix profiles should cover the declared generator envelope: %+v", got)
	}
	if got := AssessArrivalEnvelope(scenario, []int{2, 7}, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("unmeasured rates must not be interpolated: %+v", got)
	}
	wrongShape := scenario
	wrongShape.Steps = []config.Step{{Name: "work", Method: "GET", Path: "/other"}}
	if got := AssessArrivalEnvelope(wrongShape, []int{2}, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("uncalibrated request shape must block: %+v", got)
	}
	stale := record
	stale.FinishedAt = now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
	if got := AssessArrivalEnvelope(scenario, []int{2}, stale, local, now); got.Status != "BLOCKED" {
		t.Fatalf("stale calibration must block: %+v", got)
	}
	changedRuntime := local
	changedRuntime.GOMAXPROCS++
	if got := AssessArrivalEnvelope(scenario, []int{2}, record, changedRuntime, now); got.Status != "BLOCKED" {
		t.Fatalf("changed runtime must block: %+v", got)
	}
}

func TestClosedLoopEnvelopeNeverTreatsArrivalProfilesAsVUs(t *testing.T) {
	record := passingArtifact(t).Preflight
	got := AssessClosedLoopEnvelope([]int{2, 5, 2}, record, testRuntime(), time.Now().UTC())
	if got.Status != "BLOCKED" || len(got.Levels) != 3 || got.Levels[0].Status != "BLOCKED" || got.Levels[2].Status != "BLOCKED" {
		t.Fatalf("arrival matrix or duplicate VU levels were accepted: %+v", got)
	}
	if got.Levels[0].Dimensions[0].Name != "workload_model" {
		t.Fatalf("missing closed-loop model reason: %+v", got.Levels[0])
	}
}

func TestConnectionPolicyRequiresExactProfileControls(t *testing.T) {
	a := passingArtifact(t)
	local := testRuntime()
	a.Preflight.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	a.Preflight.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	a.Preflight.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	scenario := config.Scenario{BaseURL: "http://127.0.0.1", Mode: "arrival", ArrivalRate: 10, Users: 10,
		MaxRPS: 50, Duration: "2s", Timeout: "1s", ConnectionPolicy: "new_per_request",
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	coverage := AssessScenario(scenario, a.Preflight, local, now)
	for _, dimension := range coverage.Dimensions {
		if dimension.Name == "connection_policy" && dimension.Status != "MATCHED" {
			t.Fatalf("exact new-connection profile should match: %+v", coverage.Dimensions)
		}
	}
	if got := AssessDeclaredWorkloadShape(scenario, a.Preflight); got.Status != "MATCHED" {
		t.Fatalf("exact new-connection workload fingerprint should match: %+v", got)
	}
	scenario.MaxConnectionsPerHost = 1
	if got := AssessDeclaredWorkloadShape(scenario, a.Preflight); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("explicit connection cap must not inherit default-pool calibration: %+v", got)
	}
	if exactProfileControlsMatch(a.Preflight.Fixture.WorkloadProfiles, scenario, scenario.ConnectionPolicy) {
		t.Fatal("explicit connection cap inherited a fixed profile control tuple")
	}
	scenario.Duration = "5s"
	coverage = AssessScenario(scenario, a.Preflight, local, now)
	for _, dimension := range coverage.Dimensions {
		if dimension.Name == "connection_policy" && dimension.Status != "OUTSIDE_REFERENCE" {
			t.Fatalf("new-connection policy with reusable-profile duration must not match: %+v", dimension)
		}
	}
}

func TestNewConnectionProfileDoesNotInterpolateRates(t *testing.T) {
	a := passingArtifact(t)
	scenario := config.Scenario{BaseURL: "http://127.0.0.1", Mode: "arrival", ArrivalRate: 7, Users: 10,
		MaxRPS: 100, Duration: "2s", Timeout: "1s", ConnectionPolicy: "new_per_request",
		Steps: []config.Step{{Method: "GET", Path: "/work"}}}
	if got := exactProfileControlsMatch(a.Preflight.Fixture.WorkloadProfiles, scenario, scenario.ConnectionPolicy); got {
		t.Fatal("an unmeasured arrival rate must not inherit a neighboring connection profile")
	}
}

func TestExactReferenceSoakRetainsControlledReuseCoverage(t *testing.T) {
	scenario := config.Scenario{Mode: "arrival", ArrivalRate: 10, Users: 10, MaxRPS: 50,
		Duration: "30s", Timeout: "1s"}
	if !exactReferenceSoakControlsMatch(scenario, "reuse") {
		t.Fatal("the fixed controlled reuse soak should retain its exact reference controls")
	}
	if exactReferenceSoakControlsMatch(scenario, "new_per_request") {
		t.Fatal("the 30-second soak must not imply new-per-request calibration")
	}
	scenario.MaxConnectionsPerHost = 10
	if exactReferenceSoakControlsMatch(scenario, "reuse") {
		t.Fatal("an explicit connection cap must not inherit default-pool soak evidence")
	}
	scenario.MaxRPS = 100
	if exactReferenceSoakControlsMatch(scenario, "reuse") {
		t.Fatal("a different request-rate ceiling must not inherit the controlled soak")
	}
}

func TestDeclaredWorkloadShapeRequiresExactFingerprintAndVersionedRecord(t *testing.T) {
	a := passingArtifact(t)
	rec := a.Preflight
	scenario := config.Scenario{
		BaseURL: "http://127.0.0.1:8080", Users: 10, Duration: "5s", Timeout: "1s",
		MaxRPS: 50, Mode: "arrival", ArrivalRate: 10,
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}},
	}
	matched := AssessDeclaredWorkloadShape(scenario, rec)
	if matched.Status != "MATCHED" {
		t.Fatalf("exact fixed-fixture request shape should match within its narrow scope: %+v", matched)
	}
	scenario.Steps[0].Path = "/search"
	if got := AssessDeclaredWorkloadShape(scenario, rec); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("different path must be outside the exact workload fingerprint: %+v", got)
	}
	scenario.Steps[0].Path = "/work"
	scenario.ProfileMixBasis = "sanitized_analytics"
	scenario.ProfileMixReference = "summary-v1"
	if got := AssessDeclaredWorkloadShape(scenario, rec); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("externally sourced mix cannot match the one-profile fixture: %+v", got)
	}
	rec.SchemaVersion = 1
	rec.FixtureWorkloadFingerprint = ""
	if got := AssessDeclaredWorkloadShape(scenario, rec); got.Status != "UNVERIFIED" {
		t.Fatalf("legacy calibration without workload fingerprint must be unknown: %+v", got)
	}
}

func TestSchemaTwoRejectsAlteredWorkloadFingerprint(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.FixtureWorkloadFingerprint = "sha256:" + strings.Repeat("0", 64)
	if err := ValidateRecord(a.Preflight); err == nil {
		t.Fatal("altered reference workload fingerprint was accepted")
	}
}

func TestAssessmentRejectsUnmeasuredRateAndStaleRuntime(t *testing.T) {
	a := passingArtifact(t)
	local := testRuntime()
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
	local := testRuntime()
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
