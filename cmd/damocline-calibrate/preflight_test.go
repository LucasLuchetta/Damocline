package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
)

func testPreflightInputs(extended bool, repeats int) (preflightGenerator, preflightFixture, []preflightStage) {
	g := preflightGenerator{
		OS: "linux", Arch: "amd64", LogicalCPUs: 4,
		GOMAXPROCS: 2, CPUCapacityCores: 2, CPUCapacitySource: "runtime_gomaxprocs", CPUCapacityKnown: true,
		GoVersion: "go1.24.2", RunnerVersion: "source-0123456789ab",
	}
	f := preflightFixture{
		NetworkMode:   "loopback_same_host_separate_process",
		WorkloadModel: "open_loop_journey_arrivals",
		Method:        "GET", HealthyResponseBytes: 4096, HealthyDelay: "20ms",
		StageDuration: "5s", SoakDuration: "30s",
		StageUsers: 10, MaximumRPS: 100, Repeats: repeats, Extended: extended,
	}
	sequence := []int{2, 5, 10}
	if extended {
		sequence = append(sequence, 15, 20, 50, 100)
	}
	var stages []preflightStage
	for i := 0; i < repeats; i++ {
		for _, rate := range sequence {
			f.HealthyRates = append(f.HealthyRates, rate)
			n := int64(rate * 5)
			stages = append(stages, preflightStage{
				ArrivalRate: rate, MaxRPS: max(50, rate), Scheduled: n, Started: n,
				Completed: n, Outcome: "PASS",
			})
		}
	}
	return g, f, stages
}

func passingWorkloadObservations() []preflightWorkloadObservation {
	profiles := preflightWorkloadProfilesForTest()
	observations := make([]preflightWorkloadObservation, 0, len(profiles))
	for _, profile := range profiles {
		duration, _ := time.ParseDuration(profile.Duration)
		planned := int64(profile.ArrivalRate) * int64(duration/time.Second)
		requestBytes := int64(profile.RequestBodyBytes) * planned
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		tcpConnections, _ := calibration.ExpectedReferenceTCPConnections(profile, planned)
		observations = append(observations, preflightWorkloadObservation{
			ProfileID: profile.ID, Scheduled: planned, Admitted: planned,
			Started: planned, Completed: planned, Outcome: "PASS",
			ClientCompleteBodyObservations: planned,
			ClientResponseMinBytes:         int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes:         int64(profile.ResponseBodyBytes),
			ClientResponseTotalBytes:       responseBytes,
			ServerReceived:                 planned, ServerCompleted: planned,
			ServerTCPConnections:   tcpConnections,
			ServerRequestBodyBytes: requestBytes, ServerResponseBodyBytes: responseBytes,
		})
	}
	return observations
}

func preflightWorkloadProfilesForTest() []calibration.WorkloadProfile {
	return calibration.ReferenceWorkloadProfiles()
}

func TestPreflightRoundTripVersionAndEvidence(t *testing.T) {
	for _, tc := range []struct {
		extended bool
		repeats  int
		maximum  int
	}{
		{false, 1, 10},
		{true, 2, 100},
		{false, 3, 10},
	} {
		g, f, stages := testPreflightInputs(tc.extended, tc.repeats)
		f.WorkloadProfiles = preflightWorkloadProfilesForTest()
		start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
		rec, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED", passingWorkloadObservations())
		if err != nil {
			t.Fatal(err)
		}
		if rec.HighestPassingTestedArrivalRate != tc.maximum || rec.SchemaVersion != preflightSchemaVersion {
			t.Fatalf("unexpected evidence envelope: %+v", rec)
		}
		if rec.FixtureWorkloadFingerprint == "" {
			t.Fatal("preflight workload shape fingerprint is missing")
		}
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		var decoded preflightRecord
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := validatePreflightRecord(decoded); err != nil {
			t.Fatalf("round-tripped record rejected: %v", err)
		}
		again, err := preflightFingerprint(decoded.Fixture)
		if err != nil || again != rec.ScenarioFingerprintSHA256 {
			t.Fatalf("fingerprint not stable: %v", err)
		}
	}
}

func TestPreflightRejectsUnknownSchemaAndEditedFixture(t *testing.T) {
	g, f, stages := testPreflightInputs(true, 1)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rec, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED")
	if err != nil {
		t.Fatal(err)
	}
	rec.SchemaVersion++
	if err := validatePreflightRecord(rec); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("should reject a future incompatible schema: %v", err)
	}
	rec.SchemaVersion--
	rec.Fixture.HealthyRates[0] = 99
	if err := validatePreflightRecord(rec); err == nil {
		t.Fatal("modified scenario must invalidate its fingerprint/record")
	}
	rec.Fixture.HealthyRates[0] = 2
	rec.ScenarioFingerprintSHA256 = strings.Repeat("0", 64)
	if err := validatePreflightRecord(rec); err == nil {
		t.Fatal("modified fingerprint must be rejected")
	}
}

func TestPreflightFailedRunsNeverClaimPassingEnvelope(t *testing.T) {
	g, f, stages := testPreflightInputs(false, 1)
	stages[1].Dropped = 2
	stages[1].Started -= 2
	stages[1].Completed -= 2
	stages[1].Outcome = "INCONCLUSIVE"
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED"); err == nil {
		t.Fatal("incomplete stages must prevent a passing calibration record")
	}
	rec, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "CHECKS_FAILED")
	if err != nil {
		t.Fatal(err)
	}
	if rec.HighestPassingTestedArrivalRate != 0 {
		t.Fatalf("failed preflight claimed a complete tested envelope: %d", rec.HighestPassingTestedArrivalRate)
	}
}

func TestPreflightSnapshotContainsNoTargetOrSecrets(t *testing.T) {
	t.Setenv("DAMOCLINE_TOKEN", "Bearer private-customer-token-123")
	g, f, stages := testPreflightInputs(false, 1)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rec, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "REFERENCE_SMOKE_PASSED")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{
		[]byte("private-customer-token-123"),
		[]byte("Bearer"),
		[]byte("https://"),
		[]byte("127.0.0.1"),
		[]byte("/work"),
		[]byte("hostname"),
	} {
		if bytes.Contains(data, forbidden) {
			t.Fatalf("preflight leaked sensitive or environment-specific content: %s", forbidden)
		}
	}
	if !bytes.Contains(data, []byte(`"healthy_response_bytes": 4096`)) ||
		!bytes.Contains(data, []byte(`"network_mode": "loopback_same_host_separate_process"`)) {
		t.Fatalf("preflight missing explicit fixture metadata: %s", data)
	}
}

func TestPreflightRejectsUnsafeMetadataAndBackwardTime(t *testing.T) {
	g, f, stages := testPreflightInputs(false, 1)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	g.RunnerVersion = "source-local /secret/path"
	if _, err := newPreflightRecord(start, start.Add(time.Minute), g, f, stages, "CHECKS_FAILED"); err == nil {
		t.Fatal("unsafe runner metadata accepted")
	}
	g.RunnerVersion = "source-development"
	if _, err := newPreflightRecord(start, start.Add(-time.Second), g, f, stages, "CHECKS_FAILED"); err == nil {
		t.Fatal("backwards time window accepted")
	}
}
