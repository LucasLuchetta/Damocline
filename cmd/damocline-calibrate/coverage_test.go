package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func validCoverageCalibration(t *testing.T, extended bool) assessment {
	t.Helper()
	g, f, stages := testPreflightInputs(extended, 1)
	f.WorkloadProfiles = calibration.ReferenceWorkloadProfiles()
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	observations := passingWorkloadObservations()
	for index, profile := range f.WorkloadProfiles {
		if profile.Mode != "closed" {
			continue
		}
		users := int64(profile.Users)
		observations[index] = calibration.WorkloadObservation{ProfileID: profile.ID, Mode: "closed", Started: users, Completed: users, Outcome: "PASS",
			ClientCompleteBodyObservations: users, ClientResponseMinBytes: 4096, ClientResponseMaxBytes: 4096, ClientResponseTotalBytes: users * 4096,
			ServerReceived: users, ServerCompleted: users, ServerTCPConnections: 1, ServerResponseBodyBytes: users * 4096}
	}
	p, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED", observations)
	if err != nil {
		t.Fatal(err)
	}
	runs := make([]report.Result, len(stages))
	for i, s := range stages {
		runs[i] = report.Result{ArrivalsScheduled: s.Scheduled,
			IterationsStarted: s.Started, IterationsCompleted: s.Completed,
			ArrivalsDropped: s.Dropped, Outcome: s.Outcome}
	}
	return assessment{SchemaVersion: 1, Status: "REFERENCE_SMOKE_PASSED", SchedulerLag: passedSchedulerLagCalibration(),
		Preflight: p, Runs: runs}
}

func referenceShapedScenario() config.Scenario {
	return config.Scenario{
		Name: "synthetic", BaseURL: "http://127.0.0.1:8080",
		Mode: "arrival", ArrivalRate: 10, Users: 10,
		MaxRPS: 50, Duration: "5s", Timeout: "1s",
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}},
	}
}

func findDimension(t *testing.T, got coverageAssessment, name, status string) {
	t.Helper()
	for _, item := range got.Dimensions {
		if item.Name == name {
			if item.Status != status {
				t.Fatalf("%s: got %s, want %s", name, item.Status, status)
			}
			return
		}
	}
	t.Fatalf("missing dimension %s", name)
}

func TestOfflineReferenceShapeNeverBecomesCapacityApproval(t *testing.T) {
	cal := validCoverageCalibration(t, true)
	got := compareScenarioToReference(referenceShapedScenario(), cal, nil)
	if got.Status != "INCONCLUSIVE" || got.Shape != "DECLARED_SHAPE_RESEMBLES_REFERENCE" {
		t.Fatalf("fixture-like declared inputs are not sufficient for a capacity PASS: %+v", got)
	}
	if len(got.TestedArrivalRates) != 7 || got.TestedArrivalRates[6] != 100 {
		t.Fatalf("reference rates not preserved: %+v", got.TestedArrivalRates)
	}
	findDimension(t, got, "workload_model", coverageMatched)
	findDimension(t, got, "arrival_rate", coverageMatched)
	findDimension(t, got, "actor_budget", coverageMatched)
	findDimension(t, got, "declared_workload_shape", coverageMatched)
	findDimension(t, got, "connection_policy", coverageMatched)
	findDimension(t, got, "response_body_size", coverageUnknown)
	findDimension(t, got, "connections_and_server_identity", coverageUnknown)
	findDimension(t, got, "generator_host_identity", coverageUnknown)
	findDimension(t, got, "generator_workload_headroom", coverageUnknown)
}

func TestOfflineConnectionPolicyOutsideReferenceIsExplicit(t *testing.T) {
	scenario := referenceShapedScenario()
	scenario.ConnectionPolicy = "new_per_request"
	got := compareScenarioToReference(scenario, validCoverageCalibration(t, true), nil)
	if got.Status != "INCONCLUSIVE" || got.Shape == "DECLARED_SHAPE_RESEMBLES_REFERENCE" {
		t.Fatalf("untested connection policy cannot resemble the reference envelope: %+v", got)
	}
	findDimension(t, got, "connection_policy", coverageOutside)
}

func TestOfflineExplicitConnectionCapIsOutsideReference(t *testing.T) {
	scenario := referenceShapedScenario()
	scenario.MaxConnectionsPerHost = 1
	got := compareScenarioToReference(scenario, validCoverageCalibration(t, true), nil)
	if got.Status != "INCONCLUSIVE" || got.Shape == "DECLARED_SHAPE_RESEMBLES_REFERENCE" {
		t.Fatalf("explicit client connection cap must not be qualified by the default transport matrix: %+v", got)
	}
	findDimension(t, got, "declared_workload_shape", coverageOutside)
	findDimension(t, got, "connection_policy", coverageOutside)
}

