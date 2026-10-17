package engine

import (
	"context"
	"encoding/json"
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
	if result.SchemaVersion != 6 || result.ObservationSeconds != v.Duration.Seconds() || result.Generator == nil || result.Generator.ProcessSamples < 2 {
		t.Fatalf("report must include schema-v6 observation window and generator process observations: %+v", result)
	}
}

func TestWeightedProfilesAreMeasuredSeparately(t *testing.T) {
	seen := map[string]int{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := config.Scenario{
		Name: "weighted local profiles", BaseURL: srv.URL, Users: 4, Duration: "1s", MaxRPS: 50,
		P95LimitMS: 1000, MaxErrorRate: 0,
		Profiles: []config.Profile{
			{Name: "browse", Weight: 3, Steps: []config.Step{{Name: "dashboard", Path: "/dashboard"}}},
			{Name: "search", Weight: 1, Steps: []config.Step{{Name: "search", Path: "/search"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || len(r.Profiles) != 2 || r.Profiles[0].AssignedVUs != 3 || r.Profiles[1].AssignedVUs != 1 ||
		r.Profiles[0].Journeys.Total == 0 || r.Profiles[1].Journeys.Total == 0 {
		t.Fatalf("weighted profile evidence missing: %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["/dashboard"] == 0 || seen["/search"] == 0 {
		t.Fatalf("expected both workflows, got %+v", seen)
	}
}

func TestSyntheticDataIDsAreUniqueAndNeverReported(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("DAMOCLINE_TEST_DATA_PREFIX", "damocline-test")
	s := config.Scenario{Name: "synthetic write", BaseURL: srv.URL, Users: 2, Duration: "1s", MaxRPS: 30, P95LimitMS: 1000, MaxErrorRate: 0,
		SyntheticDataPrefixEnv: "DAMOCLINE_TEST_DATA_PREFIX", Steps: []config.Step{{Name: "create", Method: "POST", Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{204}}, {Name: "cleanup", Method: "DELETE", Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{204}}}}
	v, err := config.Parse(s, config.Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Success == 0 || len(seen) < 2 {
		t.Fatalf("synthetic workflow did not execute: %+v paths=%v", r, seen)
	}
	for path := range seen {
		if !strings.HasPrefix(path, "/records/damocline-test-") {
			t.Fatalf("unexpected synthetic path %q", path)
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || strings.Contains(string(encoded), "damocline-test-") {
		t.Fatalf("synthetic IDs must not be reported: %s err=%v", encoded, err)
	}
}

func TestJourneyExtractsResponseValuesForLaterSteps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("X-Test-Token", "token-123")
			_, _ = w.Write([]byte(`{"record":{"id":"alpha/beta"}}`))
		case "/records/alpha/beta":
			if r.Header.Get("X-Token") != "Bearer token-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("record found"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := config.Scenario{Name: "dependent local journey", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{
			{Name: "session", Path: "/session", Extract: map[string]config.Extract{"record_id": {JSONPointer: "/record/id"}, "token": {Header: "X-Test-Token"}}},
			{Name: "record", Path: "/records/{{record_id}}", Headers: map[string]string{"X-Token": "Bearer {{token}}"}, MustContain: "record found"},
		}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || r.Steps[0].Metrics.Success == 0 || r.Steps[1].Metrics.Success == 0 {
		t.Fatalf("dependent journey did not complete: %+v", r)
	}
}

func TestFailedExtractionFailsStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"missing":true}`))
	}))
	defer srv.Close()
	s := config.Scenario{Name: "failed extraction", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 10, P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "session", Path: "/", Extract: map[string]config.Extract{"record_id": {JSONPointer: "/id"}}}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 200 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome == "PASS" || r.Metrics.Reasons["extraction_failed"] == 0 {
		t.Fatalf("missing extraction was not reported: %+v", r)
	}
}

func TestObservationWindowsExposeLateSLOBreach(t *testing.T) {
	started := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if time.Since(started) > 150*time.Millisecond {
			time.Sleep(60 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Duration = 350 * time.Millisecond
	v.Window = 100 * time.Millisecond
	v.Scenario.P95LimitMS = 20
	r := Run(context.Background(), v)
	if len(r.Windows) < 2 || r.Outcome != "FAIL" {
		t.Fatalf("late window degradation was not visible: %+v", r)
	}
	found := false
	for _, warning := range r.Warnings {
		if strings.Contains(warning, "breached an SLO") {
			found = true
		}
	}
	if !found {
		t.Fatalf("window breach warning missing: %+v", r.Warnings)
	}
}

func TestArrivalProfilesFollowConfiguredWeights(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := config.Scenario{
		Name: "weighted arrival profiles", BaseURL: srv.URL, Users: 2, Duration: "1s", Mode: "arrival", ArrivalRate: 10, MaxRPS: 20,
		P95LimitMS: 1000, MaxErrorRate: 0,
		Profiles: []config.Profile{
			{Name: "browse", Weight: 3, Steps: []config.Step{{Name: "dashboard", Path: "/dashboard"}}},
			{Name: "search", Weight: 1, Steps: []config.Step{{Name: "search", Path: "/search"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || r.ArrivalsScheduled != 10 || r.ArrivalsDropped != 0 || len(r.Profiles) != 2 ||
		r.Profiles[0].Journeys.Total != 8 || r.Profiles[1].Journeys.Total != 2 ||
		r.Profiles[0].AssignedVUs != 0 || r.Profiles[1].AssignedVUs != 0 {
		t.Fatalf("arrival profile mix was not measured deterministically: %+v", r)
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
		// A 160ms response spans several 50ms arrival intervals. The last
		// admitted request must still be running at the 300ms cutoff,
		// independent of Windows/Linux scheduling jitter.
		time.Sleep(160 * time.Millisecond)
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
