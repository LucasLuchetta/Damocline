package acceptancelab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestRunPersistsPassingRepeatedEvidence(t *testing.T) {
	calibrator := fakeCalibratorFile(t)
	outDir := filepath.Join(t.TempDir(), "acceptance-run")
	result, err := Run(context.Background(), Options{
		CalibratorPath: calibrator, OutputDir: outDir, Repeats: 2,
		RunTimeout: 30 * time.Second, Runner: &fixtureRunner{}, qualityAssessor: persistedQualityForHarnessTest,
		runtimeProvider: verifiedTestRuntime,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != "PASS_LOCAL_REFERENCE_ONLY" || len(result.Attempts) != 2 {
		t.Fatalf("unexpected final manifest summary: status=%s attempts=%d", result.Status, len(result.Attempts))
	}
	if result.Transport == nil || result.Transport.Status != "PASSED" {
		t.Fatalf("transport fixture did not pass: %+v", result.Transport)
	}
	if len(result.P95Spread) != calibratedProfiles {
		t.Fatalf("expected one descriptive spread per exact workload profile, got %d", len(result.P95Spread))
	}
	for _, spread := range result.P95Spread {
		if spread.Runs != 2 || !strings.Contains(spread.Interpretation, "not a confidence interval") {
			t.Fatalf("repeat spread made an unsupported claim: %+v", spread)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest.json")); err != nil {
		t.Fatalf("final manifest was not saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "state-004.json")); err != nil {
		t.Fatalf("incremental run state was not saved: %v", err)
	}
	for _, artifact := range []string{"repeat-01/calibration.json", "repeat-01/stdout.log", "repeat-01/stderr.log", "repeat-02/calibration.json", "transport/transport.json", "transport/stdout.log"} {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(artifact))); err != nil {
			t.Errorf("evidence artifact %q missing: %v", artifact, err)
		}
	}
	var persisted Manifest
	data, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("final manifest is not readable JSON: %v", err)
	}
	if persisted.SchemaVersion != SchemaVersion || persisted.Status != result.Status || len(persisted.Attempts) != 2 {
		t.Fatalf("persisted manifest differs from result: %+v", persisted)
	}
}

func TestRunPreservesFailedAttemptAndContinues(t *testing.T) {
	calibrator := fakeCalibratorFile(t)
	outDir := filepath.Join(t.TempDir(), "failed-acceptance-run")
	runner := &fixtureRunner{failedFirstCalibration: true}
	result, err := Run(context.Background(), Options{
		CalibratorPath: calibrator, OutputDir: outDir, Repeats: 2,
		RunTimeout: 30 * time.Second, Runner: runner, qualityAssessor: persistedQualityForHarnessTest,
		runtimeProvider: verifiedTestRuntime,
	})
	if err == nil {
		t.Fatal("expected the failed calibration repeat to prevent acceptance")
	}
	if result.Status != "FAILED" || len(result.Attempts) != 2 || result.Attempts[0].Status != "FAILED" || result.Attempts[1].Status != "PASSED" {
		t.Fatalf("failed attempt was hidden or stopped later evidence: status=%s attempts=%+v", result.Status, result.Attempts)
	}
	for _, artifact := range []string{"repeat-01/calibration.json", "repeat-01/stdout.log", "repeat-01/stderr.log", "repeat-02/calibration.json", "transport/transport.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(artifact))); err != nil {
			t.Errorf("failed-run evidence %q missing: %v", artifact, err)
		}
	}
	stderr, err := os.ReadFile(filepath.Join(outDir, "repeat-01/stderr.log"))
	if err != nil || !strings.Contains(string(stderr), "injected reference mismatch") {
		t.Fatalf("failed child diagnostic was not preserved: %q err=%v", stderr, err)
	}
	if result.Attempts[0].ArtifactSHA256 == "" || result.Attempts[0].ArtifactBytes == 0 {
		t.Fatalf("failed calibration artifact was not hashed and recorded: %+v", result.Attempts[0])
	}
}

