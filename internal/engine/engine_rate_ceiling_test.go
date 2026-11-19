package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestRateCeilingFindingReconcilesLoopbackRequests(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	v, err := config.Parse(config.Scenario{
		Name: "bounded rate ceiling", BaseURL: server.URL, Users: 1,
		Duration: "3100ms", MaxRPS: 4, Timeout: "500ms", P95LimitMS: 500,
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{http.StatusNoContent}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	run := Run(ctx, v)
	// The final iteration may already be waiting for a token when observation
	// ends. It can use one more token during drain.
	got := received.Load()
	if got < 12 || got > 13 || int64(run.Metrics.Success) != got || run.Metrics.Failures != 0 {
		t.Fatalf("rate-limited request ledger: server=%d client=%+v", got, run.Metrics)
	}
	if len(run.Steps) != 1 || int64(run.Steps[0].HTTPTrace.RateWaitCount) != got ||
		run.Steps[0].HTTPTrace.RateWaitMS < 2000 {
		t.Fatalf("expected sustained token waits: %+v", run.Steps)
	}
	found := 0
	for _, signal := range run.Signals {
		switch signal.Code {
		case "CLIENT_RATE_CEILING_NEAR":
			found++
			if signal.Origin != "client" {
				t.Errorf("ceiling was attributed outside the generator: %+v", signal)
			}
		case "CLIENT_WORKERS_BUSY", "CLIENT_SCHEDULER_LATE", "CLIENT_CONNECTION_ACQUISITION_WAIT",
			"CONNECTION_ESTABLISHMENT_FAILURE", "HTTP_ERROR_RESPONSE":
			t.Errorf("isolated rate ceiling produced unrelated finding: %+v", signal)
		}
	}
	if found != 1 {
		t.Fatalf("expected one rate ceiling finding: %+v", run.Signals)
	}
}
