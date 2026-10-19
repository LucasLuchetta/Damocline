package arrivalexplorer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestArrivalRateSearchUsesJourneyStartsAndExactTarget(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	counts := map[int]int{}
	result, err := WithRunner(context.Background(), base, Options{
		MinJourneysPerSecond: 2, MaxJourneysPerSecond: 12, TargetJourneysPerSecond: 6,
		Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, v config.Validated) report.Result {
		rate := v.Scenario.ArrivalRate
		counts[rate]++
		outcome := "PASS"
		if rate >= 8 {
			outcome = "FAIL"
		}
		return completeRun(v, outcome, rate)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "BRACKETED" || result.HighestPassingJourneysPerSecond == nil || *result.HighestPassingJourneysPerSecond != 7 ||
		result.FirstFailingJourneysPerSecond == nil || *result.FirstFailingJourneysPerSecond != 8 {
		t.Fatalf("unexpected journey-start bracket: %+v", result)
	}
	if counts[6] != 2 || counts[7] != 2 || counts[8] != 1 {
		t.Fatalf("passing levels or exact target did not repeat, or first failure was unexpectedly repeated: %v", counts)
	}
	if result.WorkerLimit != 10 || result.Probes[0].Runs[0].Config.Users != 10 || result.Probes[0].Runs[0].Config.ArrivalRate != 2 {
		t.Fatalf("worker limit and scheduled rate were conflated: %+v", result)
	}
	if !result.TargetTested {
		t.Fatal("the explicitly configured journey-start target was not recorded as tested")
	}
}

func TestUnderdeliveryAndGeneratorBindingAreInconclusive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*report.Result)
	}{
		{name: "busy workers", mutate: func(r *report.Result) { r.ArrivalsDroppedBusy = 1; r.ArrivalsDropped = 1 }},
		{name: "late scheduler", mutate: func(r *report.Result) { r.ArrivalsDroppedLate = 1; r.ArrivalsDropped = 1 }},
		{name: "cancelled arrival", mutate: func(r *report.Result) { n := int64(1); r.ArrivalsCancelledBeforeAdmission = &n }},
		{name: "generator cpu", mutate: func(r *report.Result) { r.Generator.GoCPUUtilizationRatio = 0.95 }},
		{name: "client rate ceiling", mutate: func(r *report.Result) { r.Metrics.Total = 950; r.Config.MaxRPS = 100; r.ObservationSeconds = 10 }},
		{name: "missing generator evidence", mutate: func(r *report.Result) { r.Generator = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := arrivalScenario(t, 100, 10)
			result, err := WithRunner(context.Background(), base, Options{
				MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
			}, func(_ context.Context, v config.Validated) report.Result {
				r := completeRun(v, "FAIL", v.Scenario.ArrivalRate)
				tc.mutate(&r)
				return r
			})
			if err != nil || (result.Outcome != "INVALID_DELIVERY" && result.Outcome != "INSUFFICIENT_EVIDENCE") || result.FirstFailingJourneysPerSecond != nil || len(result.Probes) != 1 {
				t.Fatalf("generator-bound or underdelivered load formed an application bracket: %+v err=%v", result, err)
			}
		})
	}
}

func TestArrivalRateBoundsPreserveWorkerAndRequestCeilings(t *testing.T) {
	scenario := config.Scenario{Name: "arrival", Mode: "arrival", MaxRPS: 40, Users: 4, Steps: []config.Step{
		{Name: "short", Path: "/short"}, {Name: "long", Path: "/long"},
	}}
	valid := Options{MinJourneysPerSecond: 1, MaxJourneysPerSecond: 20, Repeats: 2, StageDuration: 10 * time.Second}
	if err := Validate(valid, scenario); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	shortBudget := valid
	shortBudget.Budget = 10 * time.Second
	if err := Validate(shortBudget, scenario); err == nil {
		t.Fatal("budget that cannot include the bounded engine drain was accepted")
	}
	withDrain := valid
	withDrain.Budget = 40 * time.Second
	if err := Validate(withDrain, scenario); err != nil {
		t.Fatalf("budget covering a stage and maximum drain was rejected: %v", err)
	}
	tooHigh := valid
	tooHigh.MaxJourneysPerSecond = 21
	if err := Validate(tooHigh, scenario); err == nil {
		t.Fatal("rate above request-ceiling-derived bound was accepted")
	}
	wrongMode := scenario
	wrongMode.Mode = "closed"
	if err := Validate(valid, wrongMode); err == nil {
		t.Fatal("arrival exploration accepted a closed-loop scenario")
	}
}

func TestSharedExplicitCookieCannotEstablishIndependentSessions(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	base.Steps[0].ResolvedHeaders = map[string]string{"Cookie": "synthetic-session"}
	run := completeRun(base, "PASS", 1)
	run.Warnings = []string{"Multiple virtual users may share one login identity; configure per_vu_cookie_env_prefix to test independent authenticated sessions."}
	if status, _ := classify(run, base); status != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("shared authenticated identity was treated as independent session evidence: %s", status)
	}
}

func TestMissingStepLedgerIsInsufficientEvidence(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	result, err := WithRunner(context.Background(), base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, v config.Validated) report.Result {
		r := completeRun(v, "PASS", 1)
		r.Steps = nil
		return r
	})
	if err != nil || result.Outcome != "INSUFFICIENT_EVIDENCE" || len(result.Probes) != 1 || result.Probes[0].Status != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("missing step evidence was accepted as passing: result=%+v err=%v", result, err)
	}
}

