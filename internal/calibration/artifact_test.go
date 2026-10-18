package calibration

import (
	"encoding/json"
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
	runtime := CurrentRuntime()
	g := Generator{OS: runtime.OS, Arch: runtime.Arch, LogicalCPUs: runtime.LogicalCPUs,
		GOMAXPROCS: runtime.GOMAXPROCS, GoVersion: runtime.GoVersion, RunnerVersion: runtime.RunnerVersion}
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

func passingWorkloadObservations() []WorkloadObservation {
	profiles := ReferenceWorkloadProfiles()
	observations := make([]WorkloadObservation, 0, len(profiles))
	for _, profile := range profiles {
		planned := int64(profile.ArrivalRate * 5)
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		observations = append(observations, WorkloadObservation{
			ProfileID: profile.ID, Scheduled: planned, Admitted: planned, Started: planned,
			Completed: planned, Outcome: "PASS", ClientCompleteBodyObservations: planned,
			ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
	}
	return observations
}

func TestSchemaThreeRejectsAlteredOrMissingWorkloadEvidence(t *testing.T) {
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
				t.Fatal("altered schema-v3 workload evidence was accepted")
			}
		})
	}
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
