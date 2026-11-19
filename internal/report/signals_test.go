package report

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/scheduler"
)

func TestSignalsDistinguishAnIsolatedSchedulerPause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first := true
	stats := scheduler.Drive(ctx, 500*time.Millisecond, 20, func() bool {
		if first {
			first = false
			time.Sleep(130 * time.Millisecond)
		}
		return true
	})
	if !stats.Consistent() || stats.Planned != 10 || stats.DroppedLate == 0 ||
		stats.DroppedBusy != 0 || stats.CancelledBeforeAdmission != 0 {
		t.Fatalf("expected only reconciled late drops after the injected pause: %+v", stats)
	}
	signals := DetectSignals(Result{
		ArrivalsDroppedLate: stats.DroppedLate,
		Generator:           &generatorhealth.Summary{Samples: 2, GoCPUValid: true, ProcessCPUValid: true},
	})
	if len(signals) != 1 || signals[0].Code != "CLIENT_SCHEDULER_LATE" || signals[0].Origin != "client" {
		t.Fatalf("expected only the client scheduler finding: %+v", signals)
	}
}

func TestSignalsClassifyClientAndHTTPObservationsWithoutCausality(t *testing.T) {
	r := validSyntheticQualityRun()
	r.ArrivalsDroppedBusy = 2
	r.ArrivalsDroppedLate = 1
	cancelledBeforeAdmission := int64(3)
	r.ArrivalsCancelledBeforeAdmission = &cancelledBeforeAdmission
	r.Config.MaxRPS = 10
	r.Generator = &generatorhealth.Summary{
		Samples: 10, GoCPUValid: true, ProcessCPUValid: true,
		GoCPUUtilizationRatio: 0.95, PeakProcessCPUUtilizationRatio: 0.92,
	}
	r.Metrics.Reasons = map[string]int{
		"http_503":                      1,
		"Bearer SECRET-SHOULD-NOT-LEAK": 10,
	}
	signals := DetectSignals(r)
	want := map[string]bool{
		"GENERATOR_CPU_PRESSURE":    false,
		"CLIENT_RATE_CEILING_NEAR":  false,
		"CLIENT_WORKERS_BUSY":       false,
		"CLIENT_SCHEDULER_LATE":     false,
		"CLIENT_ARRIVALS_CANCELLED": false,
		"HTTP_ERROR_RESPONSE":       false,
	}
	for _, s := range signals {
		if _, exists := want[s.Code]; exists {
			want[s.Code] = true
		}
		if s.Origin == "server_cause" {
			t.Fatal("an HTTP status cannot prove server bottleneck")
		}
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("missing signal %s in %+v", k, signals)
		}
	}
	data, err := json.Marshal(signals)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SECRET-SHOULD-NOT-LEAK") {
		t.Fatal("untrusted reason leaked into diagnostics")
	}
}

func TestSignalsExposeSchedulerAdmissionMismatch(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 17
	admitted := int64(99)
	cancelled := int64(0)
	r.ArrivalsAdmitted = &admitted
	r.ArrivalsCancelledBeforeAdmission = &cancelled
	signals := DetectSignals(r)
	for _, signal := range signals {
		if signal.Code == "CLIENT_ADMISSION_MISMATCH" && signal.Origin == "arrival_ledger" {
			return
		}
	}
	t.Fatalf("scheduler admission/worker start mismatch was not exposed: %+v", signals)
}

// Closed-loop iterations are started directly by their assigned actors, not
// admitted through the independent arrival scheduler. They intentionally
// persist zero arrivals_admitted, even when iterations_started is nonzero.
func TestClosedLoopDoesNotEmitArrivalAdmissionMismatch(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 17
	r.Config.Mode = "closed"
	r.IterationsStarted = 51
	zero := int64(0)
	r.ArrivalsAdmitted = &zero
	r.ArrivalsCancelledBeforeAdmission = &zero
	for _, signal := range DetectSignals(r) {
		if signal.Code == "CLIENT_ADMISSION_MISMATCH" {
			t.Fatalf("false arrival-ledger alarm for closed actors: %+v", signal)
		}
	}
	// The same counter mismatch is meaningful for independent arrivals.
	r.Config.Mode = "arrival"
	found := false
	for _, signal := range DetectSignals(r) {
		if signal.Code == "CLIENT_ADMISSION_MISMATCH" {
			found = true
		}
	}
	if !found {
		t.Fatal("actual arrival-scheduler mismatch was not reported")
	}
}

func TestSignalsDoNotInferUnknownInfrastructure(t *testing.T) {
	r := validSyntheticQualityRun()
	r.Config.MaxRPS = 100
	r.Generator = &generatorhealth.Summary{Samples: 10, ProcessCPUValid: true, GoCPUValid: true}
	sigs := DetectSignals(r)
	if len(sigs) != 1 || sigs[0].Code != "BOTTLENECK_NOT_ESTABLISHED" {
		t.Fatalf("unknown server bottleneck should stay unknown: %+v", sigs)
	}
	r.Generator = nil
	sigs = DetectSignals(r)
	if len(sigs) != 1 || sigs[0].Code != "GENERATOR_TELEMETRY_UNAVAILABLE" {
		t.Fatalf("missing process metrics should be explicit: %+v", sigs)
	}
}

func TestConnectionAcquisitionWaitIsAnObservationNotAProvenCause(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 28
	r.Config.MaxConnectionsPerHost = 1
	r.Steps = []StepResult{{HTTPTrace: HTTPTrace{ConnectionAcquireCount: 2, ConnectionAcquireMS: 35, ConnectionReusedCount: 1, ConnectionWaitCount: 1, ConnectionWaitMS: 35}}}
	for _, signal := range DetectSignals(r) {
		if signal.Code == "CLIENT_CONNECTION_ACQUISITION_WAIT" {
			if signal.Origin != "client_transport" || !strings.Contains(signal.Evidence, "not proven") {
				t.Fatalf("transport wait was mislabeled as a proven cause: %+v", signal)
			}
			return
		}
	}
	t.Fatal("observed reused-connection wait did not produce a client diagnostic")
}
