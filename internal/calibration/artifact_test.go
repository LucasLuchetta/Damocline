package calibration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func passedLagFixture() SchedulerLag {
	c := SchedulerLag{Method: SchedulerLagMethod, Rate: SchedulerLagRate, Duration: SchedulerLagDuration.String(),
		ToleranceMS: float64(SchedulerLagTolerance.Microseconds()) / 1000, Status: "PASSED"}
	for _, pause := range []time.Duration{90, 130, 210} {
		lag := float64((pause*time.Millisecond - time.Second/SchedulerLagRate).Microseconds()) / 1000
		for range 3 {
			c.Observations = append(c.Observations, SchedulerLagObservation{InjectedPauseMS: float64(pause), ExpectedMaxLagMS: lag, ObservedMaxLagMS: lag, Planned: 5, Admitted: 5, Outcome: "PASSED"})
		}
	}
	return c
}

func TestSourceRunnerVersionHasStableVerifiableIdentity(t *testing.T) {
	first := SourceRunnerVersion()
	second := SourceRunnerVersion()
	if first == "source-development" || first != second || !versionPattern.MatchString(first) {
		t.Fatalf("runner identity is unavailable, malformed, or unstable: first=%q second=%q", first, second)
	}
}

func passingArtifact(t *testing.T) artifact {
	t.Helper()
	start := time.Now().UTC().Truncate(time.Second)
	runtime := testRuntime()
	g := Generator{OS: runtime.OS, Arch: runtime.Arch, LogicalCPUs: runtime.LogicalCPUs,
		GOMAXPROCS: runtime.GOMAXPROCS, CPUCapacityCores: runtime.CPUCapacityCores,
		CPUCapacitySource: runtime.CPUCapacitySource, CPUCapacityKnown: runtime.CPUCapacityKnown,
		GoVersion: runtime.GoVersion, RunnerVersion: runtime.RunnerVersion}
	f := Fixture{NetworkMode: "loopback_same_host_separate_process", WorkloadModel: "open_loop_journey_arrivals",
		Method: "GET", HealthyResponseBytes: 4096, HealthyDelay: "20ms", StageDuration: "5s", SoakDuration: "30s",
		StageUsers: 10, MaximumRPS: 100, Repeats: 1, HealthyRates: []int{2, 5, 10}, WorkloadProfiles: ReferenceWorkloadProfiles()}
	stages := []Stage{{ArrivalRate: 2, MaxRPS: 50, Scheduled: 10, Started: 10, Completed: 10, Outcome: "PASS"},
		{ArrivalRate: 5, MaxRPS: 50, Scheduled: 25, Started: 25, Completed: 25, Outcome: "PASS"},
		{ArrivalRate: 10, MaxRPS: 50, Scheduled: 50, Started: 50, Completed: 50, Outcome: "PASS"}}
	rec, err := NewRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED", passingWorkloadObservations())
	if err != nil {
		t.Fatal(err)
	}
	runs := make([]stageRun, len(stages))
	for i, s := range stages {
		runs[i] = stageRun{ArrivalsScheduled: s.Scheduled, IterationsStarted: s.Started, IterationsCompleted: s.Completed, Outcome: s.Outcome}
	}
	a := artifact{SchemaVersion: 1, Status: rec.Status, Preflight: rec, Runs: runs}
	a.SchedulerLag = passedLagFixture()
	return a
}

func testRuntime() Runtime {
	runtime := CurrentRuntime()
	if runtime.LogicalCPUs < 1 {
		runtime.LogicalCPUs = 1
	}
	if runtime.GOMAXPROCS < 1 {
		runtime.GOMAXPROCS = 1
	}
	capacity := min(runtime.LogicalCPUs, runtime.GOMAXPROCS)
	runtime.CPUCapacityCores = float64(capacity)
	runtime.CPUCapacitySource = "test_fixture_runtime_capacity"
	runtime.CPUCapacityKnown = true
	return runtime
}