func TestRunCancellationPersistsIncompleteEvidence(t *testing.T) {
	calibrator := fakeCalibratorFile(t)
	outDir := filepath.Join(t.TempDir(), "interrupted-acceptance-run")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	result, err := Run(ctx, Options{
		CalibratorPath: calibrator, OutputDir: outDir, Repeats: 2,
		RunTimeout: 30 * time.Second, Runner: blockingRunner{},
	})
	if err == nil {
		t.Fatal("interrupted lab must not pass")
	}
	if result.Status != "INCONCLUSIVE" || len(result.Attempts) != 1 || result.Attempts[0].Status != "INCONCLUSIVE" {
		t.Fatalf("interruption was not represented as incomplete evidence: status=%s attempts=%+v", result.Status, result.Attempts)
	}
	if _, err := os.Stat(filepath.Join(outDir, "repeat-01/stdout.log")); err != nil {
		t.Fatalf("partial running log was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest.json")); err != nil {
		t.Fatalf("final interruption manifest was not saved: %v", err)
	}
}

func TestRunRejectsExistingEvidenceDirectory(t *testing.T) {
	calibrator := fakeCalibratorFile(t)
	outDir := t.TempDir()
	if _, err := Run(context.Background(), Options{CalibratorPath: calibrator, OutputDir: outDir, Repeats: 2, Runner: &fixtureRunner{}}); err == nil {
		t.Fatal("existing evidence directory must not be overwritten")
	}
}

func TestValidateOptionsBoundsIndependentRunsAndTimeout(t *testing.T) {
	cases := []struct {
		name    string
		options Options
	}{
		{name: "too few repeats", options: Options{CalibratorPath: "calibrator", OutputDir: "out", Repeats: 1}},
		{name: "too many repeats", options: Options{CalibratorPath: "calibrator", OutputDir: "out", Repeats: 4}},
		{name: "extended repeats", options: Options{CalibratorPath: "calibrator", OutputDir: "out", Repeats: 3, Extended: true}},
		{name: "timeout too short", options: Options{CalibratorPath: "calibrator", OutputDir: "out", Repeats: 2, RunTimeout: time.Second}},
		{name: "timeout too long", options: Options{CalibratorPath: "calibrator", OutputDir: "out", Repeats: 2, RunTimeout: 5 * time.Minute}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateOptions(testCase.options); err == nil {
				t.Fatal("invalid bounds were accepted")
			}
		})
	}
}

func TestValidProfileMatrixRejectsUntestedRateAndShape(t *testing.T) {
	profiles := syntheticCalibration().Preflight.Fixture.WorkloadProfiles
	if !validProfileMatrix(profiles) {
		t.Fatal("valid exact profile matrix was rejected")
	}
	profiles[0].ArrivalRate = 7
	if validProfileMatrix(profiles) {
		t.Fatal("untested intermediate rate was accepted")
	}
	profiles = syntheticCalibration().Preflight.Fixture.WorkloadProfiles
	profiles[0].ResponseBodyBytes = 7000
	if validProfileMatrix(profiles) {
		t.Fatal("unmeasured response shape was accepted")
	}
	profiles = syntheticCalibration().Preflight.Fixture.WorkloadProfiles
	profiles[len(profiles)-7].ArrivalRate = 30
	if validProfileMatrix(profiles) {
		t.Fatal("untested TLS load rate was accepted")
	}
}

