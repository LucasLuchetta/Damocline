package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func journeyMixScenario(baseURL string) config.Scenario {
	return config.Scenario{
		Name: "journey mix 60/25/15", BaseURL: baseURL, Users: 4, Duration: "3s",
		MaxRPS: 100, P95LimitMS: 1000, MaxErrorRate: 0,
		ProfileMixBasis: "sanitized_analytics", ProfileMixReference: "synthetic-access-summary",
		ProfileMixReferenceCounts: map[string]int64{"view_dashboard": 600, "search_records": 250, "update_record": 150},
		PerVUCookieEnvPrefix:      "DAMOCLINE_MIX_TEST_COOKIE_",
		SyntheticDataPrefixEnv:    "DAMOCLINE_MIX_TEST_PREFIX",
		Profiles: []config.Profile{
			{Name: "view_dashboard", Weight: 60, Steps: []config.Step{{Name: "dashboard", Method: http.MethodGet, Path: "/dashboard"}}},
			{Name: "search_records", Weight: 25, Steps: []config.Step{{Name: "search", Method: http.MethodGet, Path: "/records?q=synthetic"}}},
			{Name: "update_record", Weight: 15, Steps: []config.Step{
				{Name: "update", Method: http.MethodPut, Path: "/records/{{synthetic_id}}"},
				{Name: "revert", Method: http.MethodDelete, Path: "/records/{{synthetic_id}}", Cleanup: true},
			}},
		},
	}
}

func setJourneyMixEnv(t *testing.T) {
	t.Setenv("DAMOCLINE_MIX_TEST_PREFIX", "synthetic-mix")
	for i, value := range []string{"sid=a1", "sid=b2", "sid=c3", "sid=d4"} {
		t.Setenv("DAMOCLINE_MIX_TEST_COOKIE_"+string(rune('1'+i)), value)
	}
}