func passingWorkloadObservations() []WorkloadObservation {
	profiles := ReferenceWorkloadProfiles()
	observations := make([]WorkloadObservation, 0, len(profiles))
	for _, profile := range profiles {
		duration, _ := time.ParseDuration(profile.Duration)
		planned := int64(profile.ArrivalRate) * int64(duration/time.Second)
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		tcpConnections, _ := ExpectedReferenceTCPConnections(profile, planned)
		observations = append(observations, WorkloadObservation{
			ProfileID: profile.ID, Scheduled: planned, Admitted: planned, Started: planned,
			Completed: planned, Outcome: "PASS", ClientCompleteBodyObservations: planned,
			ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned,
			ServerTCPConnections:   tcpConnections,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
	}
	return observations
}

func TestSchemaSixRejectsAlteredOrMissingWorkloadEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{"missing profile", func(record *Record) {
			record.Fixture.WorkloadProfiles = record.Fixture.WorkloadProfiles[:len(record.Fixture.WorkloadProfiles)-1]
		}},
		{"missing observation", func(record *Record) {
			record.WorkloadObservations = record.WorkloadObservations[:len(record.WorkloadObservations)-1]
		}},
		{"request bytes altered", func(record *Record) { record.WorkloadObservations[4].ServerRequestBodyBytes-- }},
		{"response bytes altered", func(record *Record) { record.WorkloadObservations[1].ClientResponseTotalBytes++ }},
		{"rate pair altered", func(record *Record) { record.Fixture.WorkloadProfiles[0].MaxRPS++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := passingArtifact(t)
			test.mutate(&artifact.Preflight)
			if err := ValidateRecord(artifact.Preflight); err == nil {
				t.Fatal("altered schema-v6 workload evidence was accepted")
			}
		})
	}
}

func TestReferenceMatrixCoversOnlyExactShapeAndRatePairs(t *testing.T) {
	profiles := ReferenceWorkloadProfiles()
	if len(profiles) != 30 {
		t.Fatalf("got %d workload profiles, want 30 exact shape/connection/rate pairs", len(profiles))
	}
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		key := fmt.Sprintf("%s|%s|%d", profile.Path, profile.ConnectionPolicy, profile.ArrivalRate)
		if seen[key] {
			t.Fatalf("duplicate exact workload pair %s", key)
		}
		seen[key] = true
		if profile.ArrivalRate != 2 && profile.ArrivalRate != 5 && profile.ArrivalRate != 10 {
			t.Fatalf("unexpected interpolated or untested rate: %+v", profile)
		}
	}
	for _, shape := range referenceWorkloadShapes() {
		for _, connectionPolicy := range []string{"reuse", "new_per_request"} {
			for _, rate := range WorkloadMatrixArrivalRates() {
				key := fmt.Sprintf("%s|%s|%d", shape.Path, connectionPolicy, rate)
				if !seen[key] {
					t.Fatalf("missing exact pair path=%s connection_policy=%s rate=%d", shape.Path, connectionPolicy, rate)
				}
			}
		}
	}
}

func TestExpectedReferenceTCPConnectionsRejectsUnsupportedBounds(t *testing.T) {
	tests := []struct {
		name     string
		profile  WorkloadProfile
		planned  int64
		want     int64
		wantOkay bool
	}{
		{name: "reuse exact fixture", profile: WorkloadProfile{ConnectionPolicy: "reuse"}, planned: 10, want: 1, wantOkay: true},
		{name: "new connection for every arrival", profile: WorkloadProfile{ConnectionPolicy: "new_per_request"}, planned: 10, want: 10, wantOkay: true},
		{name: "unknown policy", profile: WorkloadProfile{ConnectionPolicy: "unknown"}, planned: 10},
		{name: "no planned arrivals", profile: WorkloadProfile{ConnectionPolicy: "reuse"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, okay := ExpectedReferenceTCPConnections(test.profile, test.planned)
			if got != test.want || okay != test.wantOkay {
				t.Fatalf("ExpectedReferenceTCPConnections() = (%d, %t), want (%d, %t)", got, okay, test.want, test.wantOkay)
			}
		})
	}
}

func TestSchemaFourArtifactsRemainReadableWithOriginalMatrix(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.SchemaVersion = 4
	a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV4()
	a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV4())
	fingerprint, err := Fingerprint(a.Preflight.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	a.Preflight.ScenarioFingerprintSHA256 = fingerprint
	if err := ValidateRecord(a.Preflight); err != nil {
		t.Fatalf("schema-v4 artifact no longer validates: %v", err)
	}
}