func TestHealthyStageMatrixRequiresExactRequestedRates(t *testing.T) {
	makeRuns := func(rates []int) []calibrationRun {
		runs := make([]calibrationRun, len(rates))
		for index, rate := range rates {
			planned := int64(rate * 5)
			run := calibrationRun{ArrivalsScheduled: planned, ArrivalsAdmitted: intPointer64(planned), IterationsStarted: planned, IterationsCompleted: planned, Outcome: "PASS"}
			run.Config.Mode = "arrival"
			run.Config.ArrivalRate = rate
			run.Config.MaxRPS = max(50, rate)
			run.Config.Users = 10
			run.Config.Duration = "5s"
			run.Config.RequestTimeout = "1s"
			run.Metrics.Total = int(planned)
			run.Metrics.Success = int(planned)
			runs[index] = run
		}
		return runs
	}
	if !healthyStagesMatch([]int{2, 5, 10}, makeRuns([]int{2, 5, 10}), false) {
		t.Fatal("exact baseline rates were rejected")
	}
	if healthyStagesMatch([]int{2, 5, 10, 15, 20}, makeRuns([]int{2, 5, 10, 15, 20}), true) {
		t.Fatal("incomplete extended rate matrix was accepted")
	}
	extendedRates := []int{2, 5, 10, 15, 20, 50, 100}
	if !healthyStagesMatch(extendedRates, makeRuns(extendedRates), true) {
		t.Fatal("complete extended rate matrix was rejected")
	}
}

func TestCalibrationLedgersReconcileIndependently(t *testing.T) {
	document := syntheticCalibration()
	if !aggregateServerLedgerMatches(document) || !profileLedgersMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadObservations) || !workloadRunsMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadRuns, document.WorkloadObservations) || !soakMatches(document.SoakRun) {
		t.Fatalf("complete synthetic lab fixture failed a ledger: aggregate=%t profiles=%t runs=%t soak=%t", aggregateServerLedgerMatches(document), profileLedgersMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadObservations), workloadRunsMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadRuns, document.WorkloadObservations), soakMatches(document.SoakRun))
	}
	document.Server.ResponseBodyBytes++
	if aggregateServerLedgerMatches(document) {
		t.Fatal("independent aggregate body-byte discrepancy was accepted")
	}
	document = syntheticCalibration()
	document.WorkloadObservations[0].ServerTCPConnections++
	if profileLedgersMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadObservations) {
		t.Fatal("independent profile TCP connection discrepancy was accepted")
	}
	document = syntheticCalibration()
	document.WorkloadRuns[33].Steps[0].HTTPTrace.TLSCount++
	if workloadRunsMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadRuns, document.WorkloadObservations) {
		t.Fatal("client trace and independent TLS handshake/TCP ledgers were not cross-checked")
	}
}

func TestNegativeControlsRequireExactCauseAndStatus(t *testing.T) {
	document := syntheticCalibration()
	if !overloadControlMatches(document) || !failureControlMatches(document) {
		t.Fatal("complete overload and HTTP 503 controls should pass")
	}
	document.OverloadRun.ArrivalsDropped++
	if overloadControlMatches(document) {
		t.Fatal("unclassified overload drops were accepted")
	}
	document = syntheticCalibration()
	document.FailureRun.Metrics.HTTP.StatusCodes[500] = document.FailureRun.Metrics.HTTP.StatusCodes[503]
	delete(document.FailureRun.Metrics.HTTP.StatusCodes, 503)
	if failureControlMatches(document) {
		t.Fatal("non-503 failures were accepted as the induced HTTP failure control")
	}
}

func TestTransportMatrixRequiresExactPassingFixtureSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transport.json")
	if err := os.WriteFile(path, syntheticTransport(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, gate := range assessTransportArtifact(path) {
		if gate.Status != "PASSED" {
			t.Fatalf("complete transport reference was rejected: %+v", gate)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	cases := document["cases"].([]any)
	cases[0].(map[string]any)["name"] = "different-fixture"
	data, _ = json.Marshal(document)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, gate := range assessTransportArtifact(path) {
		if gate.Name == "transport_independent_ledgers" && gate.Status != "FAILED" {
			t.Fatalf("mutated transport case identity was accepted: %+v", gate)
		}
	}
}

func TestTransportArtifactRejectsDuplicateJSONKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate-transport.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"schema_\u0076ersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, gate := range assessTransportArtifact(path) {
		if gate.Name == "transport_artifact" && gate.Status == "FAILED" {
			return
		}
	}
	t.Fatal("transport artifact with duplicate schema keys was not rejected")
}

func TestQualityRecomputationRejectsSelfDeclaredPositiveState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calibration.json")
	data, err := json.Marshal(syntheticCalibration())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var attempt Attempt
	gates := assessCalibrationArtifact(path, &attempt, false, report.AssessQuality)
	for _, gate := range gates {
		if gate.Name == "persisted_quality_recomputes" {
			if gate.Status != "FAILED" {
				t.Fatalf("self-declared positive quality was trusted: %+v", gate)
			}
			return
		}
	}
	t.Fatal("quality recomputation gate was absent")
}

