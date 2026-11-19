package engine

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
)

func TestArrivalWorkerExhaustionReconcilesIndependentLedger(t *testing.T) {
	fixture, err := reference.New(reference.Config{
		Delay: 1500 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()

	scenario := config.Scenario{
		Name: "bounded worker exhaustion reference", BaseURL: server.URL,
		Users: 1, Duration: "1s", Mode: "arrival", ArrivalRate: 4,
		MaxRPS: 10, Timeout: "3s", P95LimitMS: 2900,
		MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "reference", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}

	result := Run(context.Background(), validated)
	busySignals := 0
	for _, signal := range result.Signals {
		switch signal.Code {
		case "CLIENT_WORKERS_BUSY":
			busySignals++
			if signal.Origin != "client" {
				t.Errorf("worker exhaustion was attributed outside the generator: %+v", signal)
			}
		case "CLIENT_SCHEDULER_LATE", "CLIENT_RATE_CEILING_NEAR",
			"CLIENT_CONNECTION_ACQUISITION_WAIT", "CONNECTION_ESTABLISHMENT_FAILURE", "HTTP_ERROR_RESPONSE":
			t.Errorf("isolated worker exhaustion produced unrelated finding: %+v", signal)
		}
	}
	if busySignals != 1 {
		t.Fatalf("expected one worker exhaustion finding, got %+v", result.Signals)
	}
	ledger := fixture.Snapshot()
	if result.ArrivalsScheduled != 4 || result.IterationsStarted != 1 || result.ArrivalsDroppedBusy != 3 || result.ArrivalsDroppedLate != 0 || result.ArrivalsDropped != 3 {
		t.Fatalf("worker exhaustion was not classified as busy drops: %+v", result)
	}
	if result.ArrivalsScheduled != result.IterationsStarted+result.ArrivalsDropped ||
		result.ArrivalsAdmitted == nil || *result.ArrivalsAdmitted != result.IterationsStarted || result.ArrivalsCancelledBeforeAdmission == nil ||
		result.ArrivalsDropped != result.ArrivalsDroppedBusy+result.ArrivalsDroppedLate+*result.ArrivalsCancelledBeforeAdmission {
		t.Fatalf("arrival counters do not reconcile: %+v", result)
	}
	if result.SchedulerLagSamples != result.ArrivalsScheduled || result.SchedulerLagMethod == "" ||
		result.SchedulerLagP50MS > result.SchedulerLagP95MS || result.SchedulerLagP95MS > result.SchedulerLagMaxMS {
		t.Fatalf("scheduler timing evidence is incomplete or inconsistent: %+v", result)
	}
	if result.IterationsCompleted != 1 || result.Metrics.Total != 1 || result.Metrics.Success != 1 || result.Metrics.Cancelled != 0 || result.Outcome != "FAIL" {
		t.Fatalf("only admitted work should reach completion, and underdelivery cannot pass: %+v", result)
	}
	if ledger.Received != 1 || ledger.Completed != 1 || ledger.Success != 1 || ledger.Failures != 0 || ledger.Cancelled != 0 || ledger.Rejected != 0 || ledger.PeakConcurrent != 1 {
		t.Fatalf("independent server ledger does not match admitted traffic: %+v", ledger)
	}
}