func TestUnobservedRateMustNotInterpolateOrBorrowExtendedEnvelope(t *testing.T) {
	for _, tc := range []struct {
		rate     int
		extended bool
	}{
		{1, true}, {3, true}, {25, true}, {50, false}, {100, false},
	} {
		s := referenceShapedScenario()
		s.ArrivalRate = tc.rate
		s.MaxRPS = 100
		cal := validCoverageCalibration(t, tc.extended)
		got := compareScenarioToReference(s, cal, nil)
		findDimension(t, got, "arrival_rate", coverageOutside)
		if got.Shape == "DECLARED_SHAPE_RESEMBLES_REFERENCE" {
			t.Fatalf("rate %d must not pass unmeasured scenario shape", tc.rate)
		}
	}
}

func TestEveryCalibratedShapeMatchesOnlyItsExactRatePairs(t *testing.T) {
	cal := validCoverageCalibration(t, false)
	profiles := calibration.ReferenceWorkloadProfiles()
	for _, profile := range profiles {
		if profile.Mode == "closed" {
			continue
		}
		scenario := referenceShapedScenario()
		scenario.ArrivalRate = profile.ArrivalRate
		scenario.MaxRPS = profile.MaxRPS
		scenario.Users = profile.Users
		scenario.Duration = profile.Duration
		scenario.Timeout = profile.Timeout
		scenario.ConnectionPolicy = profile.ConnectionPolicy
		if profile.TransportScheme == "https" {
			scenario.BaseURL = "https://127.0.0.1"
		}
		scenario.Steps = []config.Step{{Name: "matrix", Method: profile.Method, Path: profile.Path}}
		if profile.RequestBodyBytes > 0 {
			const bodyEnv = "DAMOCLINE_TEST_MATRIX_BODY"
			t.Setenv(bodyEnv, strings.Repeat("x", profile.RequestBodyBytes))
			scenario.Steps[0].BodyEnv = bodyEnv
		}
		got := compareScenarioToReference(scenario, cal, nil)
		findDimension(t, got, "arrival_rate", coverageMatched)
		findDimension(t, got, "declared_workload_shape", coverageMatched)
		findDimension(t, got, "request_mix", coverageMatched)
		findDimension(t, got, "rate_ceiling", coverageMatched)
		findDimension(t, got, "connection_policy", coverageMatched)
		findDimension(t, got, "observation_duration", coverageMatched)
	}

	unsupported := referenceShapedScenario()
	unsupported.ArrivalRate = 7
	got := compareScenarioToReference(unsupported, cal, nil)
	findDimension(t, got, "arrival_rate", coverageOutside)
	findDimension(t, got, "declared_workload_shape", coverageMatched)
}

func TestConnectionCoverageRequiresExactProfileControlTuple(t *testing.T) {
	cal := validCoverageCalibration(t, false)
	scenario := referenceShapedScenario()
	scenario.ConnectionPolicy = "new_per_request"
	scenario.Duration = "5s"
	got := compareScenarioToReference(scenario, cal, nil)
	findDimension(t, got, "connection_policy", coverageOutside)
	findDimension(t, got, "observation_duration", coverageOutside)
	scenario.Duration = "2s"
	scenario.ArrivalRate = 7
	got = compareScenarioToReference(scenario, cal, nil)
	findDimension(t, got, "arrival_rate", coverageOutside)
	findDimension(t, got, "connection_policy", coverageOutside)
}

func TestCoverageLabelsUnsupportedDimensionsIndependently(t *testing.T) {
	cal := validCoverageCalibration(t, true)
	sc := referenceShapedScenario()
	sc.BaseURL = "https://example.invalid"
	sc.Mode = "closed"
	sc.ArrivalRate = 0
	sc.Users = 20
	sc.MaxRPS = 20
	sc.Duration = "2m"
	sc.Timeout = "8s"
	sc.Profiles = []config.Profile{
		{Name: "dashboard", Weight: 70, Steps: []config.Step{{Name: "dashboard", Method: "GET", Path: "/dashboard", Headers: map[string]string{"Authorization": "Bearer private-secret"}}}},
		{Name: "write", Weight: 30, Steps: []config.Step{{Name: "save", Method: "POST", Path: "/save", BodyEnv: "PRIVATE_DATA"}}},
	}
	sc.Steps = nil
	got := compareScenarioToReference(sc, cal, nil)
	for _, name := range []string{
		"workload_model", "arrival_rate", "actor_budget", "request_mix",
		"observation_duration", "timing_options", "transport",
	} {
		findDimension(t, got, name, coverageOutside)
	}
	if got.Status != "INCONCLUSIVE" || got.Shape != "NOT_ESTABLISHED" {
		t.Fatalf("a remote weighted workload cannot inherit the fixture PASS: %+v", got)
	}
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-secret", "PRIVATE_DATA", "example.invalid", "/dashboard", "/save"} {
		if bytes.Contains(serialized, []byte(secret)) {
			t.Fatalf("report leaked a scenario input: %s", secret)
		}
	}
}

