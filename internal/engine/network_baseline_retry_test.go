package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// droppingServer closes the connection without a response for every request
// number for which drop returns true, producing a transport failure.
func droppingServer(t *testing.T, drop func(n int64) bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if drop(hits.Add(1)) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func retryPlan(url string) *config.NetworkBaselinePlan {
	return &config.NetworkBaselinePlan{Method: http.MethodGet, URL: url + "/", Path: "/", Source: config.NetworkBaselineSourceStep,
		Requests: 20, Interval: time.Millisecond, Timeout: 500 * time.Millisecond, WallBudget: 5 * time.Second}
}

func shortSettle(t *testing.T) {
	t.Helper()
	old := networkBaselineSettle
	networkBaselineSettle = time.Millisecond
	t.Cleanup(func() { networkBaselineSettle = old })
}

func TestPreLoadBaselineRetriesOnceAfterTransientFailure(t *testing.T) {
	shortSettle(t)
	server, hits := droppingServer(t, func(n int64) bool { return n == 1 })
	b := runPreLoadNetworkBaseline(context.Background(), retryPlan(server.URL), nil)
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Config: report.RunConfig{P95LimitMS: 1000}, NetworkBaseline: b}
	if b.Completed != 20 || b.Failures != 0 || b.RetriedAfterFailures != 1 || hits.Load() != 40 {
		t.Fatalf("transient failure was not retried once: %+v hits=%d", b, hits.Load())
	}
	if !report.ValidNetworkBaseline(r) || report.NetworkBaselineIssue(r) != "" {
		t.Fatalf("clean retry still invalidated the run: %+v", b)
	}
}

func TestPreLoadBaselineKeepsProtectionForPersistentlyBadPath(t *testing.T) {
	shortSettle(t)
	// Every fourth request times out, in both attempts.
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1)%4 == 0 {
			time.Sleep(150 * time.Millisecond)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	plan := retryPlan(server.URL)
	plan.Timeout = 50 * time.Millisecond
	b := runPreLoadNetworkBaseline(context.Background(), plan, nil)
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Config: report.RunConfig{P95LimitMS: 1000}, NetworkBaseline: b}
	if b.Failures != 5 || b.RetriedAfterFailures != 5 || hits.Load() != 40 {
		t.Fatalf("persistent failures were not retried exactly once: %+v hits=%d", b, hits.Load())
	}
	if !report.ValidNetworkBaseline(r) || report.NetworkBaselineIssue(r) == "" {
		t.Fatalf("persistently bad path lost its protection: %+v", b)
	}
}

func TestPreLoadBaselineCleanAttemptIsNotRetried(t *testing.T) {
	shortSettle(t)
	server, hits := droppingServer(t, func(int64) bool { return false })
	b := runPreLoadNetworkBaseline(context.Background(), retryPlan(server.URL), nil)
	if b.Completed != 20 || b.RetriedAfterFailures != 0 || hits.Load() != 20 {
		t.Fatalf("clean baseline was retried: %+v hits=%d", b, hits.Load())
	}
}
