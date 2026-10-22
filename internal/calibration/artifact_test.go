package calibration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	a := artifact{SchemaVersion: 1, Status: rec.Status, Preflight: rec, Checks: []string{}, Runs: runs}
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
		closedLoop := profile.Mode == "closed"
		if closedLoop {
			planned = int64(profile.Users)
		}
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		measurementCount := planned
		if closedLoop {
			measurementCount = 1
		}
		tcpConnections, _ := ExpectedReferenceTCPConnections(profile, measurementCount)
		tlsHandshakes, _ := ExpectedReferenceTLSHandshakes(profile, planned)
		observations = append(observations, WorkloadObservation{
			ProfileID: profile.ID, Mode: profile.Mode, ArrivalAccountingApplicable: profile.Mode == "arrival", Started: planned,
			Completed: planned, Outcome: "PASS", ClientCompleteBodyObservations: planned,
			ClientTLSHandshakes:    tlsHandshakes,
			ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned,
			ServerTCPConnections: tcpConnections, ServerAcceptedTLSConnections: tlsHandshakes,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
		if closedLoop {
			last := &observations[len(observations)-1]
			last.Scheduled = 0
			last.Admitted = 0
			last.Dropped = 0
			last.ArrivalAccountingApplicable = false
			last.ServerTCPConnections = 1
		} else {
			last := &observations[len(observations)-1]
			last.Scheduled = planned
			last.Admitted = planned
		}
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
			artifact.Preflight.SchemaVersion = 6
			artifact.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV5()
			artifact.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV5())
			refreshArtifactFingerprint(t, &artifact.Preflight)
			test.mutate(&artifact.Preflight)
			if err := ValidateRecord(artifact.Preflight); err == nil {
				t.Fatal("altered schema-v6 workload evidence was accepted")
			}
		})
	}
}

func TestReferenceMatrixCoversOnlyExactShapeAndRatePairs(t *testing.T) {
	profiles := ReferenceWorkloadProfiles()
	if len(profiles) != 41 {
		t.Fatalf("got %d workload profiles, want 30 HTTP/5 HTTPS arrival pairs plus 6 closed-loop VU levels", len(profiles))
	}
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if profile.Mode == "closed" {
			continue
		}
		key := fmt.Sprintf("%s|%s|%d", profile.Path, profile.ConnectionPolicy, profile.ArrivalRate)
		if seen[key] {
			t.Fatalf("duplicate exact workload pair %s", key)
		}
		seen[key] = true
		validRate := profile.ArrivalRate == 2 || profile.ArrivalRate == 5 || profile.ArrivalRate == 10
		if profile.TransportScheme == "https" {
			validRate = validRate || profile.ArrivalRate == 20 || profile.ArrivalRate == 40
		}
		if !validRate {
			t.Fatalf("unexpected interpolated or untested rate: %+v", profile)
		}
	}
	for _, shape := range referenceWorkloadShapesV7() {
		for _, connectionPolicy := range []string{"reuse", "new_per_request"} {
			for _, rate := range WorkloadMatrixArrivalRates() {
				key := fmt.Sprintf("%s|%s|%d", shape.Path, connectionPolicy, rate)
				if !seen[key] {
					t.Fatalf("missing exact pair path=%s connection_policy=%s rate=%d", shape.Path, connectionPolicy, rate)
				}
			}
		}
	}
	for _, rate := range []int{2, 5, 10, 20, 40} {
		if !seen[fmt.Sprintf("/matrix/https-4k|reuse|%d", rate)] {
			t.Fatalf("missing exact trusted HTTPS profile at %d arrivals/s", rate)
		}
	}
	closedLevels := map[int]bool{}
	for _, profile := range profiles {
		if profile.Mode == "closed" {
			closedLevels[profile.Users] = true
			if profile.ArrivalRate != 0 || profile.MaxRPS != 100 || profile.Duration != "2s" {
				t.Fatalf("invalid closed-loop calibration controls: %+v", profile)
			}
		}
	}
	if len(closedLevels) != len(ClosedLoopVirtualUserLevels()) {
		t.Fatalf("closed-loop VU levels=%v", closedLevels)
	}
}