// TestJourneyMix601525WithReversibleWriteWorkflow runs a declared 60/25/15
// mix including a write workflow against a loopback ledger. The executed mix
// must match the declaration, every write must carry the synthetic prefix and
// a per-actor identity, writes must reconcile with cleanups and the server
// ledger, and the saved report must survive offline recomputation.
func TestJourneyMix601525WithReversibleWriteWorkflow(t *testing.T) {
	setJourneyMixEnv(t)
	var mu sync.Mutex
	received := map[string]int{}
	live := map[string]string{}
	badWrites := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.Method + " " + strings.SplitN(r.URL.Path, "/", 3)[1]
		received[key]++
		if r.Method == http.MethodPut || r.Method == http.MethodDelete {
			id := strings.TrimPrefix(r.URL.Path, "/records/")
			cookie, _ := r.Cookie("sid")
			if !strings.HasPrefix(id, "synthetic-mix-") || cookie == nil {
				badWrites++
			}
			if r.Method == http.MethodPut {
				live[id] = cookie.Value
			} else if owner, ok := live[id]; !ok || owner != cookie.Value {
				badWrites++
			} else {
				delete(live, id)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	v, err := config.Parse(journeyMixScenario(server.URL), config.Options{Authorized: true, AllowWrites: true, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	mix := r.ProfileMix
	if mix == nil || mix.State != report.MixMatched || mix.Starts.State != report.MixMatched || mix.Completed.State != report.MixMatched {
		t.Fatalf("declared 60/25/15 mix was not matched: %+v outcome=%s warnings=%v", mix, r.Outcome, r.Warnings)
	}
	if mix.Reference == nil || mix.Reference.Fit.State != report.ReferenceMatched || mix.Reference.Provenance != report.ReferenceProvenance {
		t.Fatalf("reference mix was not recorded as a matched assumption: %+v", mix.Reference)
	}
	for _, profile := range r.Config.Profiles {
		wantWrites := profile.Name == "update_record"
		wantVariability := report.DataVariabilityFixed
		if wantWrites {
			wantVariability = report.DataVariabilitySyntheticPerJourney
		}
		if profile.Writes != wantWrites || profile.DataVariability != wantVariability {
			t.Fatalf("workflow declaration not recorded: %+v", profile)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if badWrites != 0 || len(live) != 0 {
		t.Fatalf("writes were not synthetic, per-actor and reversible: bad=%d leftover=%d", badWrites, len(live))
	}
	steps := map[string]report.StepResult{}
	for _, step := range r.Steps {
		steps[step.Name] = step
	}
	var update report.ProfileMixWorkflow
	for _, workflow := range mix.Workflows {
		if workflow.Name == "update_record" {
			update = workflow
		}
	}
	if received["PUT records"] != steps["update"].Metrics.Total || received["DELETE records"] != steps["revert"].Metrics.Total ||
		received["PUT records"] != received["DELETE records"] || update.CompletedJourneys != steps["revert"].Metrics.Total || update.SelectedStarts < update.CompletedJourneys {
		t.Fatalf("write counts do not reconcile: server=%v update=%+v revert=%+v workflow=%+v", received, steps["update"].Metrics, steps["revert"].Metrics, update)
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var saved report.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if err := insights.ValidateRunReport(saved); err != nil {
		t.Fatalf("saved mix evidence failed offline validation: %v", err)
	}
	saved.ProfileMix.Workflows[0].SelectedStarts++
	if err := report.ValidProfileMix(saved); err == nil {
		t.Fatal("tampered mix evidence passed offline recomputation")
	}
}

func TestJourneyMixWriteWorkflowRequiresSafetyControls(t *testing.T) {
	setJourneyMixEnv(t)
	base := journeyMixScenario("http://127.0.0.1:9")
	options := config.Options{Authorized: true, AllowWrites: true, AllowInsecureHTTP: true}
	if _, err := config.Parse(base, config.Options{Authorized: true, AllowInsecureHTTP: true}); err == nil || !strings.Contains(err.Error(), "--allow-writes") {
		t.Fatalf("write workflow ran without --allow-writes: %v", err)
	}
	noPrefix := journeyMixScenario("http://127.0.0.1:9")
	noPrefix.SyntheticDataPrefixEnv = ""
	noPrefix.Profiles[2].Steps = noPrefix.Profiles[2].Steps[:1]
	noPrefix.Profiles[2].Steps[0].Path = "/records/fixed"
	if _, err := config.Parse(noPrefix, options); err == nil || !strings.Contains(err.Error(), "synthetic_data_prefix_env") {
		t.Fatalf("write workflow ran without a synthetic prefix: %v", err)
	}
	noIdentity := journeyMixScenario("http://127.0.0.1:9")
	noIdentity.PerVUCookieEnvPrefix = ""
	if _, err := config.Parse(noIdentity, options); err == nil || !strings.Contains(err.Error(), "per-actor identity") {
		t.Fatalf("write workflow ran without per-actor identity: %v", err)
	}
	fixedData := journeyMixScenario("http://127.0.0.1:9")
	fixedData.Profiles[2].Steps = fixedData.Profiles[2].Steps[:1]
	fixedData.Profiles[2].Steps[0].Path = "/records/fixed"
	if _, err := config.Parse(fixedData, options); err == nil || !strings.Contains(err.Error(), "{{synthetic_id}}") {
		t.Fatalf("write workflow ran without synthetic data: %v", err)
	}
	badReference := journeyMixScenario("http://127.0.0.1:9")
	badReference.ProfileMixReferenceCounts = map[string]int64{"view_dashboard": 1}
	if _, err := config.Parse(badReference, options); err == nil || !strings.Contains(err.Error(), "one count per declared workflow") {
		t.Fatalf("incomplete reference counts were accepted: %v", err)
	}
	assumption := journeyMixScenario("http://127.0.0.1:9")
	assumption.ProfileMixBasis, assumption.ProfileMixReference = "assumption", ""
	if _, err := config.Parse(assumption, options); err == nil || !strings.Contains(err.Error(), "non-assumption") {
		t.Fatalf("reference counts without provenance were accepted: %v", err)
	}
}