func TestManifestRuntimeRequiresMatchingVerifiedCPUEntitlement(t *testing.T) {
	runtimeInfo := verifiedTestRuntime()
	environment := Environment{
		OS: runtimeInfo.OS, Architecture: runtimeInfo.Arch, GoVersion: runtimeInfo.GoVersion,
		RunnerVersion: runtimeInfo.RunnerVersion, LogicalCPUs: runtimeInfo.LogicalCPUs,
		GOMAXPROCS: runtimeInfo.GOMAXPROCS, CPUCapacityCores: runtimeInfo.CPUCapacityCores,
		CPUCapacitySource: runtimeInfo.CPUCapacitySource, CPUCapacityKnown: runtimeInfo.CPUCapacityKnown,
	}
	generator := calibration.Generator{
		OS: runtimeInfo.OS, Arch: runtimeInfo.Arch, GoVersion: runtimeInfo.GoVersion,
		LogicalCPUs: runtimeInfo.LogicalCPUs, GOMAXPROCS: runtimeInfo.GOMAXPROCS,
		CPUCapacityCores: runtimeInfo.CPUCapacityCores, CPUCapacitySource: runtimeInfo.CPUCapacitySource,
		CPUCapacityKnown: runtimeInfo.CPUCapacityKnown, RunnerVersion: "different-binary-is-expected",
	}
	attempt := Attempt{RuntimeFingerprint: generatorRuntimeFingerprint(generator)}
	if gate := assessManifestRuntime(environment, []Attempt{attempt}); gate.Status != "PASSED" {
		t.Fatalf("matching verified CPU capacity was rejected: %+v", gate)
	}
	environment.CPUCapacityCores++
	if gate := assessManifestRuntime(environment, []Attempt{attempt}); gate.Status != "FAILED" {
		t.Fatalf("different CPU entitlement must fail repeat equivalence: %+v", gate)
	}
	environment.CPUCapacityKnown = false
	if gate := assessManifestRuntime(environment, []Attempt{attempt}); gate.Status != "FAILED" {
		t.Fatalf("unknown CPU entitlement must not pass: %+v", gate)
	}
}

func TestLimitedWriterCapsEvidenceAndReportsTruncation(t *testing.T) {
	var output strings.Builder
	writer := &limitedWriter{writer: &output, limit: 4}
	if _, err := writer.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if output.String() != "abcd" || !writer.Truncated() {
		t.Fatalf("bounded output mismatch: output=%q truncated=%t", output.String(), writer.Truncated())
	}
}

type fixtureRunner struct {
	calls                  int
	failedFirstCalibration bool
}

func (runner *fixtureRunner) Run(_ context.Context, _ string, arguments []string, stdout, stderr io.Writer) Execution {
	runner.calls++
	if len(arguments) > 0 && arguments[0] == "--transport-matrix" {
		artifactPath := argumentValue(arguments, "--out")
		_ = os.WriteFile(artifactPath, syntheticTransport(), 0o600)
		_, _ = io.WriteString(stdout, "transport fixtures passed\n")
		return Execution{ExitCode: 0}
	}
	artifactPath := argumentValue(arguments, "--out")
	document := syntheticCalibration()
	execution := Execution{ExitCode: 0}
	if runner.failedFirstCalibration && strings.Contains(artifactPath, "repeat-01") {
		document.WorkloadObservations[0].ServerReceived--
		document.Status = "CHECKS_FAILED"
		document.Checks = []string{"injected reference mismatch"}
		execution.ExitCode = 1
		execution.Error = "child process exited with a non-zero status"
		_, _ = io.WriteString(stderr, "injected reference mismatch\n")
	}
	data, _ := json.Marshal(document)
	_ = os.WriteFile(artifactPath, data, 0o600)
	_, _ = io.WriteString(stdout, "calibration attempt complete\n")
	return execution
}

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ string, _ []string, stdout, _ io.Writer) Execution {
	_, _ = io.WriteString(stdout, "partial execution\n")
	<-ctx.Done()
	return Execution{ExitCode: -1, Error: "interrupted"}
}

func fakeCalibratorFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "damocline-calibrate.exe")
	if err := os.WriteFile(path, []byte("synthetic executable marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func syntheticCalibration() calibrationDocument {
	var document calibrationDocument
	document.SchemaVersion = 1
	document.Status = "REFERENCE_SMOKE_PASSED"
	document.Preflight.SchemaVersion = 10
	document.Preflight.ScenarioFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runtimeInfo := verifiedTestRuntime()
	document.Preflight.Generator = calibration.Generator{
		OS: runtimeInfo.OS, Arch: runtimeInfo.Arch, LogicalCPUs: runtimeInfo.LogicalCPUs,
		GOMAXPROCS: runtimeInfo.GOMAXPROCS, CPUCapacityCores: runtimeInfo.CPUCapacityCores,
		CPUCapacitySource: runtimeInfo.CPUCapacitySource, CPUCapacityKnown: runtimeInfo.CPUCapacityKnown,
		GoVersion: runtimeInfo.GoVersion, RunnerVersion: runtimeInfo.RunnerVersion,
	}
	document.Preflight.Fixture.HealthyRates = []int{2, 5, 10}
	shapes := []calibratedProfile{
		{ID: "get-4k-response-shape", Method: "GET", Path: "/work", ResponseBodyBytes: 4096},
		{ID: "get-empty-response-shape", Method: "GET", Path: "/matrix/get-empty", ResponseBodyBytes: 0},
		{ID: "get-1k-response-shape", Method: "GET", Path: "/matrix/get-1k", ResponseBodyBytes: 1024},
		{ID: "get-64k-response-shape", Method: "GET", Path: "/matrix/get-64k", ResponseBodyBytes: 65536},
		{ID: "post-4k-request-shape", Method: "POST", Path: "/matrix/post-4k", RequestBodyBytes: 4096, ResponseBodyBytes: 4096},
	}
	for _, connectionPolicy := range []string{"reuse", "new_per_request"} {
		profileDuration := "5s"
		seconds := 5
		idSuffix := ""
		if connectionPolicy == "new_per_request" {
			profileDuration = "2s"
			seconds = 2
			idSuffix = "-new-connection"
		}
		for _, shape := range shapes {
			for _, rate := range []int{2, 5, 10} {
				profile := shape
				profile.ID = fmt.Sprintf("%s%s-r%d", shape.ID, idSuffix, rate)
				profile.Mode = "arrival"
				profile.TransportScheme = "http"
				profile.ArrivalRate = rate
				profile.MaxRPS = 50
				profile.Users = 10
				profile.Duration = profileDuration
				profile.Timeout = "1s"
				profile.ConnectionPolicy = connectionPolicy
				document.Preflight.Fixture.WorkloadProfiles = append(document.Preflight.Fixture.WorkloadProfiles, profile)
				planned := int64(rate * seconds)
				tcpConnections, _ := calibration.ExpectedReferenceTCPConnections(calibration.WorkloadProfile{ConnectionPolicy: profile.ConnectionPolicy}, planned)
				document.WorkloadObservations = append(document.WorkloadObservations, workloadObservation{
					ProfileID: profile.ID, Mode: "arrival", ArrivalAccountingApplicable: true, Scheduled: planned, Admitted: planned, Started: planned, Completed: planned,
					Outcome: "PASS", ClientCompleteBodyObservations: planned,
					ClientResponseMinBytes: int64(profile.ResponseBodyBytes), ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: planned * int64(profile.ResponseBodyBytes),
					ServerReceived: planned, ServerCompleted: planned, ServerTCPConnections: tcpConnections,
					ServerRequestBodyBytes: planned * int64(profile.RequestBodyBytes), ServerResponseBodyBytes: planned * int64(profile.ResponseBodyBytes),
				})
				metric := report.MetricSet{
					Total: int(planned), Success: int(planned), P95MS: 21,
					HTTP:         report.HTTPDiagnostics{StatusCodes: map[int]int{200: int(planned)}, Status2xx: int(planned)},
					ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: planned, MinBytes: int64(profile.ResponseBodyBytes), MaxBytes: int64(profile.ResponseBodyBytes), TotalBytes: planned * int64(profile.ResponseBodyBytes)},
				}
				document.WorkloadRuns = append(document.WorkloadRuns, report.Result{
					ArrivalsScheduled: planned, ArrivalsAdmitted: intPointer64(planned), IterationsStarted: planned, IterationsCompleted: planned, Outcome: "PASS",
					Metrics: metric,
					Config:  report.RunConfig{Users: profile.Users, Duration: profile.Duration, RequestTimeout: profile.Timeout, ConnectionPolicy: profile.ConnectionPolicy, MaxRPS: profile.MaxRPS, Mode: profile.Mode, ArrivalRate: profile.ArrivalRate},
					Steps:   []report.StepResult{{Method: profile.Method, Path: profile.Path, Metrics: metric}},
				})
			}
		}
	}
	for _, rate := range []int{2, 5, 10, 20, 40} {
		profile := calibratedProfile{ID: fmt.Sprintf("https-get-4k-response-shape-r%d", rate), Mode: "arrival", TransportScheme: "https", Method: "GET", Path: "/matrix/https-4k",
			ResponseBodyBytes: 4096, ArrivalRate: rate, MaxRPS: 50, Users: 10, Duration: "5s", Timeout: "1s", ConnectionPolicy: "reuse"}
		document.Preflight.Fixture.WorkloadProfiles = append(document.Preflight.Fixture.WorkloadProfiles, profile)
		planned := int64(rate * 5)
		document.WorkloadObservations = append(document.WorkloadObservations, workloadObservation{
			ProfileID: profile.ID, Mode: "arrival", ArrivalAccountingApplicable: true, Scheduled: planned, Admitted: planned, Started: planned, Completed: planned,
			Outcome: "PASS", ClientCompleteBodyObservations: planned, ClientTLSHandshakes: 1,
			ClientResponseMinBytes: 4096, ClientResponseMaxBytes: 4096, ClientResponseTotalBytes: planned * 4096,
			ServerReceived: planned, ServerCompleted: planned, ServerTCPConnections: 1, ServerResponseBodyBytes: planned * 4096,
		})
		metric := report.MetricSet{Total: int(planned), Success: int(planned), P95MS: 21,
			HTTP:         report.HTTPDiagnostics{StatusCodes: map[int]int{200: int(planned)}, Status2xx: int(planned)},
			ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: planned, MinBytes: 4096, MaxBytes: 4096, TotalBytes: planned * 4096}}
		document.WorkloadRuns = append(document.WorkloadRuns, report.Result{
			ArrivalsScheduled: planned, ArrivalsAdmitted: intPointer64(planned), IterationsStarted: planned, IterationsCompleted: planned, Outcome: "PASS",
			Metrics: metric, Config: report.RunConfig{Users: 10, Duration: "5s", RequestTimeout: "1s", ConnectionPolicy: "reuse", MaxRPS: 50, Mode: "arrival", ArrivalRate: rate},
			Steps: []report.StepResult{{Method: "GET", Path: profile.Path, Metrics: metric, HTTPTrace: report.HTTPTrace{TLSCount: 1}}},
		})
	}
	for _, users := range []int{1, 2, 5, 10, 20, 50} {
		maxRPS := 100
		profile := calibratedProfile{ID: fmt.Sprintf("closed-get-4k-vu-%d", users), Mode: "closed", TransportScheme: "http", Method: "GET", Path: "/work",
			ResponseBodyBytes: 4096, MaxRPS: maxRPS, Users: users, Duration: "2s", Timeout: "1s", ConnectionPolicy: "reuse", RampUp: "0s", ThinkTime: "0s"}
		document.Preflight.Fixture.WorkloadProfiles = append(document.Preflight.Fixture.WorkloadProfiles, profile)
		iterations := int64(users)
		document.WorkloadObservations = append(document.WorkloadObservations, workloadObservation{
			ProfileID: profile.ID, Mode: "closed", Started: iterations, Completed: iterations, Outcome: "PASS", ClientCompleteBodyObservations: iterations,
			ClientResponseMinBytes: 4096, ClientResponseMaxBytes: 4096, ClientResponseTotalBytes: iterations * 4096,
			ServerReceived: iterations, ServerCompleted: iterations, ServerTCPConnections: 1, ServerResponseBodyBytes: iterations * 4096,
		})
		metric := report.MetricSet{Total: int(iterations), Success: int(iterations), P95MS: 21,
			HTTP:         report.HTTPDiagnostics{StatusCodes: map[int]int{200: int(iterations)}, Status2xx: int(iterations)},
			ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: iterations, MinBytes: 4096, MaxBytes: 4096, TotalBytes: iterations * 4096}}
		document.WorkloadRuns = append(document.WorkloadRuns, report.Result{
			ArrivalsScheduled: iterations, IterationsStarted: iterations, IterationsCompleted: iterations, ArrivalsAdmitted: intPointer64(0), Outcome: "PASS", Metrics: metric,
			Config: report.RunConfig{Users: users, Duration: "2s", RequestTimeout: "1s", ConnectionPolicy: "reuse", MaxRPS: maxRPS, Mode: "closed", RampUp: "0s", ThinkTime: "0s"},
			Steps:  []report.StepResult{{Method: "GET", Path: "/work", Metrics: metric}},
		})
	}
	for _, rate := range document.Preflight.Fixture.HealthyRates {
		planned := int64(rate * 5)
		run := calibrationRun{ArrivalsScheduled: planned, ArrivalsAdmitted: intPointer64(planned), IterationsStarted: planned, IterationsCompleted: planned, Outcome: "PASS"}
		run.Config.Mode = "arrival"
		run.Config.ArrivalRate = rate
		run.Config.MaxRPS = max(50, rate)
		run.Config.Users = 10
		run.Config.Duration = "5s"
		run.Config.RequestTimeout = "1s"
		run.Metrics.Total = int(planned)
		run.Metrics.Success = int(planned)
		document.Runs = append(document.Runs, run)
	}
	var profileRequests, profileRequestBytes, profileResponseBytes int64
	for _, observation := range document.WorkloadObservations {
		profileRequests += observation.Completed
		profileRequestBytes += observation.ServerRequestBodyBytes
		profileResponseBytes += observation.ServerResponseBodyBytes
	}
	const soakRequests int64 = 300
	var healthyRequests int64
	for _, rate := range document.Preflight.Fixture.HealthyRates {
		healthyRequests += int64(rate * 5)
	}
	document.Server = calibrationServer{
		Received: healthyRequests + profileRequests + soakRequests, Completed: healthyRequests + profileRequests + soakRequests,
		Success: healthyRequests + profileRequests + soakRequests, RequestBodyBytes: profileRequestBytes,
		ResponseBodyBytes: healthyRequests*4096 + profileResponseBytes + soakRequests*4096,
	}
	document.SoakRun = report.Result{
		ArrivalsScheduled: soakRequests, ArrivalsAdmitted: intPointer64(soakRequests), IterationsStarted: soakRequests,
		IterationsCompleted: soakRequests, SchedulerLagSamples: soakRequests, SchedulerLagP95MS: 1, SchedulerLagMaxMS: 2, Outcome: "PASS",
		Metrics: report.MetricSet{Success: int(soakRequests), Total: int(soakRequests), ResponseBody: report.ResponseBodyDiagnostics{
			CompleteObservations: soakRequests, MinBytes: 4096, MaxBytes: 4096, TotalBytes: soakRequests * 4096,
		}},
		Quality:           &report.Quality{State: "VALID_WITHIN_ENVELOPE"},
		ReferenceEvidence: &report.ControlledReferenceEvidence{ServerReceived: soakRequests, ServerCompleted: soakRequests, ServerSuccess: soakRequests},
	}
	document.OverloadRun = calibrationOverloadRun{ArrivalsScheduled: 50, ArrivalsDropped: 33, ArrivalsDroppedBusy: 33, IterationsStarted: 17, Outcome: "FAIL"}
	document.OverloadRun.Metrics.Total = 17
	document.OverloadServer = calibrationServer{Received: 17, Completed: 17, Success: 17}
	document.FailureRun.Metrics.Total = 10
	document.FailureRun.Metrics.Success = 8
	document.FailureRun.Metrics.Failures = 2
	document.FailureRun.Metrics.HTTP = report.HTTPDiagnostics{
		StatusCodes: map[int]int{200: 8, 503: 2}, Status2xx: 8, Status5xx: 2,
	}
	document.FailureRun.Outcome = "FAIL"
	document.FailureServer = calibrationServer{Received: 10, Completed: 10, Success: 8, Failures: 2}
	return document
}

