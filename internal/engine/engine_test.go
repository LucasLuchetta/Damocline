package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func scenario(t *testing.T, url string, steps []config.Step) config.Validated {
	t.Helper()
	raw := config.Scenario{Name: "local test", BaseURL: url, Users: 2, Duration: "900ms", MaxRPS: 20, P95LimitMS: 2000, MaxErrorRate: 0, RampUp: "100ms", Timeout: "300ms", Steps: steps}
	// For fast unit tests, apply the duration limit at the config layer by testing
	// a short execution interval through direct construction below.
	raw.Duration = "1s"
	validated, err := config.Parse(raw, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	validated.Duration = 300 * time.Millisecond
	return validated
}
func TestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("expected page content"))
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/page", MustContain: "expected page"}})
	result := Run(context.Background(), v)
	if result.Metrics.Success < 1 || result.Metrics.Failures != 0 || result.Outcome != "PASS" {
		t.Fatalf("unexpected report: %+v", result)
	}
	if result.SchemaVersion != 4 || result.ObservationSeconds != v.Duration.Seconds() || result.Generator == nil || result.Generator.ProcessSamples < 2 {
		t.Fatalf("report must include schema-v4 observation window and generator process observations: %+v", result)
	}
}
func TestLoginRedirectIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", 302) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/private"}})
	r := Run(context.Background(), v)
	if r.Outcome != "FAIL" || r.Metrics.Reasons["http_302"] == 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestExpiredRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(500 * time.Millisecond); w.WriteHeader(200) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "slow", Path: "/slow"}})
	v.Timeout = 80 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Reasons["timeout"] < 1 {
		t.Fatalf("did not account for timeouts: %+v", r)
	}
}
func TestSecretNeverAppearsInOutput(t *testing.T) {
	t.Setenv("SECRET_FOR_ENGINE_TEST", "secret-session-value")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "secret-session-value" {
			t.Errorf("cookie missing")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "auth", Path: "/dashboard?private=secret", Headers: map[string]string{"Cookie": "${SECRET_FOR_ENGINE_TEST}"}}})
	r := Run(context.Background(), v)
	if strings.Contains(r.Steps[0].Path, "private") || strings.Contains(r.Steps[0].Path, "secret") {
		t.Fatalf("query leaked in path: %s", r.Steps[0].Path)
	}
}

func TestDistinctCookieSessions(t *testing.T) {
	t.Setenv("DAMOCLINE_VU_TEST_1", "sid=alpha")
	t.Setenv("DAMOCLINE_VU_TEST_2", "sid=beta")
	var mu sync.Mutex
	observed := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("sid")
		if err != nil {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		observed[ck.Value]++
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := config.Scenario{Name: "distinct", BaseURL: srv.URL, Users: 2, Duration: "1s", MaxRPS: 30, P95LimitMS: 2000, MinSamplesPerStep: 2, PerVUCookieEnvPrefix: "DAMOCLINE_VU_TEST_", Steps: []config.Step{{Name: "page", Path: "/"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	mu.Lock()
	defer mu.Unlock()
	if r.Outcome != "PASS" || observed["alpha"] == 0 || observed["beta"] == 0 {
		t.Fatalf("session isolation failed result=%+v observed=%v", r, observed)
	}
}
func TestArrivalDropReporting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(250 * time.Millisecond); w.WriteHeader(200) }))
	defer srv.Close()
	s := config.Scenario{Name: "arrival", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 30, Mode: "arrival", ArrivalRate: 20, P95LimitMS: 1000, Steps: []config.Step{{Name: "slow", Path: "/"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), v)
	if result.ArrivalsDropped == 0 || result.Outcome != "FAIL" {
		t.Fatalf("underdelivery must fail: %+v", result)
	}
}

func TestPlannedArrivalAccountingWithSlowLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(220 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	s := config.Scenario{
		Name: "local arrival fixture", BaseURL: server.URL, Users: 1,
		Duration: "1s", Mode: "arrival", ArrivalRate: 20, MaxRPS: 30,
		P95LimitMS: 1500, Steps: []config.Step{{Name: "page", Method: "GET", Path: "/"}},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	out := Run(context.Background(), v)
	if out.ArrivalsScheduled != 20 {
		t.Fatalf("lost planned events: expected 20, got %d: %+v", out.ArrivalsScheduled, out)
	}
	if out.ArrivalsScheduled != out.IterationsStarted+out.ArrivalsDropped {
		t.Fatalf("planned events do not reconcile: %+v", out)
	}
	if out.ArrivalsDropped != out.ArrivalsDroppedBusy+out.ArrivalsDroppedLate {
		t.Fatalf("dropped event reasons do not reconcile: %+v", out)
	}
	if out.ArrivalsDropped == 0 || out.Outcome == "PASS" {
		t.Fatalf("undelivered workload must not pass: %+v", out)
	}
}

func TestArrivalRunDrainsAdmittedWorkAfterScheduleWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(25 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	s := config.Scenario{Name: "arrival drain", BaseURL: server.URL, Users: 10,
		Duration: "1s", Mode: "arrival", ArrivalRate: 20, MaxRPS: 30, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.ArrivalsScheduled != 6 || r.ArrivalsDropped != 0 || r.IterationsCompleted != r.IterationsStarted ||
		r.Metrics.Cancelled != 0 || r.Outcome != "PASS" || r.ObservationSeconds != v.Duration.Seconds() ||
		r.ElapsedSeconds <= r.ObservationSeconds {
		t.Fatalf("admitted arrivals should drain after scheduling stops: %+v", r)
	}
}

func TestCancelledRunCannotPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/"}})
	v.Duration = 50 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Cancelled == 0 || r.Outcome == "PASS" {
		t.Fatalf("a run with cancelled requests must not pass: %+v", r)
	}
}

func TestInterruptedJourneyIsCountedAndCannotPass(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/"}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-requestStarted
		cancel()
	}()
	r := Run(ctx, v)
	if r.Journeys.Cancelled == 0 || r.Metrics.Cancelled == 0 ||
		r.Journeys.Total+r.Journeys.Cancelled != int(r.IterationsStarted) || r.Outcome != "ABORTED" {
		t.Fatalf("interrupted journey was not counted explicitly: %+v", r)
	}
}

func TestRunIncludesLocalGeneratorHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	result := Run(context.Background(), v)
	if result.Generator == nil || result.Generator.Samples < 2 ||
		result.Generator.PeakGoroutines < 1 || result.Generator.PeakHeapAllocBytes == 0 {
		t.Fatalf("generator health not sampled: %+v", result.Generator)
	}
	if result.Outcome != "PASS" {
		t.Fatalf("monitoring changed successful workload: %+v", result)
	}
}

func TestInsufficientSamplesCannotPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Scenario.MinSamplesPerStep = 1000
	r := Run(context.Background(), v)
	if r.Metrics.Success == 0 || r.Metrics.Total >= 1000 || r.Outcome == "PASS" {
		t.Fatalf("insufficient samples must not pass: %+v", r)
	}
}

func TestResponseBodyLimitIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, maxResponseBytes+1))
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "large", Path: "/"}})
	r := Run(context.Background(), v)
	if r.Metrics.Reasons["response_too_large"] == 0 || r.Outcome == "PASS" {
		t.Fatalf("oversized responses must be bounded and fail visibly: %+v", r)
	}
}
