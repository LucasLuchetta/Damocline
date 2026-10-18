package engine

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// TestArrivalCancellationReconcilesIndependentLedgers checks that an
// interrupted arrival run accounts separately for scheduled starts, admitted
// journeys, client-cancelled requests, and server-observed cancellation.
func TestArrivalCancellationReconcilesIndependentLedgers(t *testing.T) {
	fixture, err := reference.New(reference.Config{
		Delay: 2 * time.Second, MaxConcurrent: 2, MaxRequests: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()

	scenario := config.Scenario{
		Name: "bounded cancellation reference", BaseURL: server.URL,
		Users: 2, Duration: "3s", Mode: "arrival", ArrivalRate: 2,
		MaxRPS: 10, Timeout: "3s", P95LimitMS: 2900,
		MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "reference", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan report.Result, 1)
	go func() { done <- Run(ctx, validated) }()

	deadline := time.Now().Add(2 * time.Second)
	for fixture.Snapshot().Received == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fixture.Snapshot().Received != 1 {
		cancel()
		<-done
		t.Fatalf("reference did not receive the first planned request: %+v", fixture.Snapshot())
	}
	cancel()
	result := <-done
	for fixture.Snapshot().Cancelled == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ledger := fixture.Snapshot()

	if result.ArrivalsScheduled != result.IterationsStarted+result.ArrivalsDropped ||
		result.ArrivalsCancelledBeforeAdmission == nil ||
		result.ArrivalsDropped != result.ArrivalsDroppedBusy+result.ArrivalsDroppedLate+*result.ArrivalsCancelledBeforeAdmission {
		t.Fatalf("planned arrivals did not reconcile after cancellation: %+v", result)
	}
	if result.SchedulerLagSamples != result.ArrivalsScheduled || result.SchedulerLagMethod == "" ||
		result.SchedulerLagP50MS > result.SchedulerLagP95MS || result.SchedulerLagP95MS > result.SchedulerLagMaxMS {
		t.Fatalf("scheduler timing evidence is incomplete or inconsistent: %+v", result)
	}
	if result.IterationsStarted != result.IterationsCompleted+int64(result.Journeys.Total+result.Journeys.Cancelled) ||
		result.Journeys.Cancelled != 1 || result.Metrics.Cancelled != 1 || result.Outcome != "ABORTED" {
		t.Fatalf("client work did not preserve incomplete journey/request accounting: %+v", result)
	}
	if ledger.Received != int64(result.Metrics.Total+result.Metrics.Cancelled) ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected ||
		ledger.Completed != ledger.Success+ledger.Failures || ledger.Cancelled != 1 || ledger.Rejected != 0 {
		t.Fatalf("client and independent reference ledgers disagree: client=%+v server=%+v", result, ledger)
	}
}
