package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

// A step that met its objectives on too few samples is missing evidence,
// not an objective breach. It must be INCONCLUSIVE, never FAIL, so an
// explorer cannot report a healthy target as failing at its lowest level.
func TestRunWithTooFewStepSamplesIsInconclusiveNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Scenario.MinSamplesPerStep = 100000
	result := Run(context.Background(), v)
	if result.Metrics.Total == 0 || result.Metrics.Failures != 0 {
		t.Fatalf("fixture did not complete clean requests: %+v", result.Metrics)
	}
	if result.Outcome != "INCONCLUSIVE" {
		t.Fatalf("insufficient samples produced %s, want INCONCLUSIVE", result.Outcome)
	}
}

// A real breach on a small sample still fails.
func TestRunWithFewSamplesAndBreachStillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Scenario.MinSamplesPerStep = 100000
	if result := Run(context.Background(), v); result.Outcome != "FAIL" {
		t.Fatalf("failing requests produced %s, want FAIL", result.Outcome)
	}
}