func TestArrivalExplorerRunsBoundedLoopbackStages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "loopback-arrival-explorer", BaseURL: server.URL, Users: 2,
		Mode: "arrival", ArrivalRate: 1, Duration: "10s", MaxRPS: 10,
		Timeout: "2s", P95LimitMS: 5000, MaxErrorRate: 0.9,
		Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), v, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 1, Repeats: 2,
		StageDuration: 10 * time.Second, Budget: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "PASSED_TO_CONFIGURED_CEILING" || len(result.Probes) != 1 || len(result.Probes[0].Runs) != 2 {
		t.Fatalf("unexpected bounded loopback exploration result: %+v", result)
	}
	for _, run := range result.Probes[0].Runs {
		if run.Config.Users != 2 || run.Config.ArrivalRate != 1 || run.ArrivalsScheduled < 9 || run.ArrivalsDropped != 0 || run.Metrics.Total < 9 {
			t.Fatalf("loopback run did not preserve worker/rate units or deliver the bounded schedule: %+v", run)
		}
		if run.ArrivalsScheduled != int64(run.Metrics.Total) || int64(run.Journeys.Total) != run.ArrivalsScheduled {
			t.Fatalf("one-step loopback ledgers should reconcile independently: scheduled=%d requests=%d journeys=%d", run.ArrivalsScheduled, run.Metrics.Total, run.Journeys.Total)
		}
	}
}

func TestAbortAndInsufficientSamplesDoNotBecomeFailingRates(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := WithRunner(ctx, base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(context.Context, config.Validated) report.Result {
		t.Fatal("cancelled probe must not run")
		return report.Result{}
	})
	if err != nil || result.Outcome != "ABORTED" || result.Probes[0].Reason == "" {
		t.Fatalf("caller interruption was not preserved: %+v err=%v", result, err)
	}

	result, err = WithRunner(context.Background(), base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, v config.Validated) report.Result {
		r := completeRun(v, "FAIL", 1)
		r.Config.MinSamplesPerStep = 20
		return r
	})
	if err != nil || result.Outcome != "INSUFFICIENT_EVIDENCE" || result.FirstFailingJourneysPerSecond != nil {
		t.Fatalf("insufficient sample coverage became an application failure: %+v err=%v", result, err)
	}
}

func TestInterruptionBetweenLevelsNeverCreatesAFailureBracket(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	result, err := WithRunner(ctx, base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 4, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		cancel()
		return completeRun(v, "PASS", v.Scenario.ArrivalRate)
	})
	if err != nil || result.Outcome != "ABORTED" || result.FirstFailingJourneysPerSecond != nil || calls != 1 {
		t.Fatalf("interruption between levels was interpreted as a ceiling: result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestDeadlineDuringProbeAbortsSearch(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := WithRunner(ctx, base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(ctx context.Context, _ config.Validated) report.Result {
		<-ctx.Done()
		return report.Result{Outcome: "ABORTED"}
	})
	if err != nil || result.Outcome != "ABORTED" || result.FirstFailingJourneysPerSecond != nil ||
		len(result.Probes) != 1 || result.Probes[0].Status != "ABORTED" || result.Reason == "" {
		t.Fatalf("deadline during a probe was interpreted as an application limit: result=%+v err=%v", result, err)
	}
}

func TestMissingApplicationAuthenticationCannotFormRateBracket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}))
	defer server.Close()
	base, err := config.Parse(config.Scenario{
		Name: "loopback-auth-required", BaseURL: server.URL, Users: 2,
		Mode: "arrival", ArrivalRate: 1, Duration: "10s", MaxRPS: 10,
		Timeout: "2s", P95LimitMS: 5000, MaxErrorRate: 0.01,
		Steps: []config.Step{{Name: "protected-read", Method: http.MethodGet, Path: "/private", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 1, Repeats: 2,
		StageDuration: 10 * time.Second, Budget: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "INSUFFICIENT_EVIDENCE" || result.FirstFailingJourneysPerSecond != nil ||
		len(result.Probes) != 1 || result.Probes[0].Status != "INSUFFICIENT_EVIDENCE" || len(result.Probes[0].Runs) != 1 {
		t.Fatalf("missing application authentication was treated as a capacity failure: result=%+v", result)
	}
}

func arrivalScenario(t *testing.T, maxRPS, workers int) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{
		Name: "local-arrival", BaseURL: "http://127.0.0.1:18080", Users: workers,
		Mode: "arrival", ArrivalRate: 1, Duration: "10s", MaxRPS: maxRPS,
		P95LimitMS: 1000, MaxErrorRate: 0.1, Steps: []config.Step{{Name: "read", Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func completeRun(v config.Validated, outcome string, rate int) report.Result {
	total := rate * 10
	successes := total
	if outcome == "FAIL" {
		successes = total - 1
	}
	metrics := report.MetricSet{Total: total, Success: successes, Failures: total - successes, P95MS: 10}
	return report.Result{
		SchemaVersion: 3, Outcome: outcome, ObservationSeconds: 10, ElapsedSeconds: 10,
		Config:  report.RunConfig{Mode: "arrival", Users: v.Scenario.Users, ArrivalRate: rate, MaxRPS: v.Scenario.MaxRPS, MinSamplesPerStep: 1},
		Metrics: metrics, Journeys: metrics,
		Steps: []report.StepResult{{Name: "read", Metrics: metrics}},
		Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.1,
			ProcessSamples: 4, ProcessCPUValid: true, PeakProcessCPUUtilizationRatio: 0.1, PeakProcessRSSBytes: 1024},
	}
}
