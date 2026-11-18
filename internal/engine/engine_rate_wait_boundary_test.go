package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestRateTokenWaitExcludesHTTPExecution(t *testing.T) {
	received := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- time.Now()
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "rate wait boundary", BaseURL: server.URL, Users: 1, Mode: "arrival", ArrivalRate: 1,
		Duration: "1s", MaxRPS: 100, Timeout: "1s", P95LimitMS: 1500, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var before time.Time
	run := runWithTransportAndIdentity(ctx, v, nil, func() (string, string) {
		before = time.Now()
		return "git_commit", "0123456789abcdef0123456789abcdef01234567"
	})
	if run.Metrics.Success != 1 || len(run.Steps) != 1 {
		t.Fatalf("single loopback request did not complete: %+v", run.Metrics)
	}
	select {
	case at := <-received:
		assertRateWaitEndsBeforeServerReceipt(t, run.Steps[0].HTTPTrace, before, at)
	default:
		t.Fatal("independent server receipt was not observed")
	}
}

func TestExecutableIdentityPreparationIsOutsideObservedElapsedTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "identity observation boundary", BaseURL: server.URL, Users: 1, Mode: "arrival", ArrivalRate: 1,
		Duration: "1s", MaxRPS: 100, Timeout: "1s", P95LimitMS: 1500, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{http.StatusNoContent}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	var preparation time.Duration
	identity := func() (string, string) {
		before := time.Now()
		time.Sleep(120 * time.Millisecond)
		preparation = time.Since(before)
		return "git_commit", "0123456789abcdef0123456789abcdef01234567"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	before := time.Now()
	run := runWithTransportAndIdentity(ctx, v, nil, identity)
	outside := time.Since(before)
	if run.Metrics.Success != 1 {
		t.Fatalf("single observation request did not complete: %+v", run.Metrics)
	}
	// The report's millisecond rounding can account for at most this allowance.
	// Compare monotonic intervals; do not impose ordering on wall-clock timestamps.
	if outside.Seconds()-run.ElapsedSeconds < preparation.Seconds()-.002 {
		t.Fatalf("identity preparation was included in observed elapsed time: outside=%s preparation=%s report=%.6f s", outside, preparation, run.ElapsedSeconds)
	}
}

func TestRateTokenWaitExcludesCleanupHTTPExecution(t *testing.T) {
	posted := make(chan struct{}, 1)
	cleanupReceived := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posted <- struct{}{}
			<-r.Context().Done()
		case http.MethodDelete:
			cleanupReceived <- time.Now()
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	t.Setenv("DAMOCLINE_TEST_RATE_WAIT_PREFIX", "rate-wait-fixture")
	v, err := config.Parse(config.Scenario{
		Name: "cleanup rate wait boundary", BaseURL: server.URL, Users: 1, Mode: "arrival", ArrivalRate: 1,
		Duration: "1s", MaxRPS: 100, Timeout: "2s", P95LimitMS: 1500, MinSamplesPerStep: 1,
		SyntheticDataPrefixEnv: "DAMOCLINE_TEST_RATE_WAIT_PREFIX",
		Steps: []config.Step{
			{Name: "create", Method: http.MethodPost, Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{http.StatusCreated}},
			{Name: "cleanup", Method: http.MethodDelete, Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{http.StatusNoContent}, Cleanup: true},
		},
	}, config.Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan report.Result, 1)
	go func() { done <- Run(ctx, v) }()
	select {
	case <-posted:
	case <-ctx.Done():
		t.Fatal("independent POST receipt was not observed")
	}
	beforeCancel := time.Now()
	cancel()
	var run report.Result
	select {
	case run = <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("bounded cleanup did not finish")
	}
	if run.Outcome != "ABORTED" {
		t.Fatalf("external cancellation was not preserved: %s", run.Outcome)
	}
	for _, step := range run.Steps {
		if !step.Cleanup {
			continue
		}
		if step.Metrics.Success != 1 {
			t.Fatalf("single cleanup request did not complete: %+v", step.Metrics)
		}
		select {
		case at := <-cleanupReceived:
			assertRateWaitEndsBeforeServerReceipt(t, step.HTTPTrace, beforeCancel, at)
		default:
			t.Fatal("independent cleanup receipt was not observed")
		}
		return
	}
	t.Fatal("cleanup evidence was absent")
}

func assertRateWaitEndsBeforeServerReceipt(t *testing.T, trace report.HTTPTrace, before, received time.Time) {
	t.Helper()
	// Token admission precedes request execution and server receipt. This
	// event-order bound does not depend on a tight host timing tolerance.
	upperMS := float64(received.Sub(before)) / float64(time.Millisecond)
	if trace.RateWaitCount != 1 || trace.CompleteCount != 1 || trace.RateWaitMS < 0 || trace.RateWaitMS > upperMS {
		t.Fatalf("rate-token wait extended beyond server receipt: wait=%.6f ms upper=%.6f ms trace=%+v", trace.RateWaitMS, upperMS, trace)
	}
}