func TestClosedLoopObservationRequiresModeAndNoArrivalCounters(t *testing.T) {
	record := passingArtifact(t).Preflight
	index := len(record.Fixture.WorkloadProfiles) - 1
	if record.WorkloadObservations[index].Mode != "closed" || ValidateRecord(record) != nil {
		t.Fatal("valid closed-loop profile evidence did not validate")
	}
	missingMode := record
	missingMode.WorkloadObservations = append([]WorkloadObservation(nil), record.WorkloadObservations...)
	missingMode.WorkloadObservations[index].Mode = ""
	if ValidateRecord(missingMode) == nil {
		t.Fatal("closed-loop observation with missing mode was accepted")
	}
	fakeArrival := record
	fakeArrival.WorkloadObservations = append([]WorkloadObservation(nil), record.WorkloadObservations...)
	fakeArrival.WorkloadObservations[index].Scheduled = 1
	if ValidateRecord(fakeArrival) == nil {
		t.Fatal("arrival counters were accepted as closed-loop evidence")
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

func TestCalibrationArtifactRejectsDuplicateJSONKeys(t *testing.T) {
	data, err := json.Marshal(passingArtifact(t))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
	if bytes.Equal(data, duplicate) {
		t.Fatal("test fixture has no schema_version field")
	}
	if _, err := ValidateArtifactBytes(duplicate, true); err == nil || !strings.Contains(err.Error(), "ambiguous JSON") {
		t.Fatalf("duplicate calibration schema key was not rejected clearly: %v", err)
	}
}

func TestCalibrationArtifactRejectsOmittedRequiredEvidence(t *testing.T) {
	data, err := json.Marshal(passingArtifact(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"scheduler_lag_calibration":`, `"generator":`, `"healthy_stages":`, `"workload_observations":`} {
		mutated := bytes.Replace(data, []byte(field), []byte(`"omitted_fixture_field":`), 1)
		if bytes.Equal(mutated, data) {
			t.Fatalf("fixture lacks required field %s", field)
		}
		if _, err := ValidateArtifactBytes(mutated, true); err == nil {
			t.Fatalf("accepted calibration artifact without %s", field)
		}
	}
}

func TestCalibrationArtifactAllowsNullChecksWhenNoDiscrepanciesExist(t *testing.T) {
	a := passingArtifact(t)
	a.Checks = nil
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateArtifactBytes(data, true); err != nil {
		t.Fatalf("valid passing calibration with an empty nil checks list was rejected: %v", err)
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
			a.Preflight.SchemaVersion = 6
			a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV5()
			a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV5())
			refreshArtifactFingerprint(t, &a.Preflight)
			test.mutate(&a.Preflight.WorkloadObservations[test.index])
			if err := ValidateRecord(a.Preflight); err == nil {
				t.Fatal("schema-v6 calibration accepted incorrect per-profile TCP connection evidence")
			}
		})
	}
}

func TestSchemaSevenRejectsPassingCalibrationWithoutVerifiedCPUCapacity(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.SchemaVersion = 7
	a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV5()
	a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV5())
	refreshArtifactFingerprint(t, &a.Preflight)
	a.Preflight.Generator.CPUCapacityKnown = false
	if err := ValidateRecord(a.Preflight); err == nil {
		t.Fatal("passing schema-v7 calibration accepted unknown effective CPU capacity")
	}
}

func TestSchemaEightRequiresExactTrustedTLSHandshakeEvidence(t *testing.T) {
	tests := []struct {
		name  string
		value int64
	}{
		{name: "missing handshake", value: 0},
		{name: "extra handshake", value: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := passingArtifact(t)
			a.Preflight.WorkloadObservations[30].ClientTLSHandshakes = test.value
			if err := ValidateRecord(a.Preflight); err == nil {
				t.Fatal("schema-v8 calibration accepted an inconsistent TLS handshake count")
			}
		})
	}
}

func TestSchemaNineArtifactsRetainOriginalWorkloadMatrix(t *testing.T) {
	a := passingArtifact(t)
	a.Preflight.SchemaVersion = 9
	a.Preflight.Fixture.WorkloadProfiles = referenceWorkloadProfilesV9()
	a.Preflight.WorkloadObservations = passingObservationsForProfiles(referenceWorkloadProfilesV9())
	refreshArtifactFingerprint(t, &a.Preflight)
	if len(a.Preflight.Fixture.WorkloadProfiles) != 39 {
		t.Fatalf("historical schema-v9 profile count = %d, want 39", len(a.Preflight.Fixture.WorkloadProfiles))
	}
	if err := ValidateRecord(a.Preflight); err != nil {
		t.Fatalf("schema-v9 artifact no longer validates against its original matrix: %v", err)
	}
}

func TestSchemaTenRequiresExactHigherRateTLSHandshakes(t *testing.T) {
	for _, index := range []int{33, 34} {
		t.Run(ReferenceWorkloadProfiles()[index].ID, func(t *testing.T) {
			a := passingArtifact(t)
			a.Preflight.WorkloadObservations[index].ClientTLSHandshakes = 0
			if err := ValidateRecord(a.Preflight); err == nil {
				t.Fatal("schema-v10 calibration accepted missing TLS handshake evidence at higher load")
			}
		})
	}
}

func TestSchemaElevenUsesAcceptedTLSSocketsForHandshakeReconciliation(t *testing.T) {
	a := passingArtifact(t)
	index := -1
	for i, profile := range a.Preflight.Fixture.WorkloadProfiles {
		if profile.ID == "https-get-4k-response-shape-r40" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("higher-rate TLS profile is missing")
	}
	observation := &a.Preflight.WorkloadObservations[index]
	// A TLS connection can complete its handshake and be accepted without
	// carrying a request when concurrent dials race to establish reuse.
	observation.ClientTLSHandshakes = 2
	observation.ServerAcceptedTLSConnections = 2
	observation.ServerTCPConnections = 1
	if err := ValidateRecord(a.Preflight); err != nil {
		t.Fatalf("schema-v11 artifact rejected a reconciled idle TLS connection: %v", err)
	}
	observation.ServerAcceptedTLSConnections = 1
	if err := ValidateRecord(a.Preflight); err == nil {
		t.Fatal("schema-v11 artifact accepted client handshake count that differs from accepted TLS sockets")
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
	hasClosedLoop := false
	for _, profile := range profiles {
		hasClosedLoop = hasClosedLoop || profile.Mode == "closed"
	}
	for _, profile := range profiles {
		duration, _ := time.ParseDuration(profile.Duration)
		planned := int64(profile.ArrivalRate) * int64(duration/time.Second)
		if profile.Mode == "closed" {
			planned = int64(profile.Users)
		}
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		connectionObservations := planned
		if profile.Mode == "closed" {
			connectionObservations = 1
		}
		tcpConnections, _ := ExpectedReferenceTCPConnections(profile, connectionObservations)
		tlsHandshakes, _ := ExpectedReferenceTLSHandshakes(profile, planned)
		mode, arrivalAccounting := "", false
		if hasClosedLoop {
			mode, arrivalAccounting = profile.Mode, profile.Mode == "arrival"
		}
		observations = append(observations, WorkloadObservation{
			ProfileID: profile.ID, Mode: mode, ArrivalAccountingApplicable: arrivalAccounting, Scheduled: planned, Admitted: planned, Started: planned,
			Completed: planned, Outcome: "PASS", ClientCompleteBodyObservations: planned,
			ClientTLSHandshakes:    tlsHandshakes,
			ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned,
			ServerTCPConnections: tcpConnections, ServerAcceptedTLSConnections: tlsHandshakes,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
		if profile.Mode == "closed" {
			last := &observations[len(observations)-1]
			last.Scheduled, last.Admitted, last.Dropped, last.ArrivalAccountingApplicable = 0, 0, 0, false
			last.ServerTCPConnections = 1
		}
	}
	return observations
}

func refreshArtifactFingerprint(t *testing.T, record *Record) {
	t.Helper()
	fingerprint, err := Fingerprint(record.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	record.ScenarioFingerprintSHA256 = fingerprint
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
