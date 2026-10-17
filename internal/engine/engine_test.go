package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
