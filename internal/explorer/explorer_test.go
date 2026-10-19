package explorer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestPerWorkflowSessionsScaleToExplorationMaximum(t *testing.T) {
	for actor := 1; actor <= 3; actor++ {
		t.Setenv(fmt.Sprintf("EXPLORE_READ_%d", actor), fmt.Sprintf("sid=read-%d", actor))
		t.Setenv(fmt.Sprintf("EXPLORE_SEARCH_%d", actor), fmt.Sprintf("sid=search-%d", actor))
	}
	scenario := config.Scenario{Name: "bounded auth exploration", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 20, P95LimitMS: 1000,
		Profiles: []config.Profile{
			{Name: "read", Weight: 1, PerVUCookieEnvPrefix: "EXPLORE_READ_", Steps: []config.Step{{Name: "read", Path: "/read"}}},
			{Name: "search", Weight: 1, PerVUCookieEnvPrefix: "EXPLORE_SEARCH_", Steps: []config.Step{{Name: "search", Path: "/search"}}},
		}}
	base, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	observed := map[int]bool{}
	_, err = WithRunner(context.Background(), base, Options{MinUsers: 1, MaxUsers: 3, Repeats: 2, StageDuration: 10 * time.Second}, func(_ context.Context, run config.Validated) report.Result {
		for _, profile := range run.Profiles {
			if len(profile.Cookies) != run.Scenario.Users {
				t.Errorf("workflow %q has %d actor cookies at %d users", profile.Name, len(profile.Cookies), run.Scenario.Users)
			}
		}
		observed[run.Scenario.Users] = true
		return report.Result{Outcome: "PASS"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !observed[1] || !observed[2] || !observed[3] {
		t.Fatalf("exploration did not exercise expected worker levels with profile sessions: %v", observed)
	}
	t.Setenv("EXPLORE_SEARCH_2", "sid=read-2")
	runnerCalled := false
	if _, err := WithRunner(context.Background(), base, Options{MinUsers: 1, MaxUsers: 3, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result {
		runnerCalled = true
		return report.Result{Outcome: "PASS"}
	}); err == nil || runnerCalled {
		t.Fatal("exploration did not validate distinct cookies through its maximum actor count")
	}
}

func TestSchemaTwoPersistsProbeAndAbortReasons(t *testing.T) {
	result := Result{SchemaVersion: 2, Outcome: "INCONCLUSIVE", Reason: "wall-time cap reached", Probes: []Probe{{Users: 7, Status: "INCONCLUSIVE", Reason: "arrivals were dropped"}}}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var restored Result
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.SchemaVersion != 2 || restored.Reason != result.Reason || len(restored.Probes) != 1 || restored.Probes[0].Reason != result.Probes[0].Reason {
		t.Fatalf("versioned exploration reasons did not round-trip: %+v", restored)
	}
}

func TestBracketAndRefine(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "test", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 2, MaxUsers: 30, Repeats: 2, StageDuration: 10 * time.Second}, func(_ context.Context, v config.Validated) report.Result {
		counts[v.Scenario.Users]++
		outcome := "PASS"
		if v.Scenario.Users > 13 {
			outcome = "FAIL"
		}
		return report.Result{Outcome: outcome}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.HighestPassingUsers == nil || *r.HighestPassingUsers != 13 || r.FirstFailingUsers == nil || *r.FirstFailingUsers != 14 {
		t.Fatalf("bad bracket: %+v", r)
	}
	if r.Outcome != "BRACKETED" {
		t.Fatalf("bad outcome: %s", r.Outcome)
	}
	if counts[13] != 2 {
		t.Fatalf("passing level should be confirmed twice: %d", counts[13])
	}
}
func TestNoPassingLevel(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result { return report.Result{Outcome: "FAIL"} })
	if err != nil || r.Outcome != "NO_PASSING_LEVEL" || r.HighestPassingUsers != nil {
		t.Fatalf("unexpected: %+v, %v", r, err)
	}
}

func TestGeneratorLimitedFailureCannotBracketApplicationCapacity(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 8, Repeats: 2, StageDuration: 10 * time.Second}, func(_ context.Context, run config.Validated) report.Result {
		return report.Result{Outcome: "FAIL", SchemaVersion: 3, ElapsedSeconds: 10, ObservationSeconds: 10,
			Config: report.RunConfig{Users: run.Scenario.Users, Mode: "closed", MaxRPS: 100}, Metrics: report.MetricSet{Total: 100, Failures: 100},
			Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.95,
				ProcessSamples: 4, ProcessCPUValid: true, PeakProcessRSSBytes: 1024}}
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || result.FirstFailingUsers != nil || result.Reason == "" {
		t.Fatalf("generator-bound failure was reported as an application bracket: %+v err=%v", result, err)
	}
}

func TestAbort(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := WithRunner(ctx, v, Options{MinUsers: 1, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result {
		t.Fatal("should not run")
		return report.Result{}
	})
	if err != nil || r.Outcome != "ABORTED" || r.Reason == "" || len(r.Probes) != 1 || r.Probes[0].Reason == "" {
		t.Fatalf("unexpected: %+v %v", r, err)
	}
}

func TestInconclusiveGeneratorCannotBecomeApplicationFailure(t *testing.T) {
	v, err := config.Parse(config.Scenario{
		Name: "local scenario", BaseURL: "http://127.0.0.1:18080",
		Users: 1, Duration: "10s", MaxRPS: 20, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "local", Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := WithRunner(context.Background(), v, Options{
		MinUsers: 1, MaxUsers: 4, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "INCONCLUSIVE", ArrivalsDroppedLate: 1}
	})
	if err != nil || r.Outcome != "INCONCLUSIVE" || r.HighestPassingUsers != nil || r.FirstFailingUsers != nil {
		t.Fatalf("generator limitations must not be branded as server capacity failure: %+v err=%v", r, err)
	}
}

func TestPassWithInvalidOrIncompleteDeliveryCannotCountAsPassing(t *testing.T) {
	cancelled := int64(1)
	for _, tc := range []struct {
		name string
		run  report.Result
	}{
		{name: "invalid quality", run: report.Result{Outcome: "PASS", Quality: &report.Quality{State: report.QualityInvalid}}},
		{name: "dropped arrivals", run: report.Result{Outcome: "PASS", ArrivalsDropped: 1}},
		{name: "cancelled before admission", run: report.Result{Outcome: "PASS", ArrivalsCancelledBeforeAdmission: &cancelled}},
		{name: "cancelled journey", run: report.Result{Outcome: "PASS", Journeys: report.MetricSet{Cancelled: 1}}},
		{name: "current schema ledger invalid", run: report.Result{SchemaVersion: report.CurrentSchemaVersion, Outcome: "PASS"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := config.Parse(config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:18080", Users: 1, Duration: "10s", MaxRPS: 20, P95LimitMS: 1000, Steps: []config.Step{{Name: "page", Path: "/"}}}, config.Options{Authorized: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 1, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result { return tc.run })
			if err != nil || result.Outcome != "INCONCLUSIVE" || result.HighestPassingUsers != nil || len(result.Probes) != 1 || result.Probes[0].Reason == "" {
				t.Fatalf("invalid delivery was counted as a passing probe: %+v err=%v", result, err)
			}
		})
	}
}

func TestBusinessTargetIsTestedAtExactLoad(t *testing.T) {
	v, err := config.Parse(config.Scenario{
		Name: "synthetic goal", BaseURL: "https://example.com",
		Users: 2, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "page", Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	out, err := WithRunner(context.Background(), v, Options{
		MinUsers: 2, MaxUsers: 20, TargetUsers: 15, Repeats: 2,
		StageDuration: 10 * time.Second,
	}, func(_ context.Context, c config.Validated) report.Result {
		counts[c.Scenario.Users]++
		return report.Result{Outcome: "PASS"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts[15] != 2 || out.Outcome != "PASSED_TO_CONFIGURED_CEILING" {
		t.Fatalf("exact business goal stage missing: %+v counts=%v", out, counts)
	}
}
func TestOutOfRangeBusinessTargetNeverRaisesSafetyCeiling(t *testing.T) {
	v, err := config.Parse(config.Scenario{
		Name: "synthetic goal", BaseURL: "https://example.com",
		Users: 2, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "page", Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	_, err = WithRunner(context.Background(), v, Options{
		MinUsers: 2, MaxUsers: 16, TargetUsers: 250, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, c config.Validated) report.Result {
		counts[c.Scenario.Users]++
		return report.Result{Outcome: "PASS"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts[250] != 0 || counts[16] == 0 {
		t.Fatalf("exceeded safety range: %v", counts)
	}
}
