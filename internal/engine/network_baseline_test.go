package engine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// baselineServer injects a deterministic per-request delay on /baseline so
// stable and jittery generator paths can be simulated on loopback.
func baselineServer(t *testing.T, delay func(n int64) time.Duration) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var baselineHits, loadHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/baseline" {
			time.Sleep(delay(baselineHits.Add(1)))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		loadHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, &baselineHits, &loadHits
}

func baselineScenario(t *testing.T, base string, p95Limit float64) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{Name: "baseline", BaseURL: base, Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: p95Limit,
		BaselinePath: "/baseline", Steps: []config.Step{{Name: "page", Path: "/page"}}}, config.Options{Authorized: true, NetworkBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	v.NetworkBaseline.Interval = 2 * time.Millisecond
	return v
}

func TestNetworkBaselineStablePathIsRecordedWithoutCaveat(t *testing.T) {
	server, baselineHits, loadHits := baselineServer(t, func(int64) time.Duration { return 2 * time.Millisecond })
	r := Run(context.Background(), baselineScenario(t, server.URL, 1000))
	b := r.NetworkBaseline
	if b == nil || b.Status != report.NetworkBaselineMeasured || b.Planned != config.NetworkBaselineRequests || b.Completed != b.Planned ||
		b.Failures != 0 || b.Path != "/baseline" || b.Source != config.NetworkBaselineSourcePath || b.P50MS < 2 || b.P95MS > b.MaxMS {
		t.Fatalf("unexpected baseline: %+v", b)
	}
	if int(baselineHits.Load()) != b.Planned || int(loadHits.Load()) != r.Metrics.Total {
		t.Fatalf("baseline leaked into load counters: baseline hits=%d load hits=%d metrics=%d", baselineHits.Load(), loadHits.Load(), r.Metrics.Total)
	}
	if issue := report.NetworkBaselineIssue(r); issue != "" {
		t.Fatalf("stable baseline flagged: %s", issue)
	}
	if !report.ValidNetworkBaseline(r) || r.SchemaVersion != report.CurrentSchemaVersion {
		t.Fatalf("baseline evidence invalid in schema %d", r.SchemaVersion)
	}
}

func TestNetworkBaselineUnstablePathLimitsQuality(t *testing.T) {
	server, _, _ := baselineServer(t, func(n int64) time.Duration {
		if n%5 == 0 {
			return 300 * time.Millisecond
		}
		return time.Millisecond
	})
	r := Run(context.Background(), baselineScenario(t, server.URL, 1000))
	if r.NetworkBaseline == nil || r.NetworkBaseline.MaxToP50Ratio <= report.NetworkBaselineDispersionRatio {
		t.Fatalf("jitter was not recorded: %+v", r.NetworkBaseline)
	}
	if r.Quality == nil || r.Quality.State != report.QualityLimited || !hasText(r.Quality.Findings, "unstable") || !hasText(r.Warnings, "network path may dominate") {
		t.Fatalf("unstable baseline did not limit quality: %+v warnings=%v", r.Quality, r.Warnings)
	}
}

func TestNetworkBaselineDominatingObjectiveLimitsQuality(t *testing.T) {
	server, _, _ := baselineServer(t, func(int64) time.Duration { return 60 * time.Millisecond })
	r := Run(context.Background(), baselineScenario(t, server.URL, 100))
	if r.Quality == nil || r.Quality.State == report.QualityValid || !hasText(r.Quality.Findings, "exceeds 50% of the 100 ms request p95 objective") {
		t.Fatalf("dominant baseline did not limit quality: %+v", r.Quality)
	}
}

func TestNetworkBaselineTotalFailureIsInvalid(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	plan := &config.NetworkBaselinePlan{Method: http.MethodGet, URL: "http://" + addr + "/", Path: "/", Source: config.NetworkBaselineSourceStep,
		Requests: 5, Interval: time.Millisecond, Timeout: 200 * time.Millisecond, WallBudget: 5 * time.Second}
	b := runNetworkBaseline(context.Background(), plan, nil)
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Config: report.RunConfig{P95LimitMS: 1000}, NetworkBaseline: b}
	if b.Completed != 0 || b.Failures != 5 || !report.ValidNetworkBaseline(r) || !report.NetworkBaselineInvalid(r) {
		t.Fatalf("unexpected failed baseline: %+v", b)
	}
	if q := report.AssessQuality(r); q.State != report.QualityInvalid {
		t.Fatalf("failed baseline did not invalidate quality: %+v", q)
	}
}

func TestNetworkBaselineExplicitSkipIsRecordedWithoutTraffic(t *testing.T) {
	server, baselineHits, _ := baselineServer(t, func(int64) time.Duration { return 0 })
	v, err := config.Parse(config.Scenario{Name: "skip", BaseURL: server.URL, Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000,
		NetworkBaselineSkipReason: "generator-on-target-host", Steps: []config.Step{{Name: "page", Path: "/page"}}}, config.Options{Authorized: true, NetworkBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	if r.NetworkBaseline == nil || r.NetworkBaseline.Status != report.NetworkBaselineSkipped || r.NetworkBaseline.SkipReason != "generator-on-target-host" || baselineHits.Load() != 0 {
		t.Fatalf("explicit skip not recorded: %+v hits=%d", r.NetworkBaseline, baselineHits.Load())
	}
	if !report.ValidNetworkBaseline(r) {
		t.Fatal("explicit skip evidence invalid")
	}
}

func hasText(values []string, want string) bool {
	for _, v := range values {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}
