package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func writePassingCalibration(t *testing.T, mutate func(*[]int64)) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rt := calibration.CurrentRuntime()
	gen := calibration.Generator{OS: rt.OS, Arch: rt.Arch, LogicalCPUs: rt.LogicalCPUs, GOMAXPROCS: rt.GOMAXPROCS,
		CPUCapacityCores: rt.CPUCapacityCores, CPUCapacitySource: rt.CPUCapacitySource, CPUCapacityKnown: rt.CPUCapacityKnown,
		GoVersion: rt.GoVersion, RunnerVersion: rt.RunnerVersion}
	fixture := calibration.Fixture{NetworkMode: "loopback_same_host_separate_process", WorkloadModel: "open_loop_journey_arrivals", Method: "GET", HealthyResponseBytes: 4096,
		HealthyDelay: "20ms", StageDuration: "5s", SoakDuration: "30s", StageUsers: 10, MaximumRPS: 100, Repeats: 1, HealthyRates: []int{2, 5, 10}, WorkloadProfiles: calibration.ReferenceWorkloadProfiles()}
	stages := []calibration.Stage{{ArrivalRate: 2, MaxRPS: 50, Scheduled: 10, Started: 10, Completed: 10, Outcome: "PASS"}, {ArrivalRate: 5, MaxRPS: 50, Scheduled: 25, Started: 25, Completed: 25, Outcome: "PASS"}, {ArrivalRate: 10, MaxRPS: 50, Scheduled: 50, Started: 50, Completed: 50, Outcome: "PASS"}}
	record, err := calibration.NewRecord(now.Add(-2*time.Minute), now.Add(-time.Minute), gen, fixture, stages, "REFERENCE_SMOKE_PASSED", passingCalibrationWorkloadObservations())
	if err != nil {
		t.Fatal(err)
	}
	type run struct {
		ArrivalsScheduled   int64  `json:"arrivals_scheduled"`
		IterationsStarted   int64  `json:"iterations_started"`
		IterationsCompleted int64  `json:"iterations_completed"`
		ArrivalsDropped     int64  `json:"arrivals_dropped"`
		Outcome             string `json:"outcome"`
	}
	type artifact struct {
		SchemaVersion int                      `json:"schema_version"`
		Status        string                   `json:"status"`
		Preflight     calibration.Record       `json:"preflight"`
		Checks        []string                 `json:"checks"`
		SchedulerLag  calibration.SchedulerLag `json:"scheduler_lag_calibration"`
		Runs          []run                    `json:"runs"`
	}
	a := artifact{SchemaVersion: 1, Status: record.Status, Preflight: record, Runs: []run{{10, 10, 10, 0, "PASS"}, {25, 25, 25, 0, "PASS"}, {50, 50, 50, 0, "PASS"}}}
	a.SchedulerLag = calibration.SchedulerLag{Method: calibration.SchedulerLagMethod, Rate: calibration.SchedulerLagRate, Duration: calibration.SchedulerLagDuration.String(), ToleranceMS: float64(calibration.SchedulerLagTolerance.Microseconds()) / 1000, Status: "PASSED"}
	for _, pause := range []time.Duration{90, 130, 210} {
		lag := float64((pause*time.Millisecond - time.Second/calibration.SchedulerLagRate).Microseconds()) / 1000
		for range 3 {
			a.SchedulerLag.Observations = append(a.SchedulerLag.Observations, calibration.SchedulerLagObservation{InjectedPauseMS: float64(pause), ExpectedMaxLagMS: lag, ObservedMaxLagMS: lag, Planned: 5, Admitted: 5, Outcome: "PASSED"})
		}
	}
	if mutate != nil {
		counts := []int64{a.Runs[0].ArrivalsScheduled, a.Runs[0].IterationsStarted, a.Runs[0].IterationsCompleted}
		mutate(&counts)
		a.Runs[0].IterationsStarted = counts[1]
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preflight.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func passingCalibrationWorkloadObservations() []calibration.WorkloadObservation {
	profiles := calibration.ReferenceWorkloadProfiles()
	observations := make([]calibration.WorkloadObservation, 0, len(profiles))
	for _, profile := range profiles {
		duration, _ := time.ParseDuration(profile.Duration)
		planned := int64(profile.ArrivalRate) * int64(duration/time.Second)
		responseBytes := int64(profile.ResponseBodyBytes) * planned
		tcpConnections, _ := calibration.ExpectedReferenceTCPConnections(profile, planned)
		observations = append(observations, calibration.WorkloadObservation{
			ProfileID: profile.ID, Scheduled: planned, Admitted: planned, Started: planned, Completed: planned, Outcome: "PASS",
			ClientCompleteBodyObservations: planned, ClientResponseMinBytes: int64(profile.ResponseBodyBytes),
			ClientResponseMaxBytes: int64(profile.ResponseBodyBytes), ClientResponseTotalBytes: responseBytes,
			ServerReceived: planned, ServerCompleted: planned, ServerTCPConnections: tcpConnections,
			ServerRequestBodyBytes: int64(profile.RequestBodyBytes) * planned, ServerResponseBodyBytes: responseBytes,
		})
	}
	return observations
}

func TestAttachCalibrationRecordsUnknownsWithoutPromotingQuality(t *testing.T) {
	path := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 10, Users: 10, MaxRPS: 100, Duration: "5s", Timeout: "1s", Steps: []config.Step{{Name: "read", Method: "GET", Path: "/"}}}
	result := report.Result{SchemaVersion: 12, ElapsedSeconds: 5, ObservationSeconds: 5, Config: report.RunConfig{Users: 10, MaxRPS: 100, Mode: "arrival", ArrivalRate: 10}}
	if err := attachCalibration(&result, scenario, path, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if result.Calibration == nil || result.Calibration.Coverage.Status != "INCONCLUSIVE" {
		t.Fatalf("calibration should attach with inconclusive coverage: %+v", result.Calibration)
	}
	var observedSize *calibration.Dimension
	var observedLatency *calibration.Dimension
	for i := range result.Calibration.Coverage.Dimensions {
		if result.Calibration.Coverage.Dimensions[i].Name == "observed_response_size" {
			observedSize = &result.Calibration.Coverage.Dimensions[i]
		}
		if result.Calibration.Coverage.Dimensions[i].Name == "observed_response_latency" {
			observedLatency = &result.Calibration.Coverage.Dimensions[i]
		}
	}
	if observedSize == nil || observedSize.Status != "UNVERIFIED" {
		t.Fatalf("missing run body sizes must be visible as UNVERIFIED: %+v", result.Calibration.Coverage.Dimensions)
	}
	if observedLatency == nil || observedLatency.Status != "UNVERIFIED" {
		t.Fatalf("missing run latency samples must be visible as UNVERIFIED: %+v", result.Calibration.Coverage.Dimensions)
	}
	if result.Quality == nil || result.Quality.State == report.QualityValid {
		t.Fatalf("fixed fixture does not prove target response or fixture identity: %+v", result.Quality)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"127.0.0.1:8080", "preflight.json"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("report leaked scenario or local file data: %s", secret)
		}
	}
}

func TestAttachCalibrationRejectsBrokenStageLedger(t *testing.T) {
	path := writePassingCalibration(t, func(counts *[]int64) { (*counts)[1]-- })
	result := report.Result{}
	err := attachCalibration(&result, config.Scenario{}, path, time.Now().UTC())
	if err == nil || result.Calibration != nil {
		t.Fatal("inconsistent calibration was attached")
	}
}
