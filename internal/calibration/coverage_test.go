package calibration

import (
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestValidateClosedLoopEnvelopeMatchesExactPlanAndDimensionStates(t *testing.T) {
	matched := Dimension{Name: "exact_profile", Status: "MATCHED", Reason: "exact tested tuple"}
	ready := ClosedLoopEnvelope{Status: "READY", Levels: []ClosedLoopAssessment{{
		Users: 2, Status: "READY", Reason: "exact local profile", Dimensions: []Dimension{matched},
	}}}
	if err := ValidateClosedLoopEnvelope(ready, []int{2}); err != nil {
		t.Fatalf("consistent exact READY envelope rejected: %v", err)
	}
	mixed := ClosedLoopEnvelope{Status: "BLOCKED", Levels: []ClosedLoopAssessment{
		{Users: 2, Status: "READY", Reason: "exact local profile", Dimensions: []Dimension{matched}},
		{Users: 4, Status: "BLOCKED", Reason: "no exact profile", Dimensions: []Dimension{{Name: "exact_profile", Status: "OUTSIDE_REFERENCE", Reason: "not measured"}}},
	}}
	if err := ValidateClosedLoopEnvelope(mixed, []int{2, 4}); err != nil {
		t.Fatalf("valid mixed base/spike coverage was rejected: %v", err)
	}
	missingSpike := mixed
	missingSpike.Levels = missingSpike.Levels[:1]
	if err := ValidateClosedLoopEnvelope(missingSpike, []int{2, 4}); err == nil {
		t.Fatal("envelope missing a planned spike level was accepted")
	}
	for name, mutate := range map[string]func(*ClosedLoopEnvelope){
		"wrong planned VU level": func(e *ClosedLoopEnvelope) { e.Levels[0].Users = 3 },
		"READY with blocker":     func(e *ClosedLoopEnvelope) { e.Levels[0].Dimensions[0].Status = "OUTSIDE_REFERENCE" },
		"duplicate dimension": func(e *ClosedLoopEnvelope) {
			e.Levels[0].Dimensions = append(e.Levels[0].Dimensions, e.Levels[0].Dimensions[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			envelope := ready
			mutate(&envelope)
			if err := ValidateClosedLoopEnvelope(envelope, []int{2}); err == nil {
				t.Fatal("malformed closed-loop calibration envelope was accepted")
			}
		})
	}
}

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

func TestClosedLoopEnvelopeRequiresExactVersionedVUProfile(t *testing.T) {
	a := passingArtifact(t)
	record := a.Preflight
	local := testRuntime()
	record.Generator.RunnerVersion = local.RunnerVersion
	now := time.Now().UTC()
	record.StartedAt = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	record.FinishedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1", Users: 5, Duration: "2s", Mode: "closed", MaxRPS: 100, Timeout: "1s", ConnectionPolicy: "reuse", RampUp: "0s", ThinkTime: "0s", P95LimitMS: 500, MaxErrorRate: 0.1, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	got := AssessClosedLoopEnvelope(scenario, []int{5}, record, local, now)
	if got.Status != "READY" || got.Levels[0].Status != "READY" {
		t.Fatalf("exact closed-loop profile should match: %+v", got)
	}
	if got := AssessClosedLoopEnvelope(scenario, []int{6}, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("unmeasured VU level was interpolated: %+v", got)
	}
	changed := scenario
	changed.MaxRPS = 19
	if got := AssessClosedLoopEnvelope(changed, []int{5}, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("different request cap borrowed a VU profile: %+v", got)
	}
	changed = scenario
	changed.RampUp = "1s"
	if got := AssessClosedLoopEnvelope(changed, []int{5}, record, local, now); got.Status != "BLOCKED" {
		t.Fatalf("ramp-up change borrowed a VU profile: %+v", got)
	}
	stale := record
	stale.FinishedAt = now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
	if got := AssessClosedLoopEnvelope(scenario, []int{5}, stale, local, now); got.Status != "BLOCKED" {
		t.Fatalf("stale closed-loop calibration remained usable: %+v", got)
	}
	legacy := record
	legacy.SchemaVersion = 8
	legacy.Fixture.WorkloadProfiles = referenceWorkloadProfilesV8()
	legacy.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV8())
	refreshArtifactFingerprint(t, &legacy)
	if got := AssessClosedLoopEnvelope(scenario, []int{5}, legacy, local, now); got.Status != "BLOCKED" {
		t.Fatalf("schema-v8 arrival profiles qualified VUs: %+v", got)
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