func TestRateBudgetAndSoakAreScoped(t *testing.T) {
	cal := validCoverageCalibration(t, true)
	sc := referenceShapedScenario()
	sc.MaxRPS = 5
	findDimension(t, compareScenarioToReference(sc, cal, nil), "rate_ceiling", coverageOutside)
	sc.MaxRPS = 100
	sc.Duration = "30s"
	findDimension(t, compareScenarioToReference(sc, cal, nil), "observation_duration", coverageMatched)
	sc.ArrivalRate = 100
	findDimension(t, compareScenarioToReference(sc, cal, nil), "observation_duration", coverageOutside)
	sc.Duration = "31s"
	findDimension(t, compareScenarioToReference(sc, cal, nil), "observation_duration", coverageOutside)
}

func TestFailedAndMissingCalibrationNeverYieldPassingRates(t *testing.T) {
	cal := validCoverageCalibration(t, false)
	cal.Status = "CHECKS_FAILED"
	cal.Preflight.Status = "CHECKS_FAILED"
	cal.Preflight.HighestPassingTestedArrivalRate = 0
	got := compareScenarioToReference(referenceShapedScenario(), cal, nil)
	if len(got.TestedArrivalRates) != 0 || got.Status != "INCONCLUSIVE" {
		t.Fatalf("failed calibration exposed passing rates: %+v", got)
	}
	findDimension(t, got, "calibration", coverageOutside)
	findDimension(t, got, "declared_workload_shape", coverageMatched)
	missing := compareScenarioToReference(referenceShapedScenario(), assessment{}, errors.New("missing"))
	findDimension(t, missing, "calibration", coverageUnknown)
	findDimension(t, missing, "arrival_rate", coverageUnknown)
	findDimension(t, missing, "actor_budget", coverageUnknown)
	findDimension(t, missing, "rate_ceiling", coverageUnknown)
	if len(missing.TestedArrivalRates) != 0 {
		t.Fatal("missing calibration cannot certify any rate")
	}
}

func TestCalibrationFileLoaderValidatesConsistencyAndSize(t *testing.T) {
	cal := validCoverageCalibration(t, false)
	dir := t.TempDir()
	path := filepath.Join(dir, "calibration.json")
	write := func(a assessment) {
		t.Helper()
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(cal)
	if _, err := loadCalibrationForCoverage(path); err != nil {
		t.Fatal(err)
	}
	cal.SchedulerLag = schedulerLagCalibration{}
	write(cal)
	if _, err := loadCalibrationForCoverage(path); err == nil {
		t.Fatal("passing calibration without scheduler-lag evidence was trusted")
	}
	cal.SchedulerLag = passedSchedulerLagCalibration()
	cal.Runs[0].IterationsStarted = 0
	write(cal)
	corrupted, readErr := loadCalibrationForCoverage(path)
	if readErr == nil {
		t.Fatal("calibration stage counters disagree with run but were trusted")
	}
	coverage := compareScenarioToReference(referenceShapedScenario(), corrupted, readErr)
	if len(coverage.TestedArrivalRates) != 0 {
		t.Fatal("inconsistent artifact must not report measured rates")
	}
	findDimension(t, coverage, "arrival_rate", coverageUnknown)
	cal.Runs[0].IterationsStarted = cal.Preflight.HealthyStages[0].Started
	cal.Checks = []string{"known discrepancy"}
	write(cal)
	if _, err := loadCalibrationForCoverage(path); err == nil {
		t.Fatal("PASS with discrepancies was trusted")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxCalibrationBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCalibrationForCoverage(path); err == nil {
		t.Fatal("oversize calibration accepted")
	}
}

func TestOfflineCheckDoesNotContactTargetOrPrintScenarioSecrets(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	cal := validCoverageCalibration(t, false)
	calPath := filepath.Join(t.TempDir(), "calibration.json")
	calData, err := json.Marshal(cal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(calPath, calData, 0600); err != nil {
		t.Fatal(err)
	}

	sc := referenceShapedScenario()
	sc.BaseURL = srv.URL
	sc.Steps[0].Headers = map[string]string{"Authorization": "Bearer unique-private-marker"}
	scPath := filepath.Join(t.TempDir(), "scenario.json")
	scData, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scPath, scData, 0600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runOfflineCoverage(calPath, scPath, &out)
	if !errors.Is(err, errInconclusive) {
		t.Fatalf("should return inconclusive: %v", err)
	}
	if hits != 0 {
		t.Fatal("offline scenario check made a network request")
	}
	if strings.Contains(out.String(), "unique-private-marker") || strings.Contains(out.String(), srv.URL) ||
		strings.Contains(out.String(), scPath) {
		t.Fatalf("offline assessment leaked scenario information: %s", out.String())
	}
	var got coverageAssessment
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != coverageInconclusive {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestBadScenarioFailsWithoutNetwork(t *testing.T) {
	var out bytes.Buffer
	err := runOfflineCoverage("nonexistent-calibration", "nonexistent-scenario", &out)
	if err == nil || errors.Is(err, errInconclusive) || out.Len() != 0 {
		t.Fatalf("missing scenario must fail before assessment: %v, %s", err, out.String())
	}
}