func verifiedTestRuntime() calibration.Runtime {
	runtimeInfo := calibration.CurrentRuntime()
	if runtimeInfo.LogicalCPUs < 1 {
		runtimeInfo.LogicalCPUs = 1
	}
	if runtimeInfo.GOMAXPROCS < 1 {
		runtimeInfo.GOMAXPROCS = 1
	}
	runtimeInfo.CPUCapacityCores = float64(min(runtimeInfo.LogicalCPUs, runtimeInfo.GOMAXPROCS))
	runtimeInfo.CPUCapacitySource = "test_fixture_runtime_capacity"
	runtimeInfo.CPUCapacityKnown = true
	return runtimeInfo
}

func syntheticTransport() []byte {
	type expectedCase struct {
		name              string
		protocol          string
		status            int
		bytes             int
		connectionsReused int
	}
	expected := []expectedCase{
		{name: "http-empty", protocol: "http", status: 200, bytes: 0},
		{name: "http-1k", protocol: "http", status: 200, bytes: 1024},
		{name: "http-4k", protocol: "http", status: 200, bytes: 4096},
		{name: "http-64k", protocol: "http", status: 200, bytes: 65536},
		{name: "http-keepalive", protocol: "http", status: 200, bytes: 1024, connectionsReused: 2},
		{name: "http-connection-close", protocol: "http", status: 200, bytes: 1024},
		{name: "http-tail-delay", protocol: "http", status: 200, bytes: 1024},
		{name: "http-streaming", protocol: "http", status: 200, bytes: 4096},
		{name: "http-503", protocol: "http", status: 503, bytes: 128},
		{name: "tls-private-fixture", protocol: "https", status: 200, bytes: 4096},
	}
	cases := make([]map[string]any, len(expected))
	for index, testCase := range expected {
		cases[index] = map[string]any{
			"name": testCase.name, "protocol": testCase.protocol, "expected_status": testCase.status,
			"expected_bytes": testCase.bytes, "requests": 3, "server_received": 3,
			"server_completed": 3, "client_completed": 3, "connections_reused": testCase.connectionsReused,
			"first_byte_observed": true, "status": "PASS",
		}
	}
	data, _ := json.Marshal(map[string]any{
		"schema_version": 1, "status": "TRANSPORT_FIXTURES_PASSED", "kind": "http_transport_reference",
		"bounded_to_loopback": true, "requests_total": 30, "cases": cases, "limitations": []string{"synthetic test evidence", "loopback only", "not capacity evidence"},
	})
	return data
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}

func intPointer64(value int64) *int64 { return &value }

func persistedQualityForHarnessTest(run report.Result) *report.Quality { return run.Quality }
