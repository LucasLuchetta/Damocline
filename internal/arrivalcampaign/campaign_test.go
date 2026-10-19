package arrivalcampaign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestRequiredStartsPerSecondUsesSuccessiveGrowthAndReserve(t *testing.T) {
	got, err := RequiredStartsPerSecond(5, 25, 20)
	if err != nil || got != 8 {
		t.Fatalf("RequiredStartsPerSecond() = %d, %v; want 8", got, err)
	}
	if _, err := RequiredStartsPerSecond(100, 25, 20); err == nil {
		t.Fatal("demand beyond the safe arrival ceiling was accepted")
	}
}

func TestWithRunnerKeepsArrivalScheduleAndAbortsWithinBudget(t *testing.T) {
	base := validatedArrival(t)
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	calls := 0
	parent, cancel := context.WithCancel(context.Background())
	result, err := WithRunner(parent, base, options, func(ctx context.Context, v config.Validated) report.Result {
		calls++
		if v.Scenario.Mode != "arrival" || v.Scenario.ArrivalRate != 1 || v.Scenario.Users != 2 {
			t.Fatalf("phase controls changed units or worker limit: %+v", v.Scenario)
		}
		cancel()
		<-ctx.Done()
		return report.Result{Outcome: "ABORTED"}
	})
	if err != nil || result.Outcome != "ABORTED" || calls != 1 || len(result.Trials) != 1 {
		t.Fatalf("campaign = outcome %s, calls %d, err %v", result.Outcome, calls, err)
	}
}

func TestWithRunnerRejectsCalculatedDemandMismatch(t *testing.T) {
	base := validatedArrival(t)
	_, err := WithRunner(context.Background(), base, Options{Observation: 10 * time.Second, Recovery: 10 * time.Second,
		Repeats: 2, PeakRate: 2, GrowthPercent: 25, ReservePercent: 20}, func(context.Context, config.Validated) report.Result {
		t.Fatal("runner called after invalid demand validation")
		return report.Result{}
	})
	if err == nil {
		t.Fatal("mismatched scenario rate and calculated target were accepted")
	}
}

func TestValidateRequiresBudgetToCoverBoundedPhaseDrains(t *testing.T) {
	base := validatedArrival(t)
	options := Options{Observation: 10*time.Second, Recovery: 10*time.Second, Repeats: 2, Budget: 50*time.Second}
	if err := Validate(base, options); err == nil { t.Fatal("explicit budget smaller than bounded phase drains was accepted") }
}

func TestWithRunnerPreservesTrialAndPhaseSchedule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	base, err := config.Parse(config.Scenario{
		Name: "arrival-campaign-loopback", BaseURL: server.URL, Users: 2, Mode: "arrival", ArrivalRate: 1,
		Duration: "10s", WindowDuration: "2s", MaxRPS: 10, Timeout: "2s", P95LimitMS: 5000, MaxErrorRate: .1,
		MinSamplesPerStep: 1, Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Warmup: 0, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	var seen []string
	result, err := WithRunner(context.Background(), base, options, func(ctx context.Context, v config.Validated) report.Result {
		seen = append(seen, v.Scenario.Duration+":"+string(rune('0'+v.Scenario.ArrivalRate)))
		return report.Result{Outcome: "PASS"}
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(result.Trials) != 1 || len(seen) != 1 {
		t.Fatalf("unexpected campaign result: outcome=%s trials=%d phases=%v err=%v", result.Outcome, len(result.Trials), seen, err)
	}
	if result.Trials[0].Phases[0].Name != "observation" {
		t.Fatalf("phase ordering changed: %+v", result.Trials[0].Phases)
	}
}

func TestWithRunnerCreatesWindowScheduleWhenBaseHasNone(t *testing.T) {
	base := validatedArrival(t)
	base.Window = 0
	base.Scenario.WindowDuration = ""
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	seen := 0
	_, err := WithRunner(context.Background(), base, options, func(_ context.Context, v config.Validated) report.Result {
		seen++
		if v.Window != 2500*time.Millisecond || v.Scenario.WindowDuration != "2.5s" {
			t.Fatalf("unexpected default window: %+v (%s)", v.Window, v.Scenario.WindowDuration)
		}
		return report.Result{Outcome: "INCONCLUSIVE"}
	})
	if err != nil || seen != 1 {
		t.Fatalf("runner calls=%d err=%v", seen, err)
	}
}

func validatedArrival(t *testing.T) config.Validated {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	base, err := config.Parse(config.Scenario{Name: "bounded", BaseURL: server.URL, Users: 2, Mode: "arrival", ArrivalRate: 1,
		Duration: "10s", WindowDuration: "2s", MaxRPS: 10, Timeout: "2s", P95LimitMS: 5000, MaxErrorRate: .1, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/", ExpectedStatus: []int{http.StatusOK}}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return base
}
