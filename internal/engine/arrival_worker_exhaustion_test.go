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
	ledger := fixture.Snapshot()
	if result.ArrivalsScheduled != 4 || result.IterationsStarted != 1 || result.ArrivalsDroppedBusy != 3 || result.ArrivalsDroppedLate != 0 || result.ArrivalsDropped != 3 {
		t.Fatalf("worker exhaustion was not classified as busy drops: %+v", result)
	}
	if result.ArrivalsScheduled != result.IterationsStarted+result.ArrivalsDropped ||
		result.ArrivalsDropped != result.ArrivalsDroppedBusy+result.ArrivalsDroppedLate {
		t.Fatalf("arrival counters do not reconcile: %+v", result)
	}
	if result.IterationsCompleted != 1 || result.Metrics.Total != 1 || result.Metrics.Success != 1 || result.Metrics.Cancelled != 0 || result.Outcome != "FAIL" {
		t.Fatalf("only admitted work should reach completion, and underdelivery cannot pass: %+v", result)
	}
	if ledger.Received != 1 || ledger.Completed != 1 || ledger.Success != 1 || ledger.Failures != 0 || ledger.Cancelled != 0 || ledger.Rejected != 0 || ledger.PeakConcurrent != 1 {
		t.Fatalf("independent server ledger does not match admitted traffic: %+v", ledger)
	}
}