func TestSchemaFiveArtifactsRemainReadableWithoutTCPConnectionEvidence(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.SchemaVersion = 5
	a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV5()
	a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV5())
	for index := range a.Preflight.WorkloadObservations {
		a.Preflight.WorkloadObservations[index].ServerTCPConnections = 0
	}
	fingerprint, err := Fingerprint(a.Preflight.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	a.Preflight.ScenarioFingerprintSHA256 = fingerprint
	if err := ValidateRecord(a.Preflight); err != nil {
		t.Fatalf("schema-v5 artifact without new connection evidence no longer validates: %v", err)
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateArtifactBytes(data, true); err != nil {
		t.Fatalf("schema-v5 calibration JSON without TCP connection fields no longer loads: %v", err)
	}
}

func TestSchemaSixRequiresExactTCPConnectionEvidence(t *testing.T) {
	tests := []struct {
		name   string
		index  int
		mutate func(*WorkloadObservation)
	}{
		{name: "missing reusable connection count", index: 0, mutate: func(observation *WorkloadObservation) { observation.ServerTCPConnections = 0 }},
		{name: "too many reusable connections", index: 0, mutate: func(observation *WorkloadObservation) { observation.ServerTCPConnections = 2 }},
		{name: "missing new-per-request connection", index: 15, mutate: func(observation *WorkloadObservation) { observation.ServerTCPConnections-- }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := passingArtifact(t)
			test.mutate(&a.Preflight.WorkloadObservations[test.index])
			if err := ValidateRecord(a.Preflight); err == nil {
				t.Fatal("schema-v6 calibration accepted incorrect per-profile TCP connection evidence")
			}
		})
	}
}

func TestSchemaSevenRejectsPassingCalibrationWithoutVerifiedCPUCapacity(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.Generator.CPUCapacityKnown = false
	if err := ValidateRecord(a.Preflight); err == nil {
		t.Fatal("passing schema-v7 calibration accepted unknown effective CPU capacity")
	}
}

func TestSchemaSixRejectsAlteredConnectionPolicyTuple(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.Fixture.WorkloadProfiles[15].Duration = "5s"
	fingerprint, err := Fingerprint(a.Preflight.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	a.Preflight.ScenarioFingerprintSHA256 = fingerprint
	if err := ValidateRecord(a.Preflight); err == nil {
		t.Fatal("schema-v6 matrix accepted an altered new-per-request duration")
	}
}

func TestSchemaThreeArtifactsRemainReadableWithOriginalMatrix(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.SchemaVersion = 3
	a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV3()
	a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV3())
	fingerprint, err := Fingerprint(a.Preflight.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	a.Preflight.ScenarioFingerprintSHA256 = fingerprint
	if err := ValidateRecord(a.Preflight); err != nil {
		t.Fatalf("schema-v3 artifact no longer validates: %v", err)
	}
}

func passingObservationsForProfiles(profiles []WorkloadProfile) []WorkloadObservation {
	observations := make([]WorkloadObservation, 0, len(profiles))
	for _, profile := range profiles {
		duration, _ := time.ParseDuration(profile.Duration)
		planned := int64(profile.ArrivalRate) * int64(duration/time.Second)
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		tcpConnections, _ := ExpectedReferenceTCPConnections(profile, planned)
		observations = append(observations, WorkloadObservation{
			ProfileID: profile.ID, Scheduled: planned, Admitted: planned, Started: planned,
			Completed: planned, Outcome: "PASS", ClientCompleteBodyObservations: planned,
			ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned,
			ServerTCPConnections:   tcpConnections,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
	}
	return observations
}

func TestLoadRecordRequiresValidArtifactAndReconciledStageLedgers(t *testing.T) {
	a := passingArtifact(t)
	path := filepath.Join(t.TempDir(), "calibration.json")
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScenarioFingerprintSHA256 != a.Preflight.ScenarioFingerprintSHA256 {
		t.Fatal("loaded record changed")
	}
	a.Runs[0].IterationsStarted--
	data, _ = json.Marshal(a)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecord(path); err == nil {
		t.Fatal("ledger mismatch was accepted")
	}
}

func TestLoadRecordRejectsTrailingAndOversizedInput(t *testing.T) {
	a := passingArtifact(t)
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "calibration.json")
	if err := os.WriteFile(path, append(data, []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecord(path); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	if err := os.WriteFile(path, make([]byte, maxArtifactBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecord(path); err == nil {
		t.Fatal("oversized artifact was accepted")
	}
}

func TestArtifactValidationAllowsInspectionButNotAttachmentOfFailedCalibration(t *testing.T) {
	a := passingArtifact(t)
	a.Status = "CHECKS_FAILED"
	a.Preflight.Status = "CHECKS_FAILED"
	a.Preflight.HighestPassingTestedArrivalRate = 0
	a.Checks = []string{"synthetic failure"}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateArtifactBytes(b, false); err != nil {
		t.Fatalf("offline inspection should accept structurally valid failed evidence: %v", err)
	}
	if _, err := ValidateArtifactBytes(b, true); err == nil {
		t.Fatal("failed calibration must not attach to a run")
	}
}
