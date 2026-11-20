package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func thinkTimeJourneyScenario(t *testing.T, baseURL string, journeyLimitMS float64, profiles bool) config.Validated {
	t.Helper()
	s := config.Scenario{Name: "think-time journey", BaseURL: baseURL, Users: 2, Duration: "1s", MaxRPS: 50, P95LimitMS: 80, JourneyP95LimitMS: journeyLimitMS, MaxErrorRate: 0, ThinkTime: "150ms", WindowDuration: "1s",
		Steps: []config.Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}}
	if profiles {
		s.Steps = nil
		s.Profiles = []config.Profile{
			{Name: "browse", Weight: 1, Steps: []config.Step{{Name: "list", Path: "/list"}, {Name: "item", Path: "/item"}}},
			{Name: "search", Weight: 1, Steps: []config.Step{{Name: "query", Path: "/query"}, {Name: "result", Path: "/result"}}},
		}
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func loopbackFastServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestJourneyP95LimitLetsThinkTimeJourneyPassWithRequestLimitMet(t *testing.T) {
	v := thinkTimeJourneyScenario(t, loopbackFastServer(t), 5000, false)
	r := Run(context.Background(), v)
	if r.Config.JourneyP95LimitMS != 5000 || r.Config.P95LimitMS != 80 {
		t.Fatalf("run config did not retain both limits: %+v", r.Config)
	}
	if r.Journeys.P95MS <= r.Config.P95LimitMS || r.Metrics.P95MS > r.Config.P95LimitMS {
		t.Fatalf("fixture must produce journeys above and requests below the request limit: requests=%+v journeys=%+v", r.Metrics, r.Journeys)
	}
	if r.Outcome != "PASS" {
		t.Fatalf("journey within journey_p95_limit_ms should pass: outcome=%s warnings=%v", r.Outcome, r.Warnings)
	}
	if slices.Contains(r.Warnings, report.JourneyLimitIncludesThinkTimeWarning) {
		t.Fatal("explicit journey limit must not emit the think-time objective warning")
	}
}

func TestJourneyP95LimitFailsWhenJourneyExceedsIt(t *testing.T) {
	v := thinkTimeJourneyScenario(t, loopbackFastServer(t), 100, false)
	r := Run(context.Background(), v)
	if r.Journeys.P95MS <= 100 || r.Outcome != "FAIL" {
		t.Fatalf("journey above journey_p95_limit_ms must fail: journeys=%+v outcome=%s", r.Journeys, r.Outcome)
	}
}

func TestAbsentJourneyP95LimitKeepsRequestLimitAndWarnsAboutThinkTime(t *testing.T) {
	v := thinkTimeJourneyScenario(t, loopbackFastServer(t), 0, false)
	r := Run(context.Background(), v)
	if r.Config.JourneyP95LimitMS != 0 || r.Outcome != "FAIL" {
		t.Fatalf("absent journey limit must keep historical journey FAIL: outcome=%s config=%+v", r.Outcome, r.Config)
	}
	if !slices.Contains(r.Warnings, report.JourneyLimitIncludesThinkTimeWarning) {
		t.Fatalf("missing think-time objective warning: %v", r.Warnings)
	}
}

func TestJourneyP95LimitAppliesToWindowProfileJourneys(t *testing.T) {
	pass := Run(context.Background(), thinkTimeJourneyScenario(t, loopbackFastServer(t), 5000, true))
	if pass.Outcome != "PASS" || len(pass.Windows) == 0 || len(pass.Windows[0].Profiles) != 2 {
		t.Fatalf("profile journeys within journey limit should pass: outcome=%s warnings=%v windows=%+v", pass.Outcome, pass.Warnings, pass.Windows)
	}
	for _, profile := range pass.Windows[0].Profiles {
		if profile.Metrics.P95MS <= pass.Config.P95LimitMS {
			t.Fatalf("profile journey p95 should include think time above the request limit: %+v", profile)
		}
	}
	legacy := Run(context.Background(), thinkTimeJourneyScenario(t, loopbackFastServer(t), 0, true))
	if legacy.Outcome != "FAIL" {
		t.Fatalf("absent journey limit must keep profile journeys on p95_limit_ms: outcome=%s", legacy.Outcome)
	}
}

func TestJourneyP95LimitValidation(t *testing.T) {
	for _, limit := range []float64{-1, 120001} {
		s := config.Scenario{Name: "bounds", BaseURL: "http://127.0.0.1:1", Users: 1, Duration: "1s", MaxRPS: 1, P95LimitMS: 100, JourneyP95LimitMS: limit, Steps: []config.Step{{Name: "a", Path: "/"}}}
		if _, err := config.Parse(s, config.Options{Authorized: true}); err == nil {
			t.Fatalf("journey_p95_limit_ms %v was accepted", limit)
		}
	}
}
