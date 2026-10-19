package campaign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func scenario(t *testing.T) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{Name: "local", BaseURL: "http://127.0.0.1:8080", Users: 1, Duration: "10s", MaxRPS: 10, P95LimitMS: 1000, Steps: []config.Step{{Name: "page", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCampaignSeparatesWarmupObservationAndRecovery(t *testing.T) {
	var durations []time.Duration
	result, err := WithRunner(context.Background(), scenario(t), Options{Warmup: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		durations = append(durations, v.Duration)
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(result.Trials) != 2 || len(durations) != 6 || result.ObservationVariability == nil || result.ObservationVariability.Runs != 2 || result.ObservationVariability.SuccessfulJourneysMinPerSec != 8 || result.ObservationVariability.SuccessfulJourneysMaxPerSec != 8 {
		t.Fatalf("unexpected campaign: %+v err=%v durations=%v", result, err, durations)
	}
}

func TestCampaignRunsStableSoakOnceAfterIndependentTrials(t *testing.T) {
	var phases []string
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		phases = append(phases, v.Duration.String())
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(phases) != 5 || len(result.Trials[1].Phases) != 3 || result.Trials[1].Phases[2].Name != "soak" || result.Trials[1].Phases[2].Stability != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("expected one final stable soak after four repeated phases (durations %v): outcome=%s err=%v", phases, result.Outcome, err)
	}
}

func TestCampaignSkipsSoakWhenAnyRepeatedTrialFails(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if calls == 3 { // the first recovery phase follows one passing observation
			return report.Result{Outcome: "FAIL", Config: report.RunConfig{Users: 1, Duration: v.Duration.String()}, Target: "http://127.0.0.1:8080"}
		}
		return stableResult(v)
	})
	if err != nil || result.Outcome != "FAIL" || len(result.Trials) != 2 || calls != 3 || len(result.Trials[1].Phases) != 1 || result.Trials[1].Phases[0].Name != "observation" {
		t.Fatalf("soak should not run after an earlier required phase fails: outcome=%s trials=%d calls=%d err=%v", result.Outcome, len(result.Trials), calls, err)
	}
}

func TestCampaignSoakDriftPreventsPassing(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		run := stableResult(v)
		if calls == 5 { // final soak follows two passing observation/recovery pairs
			run.Windows[3].Metrics.P95MS = 140
			run.Windows[3].Journeys.P95MS = 140
		}
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(result.Trials) != 2 || result.Trials[1].Phases[2].Name != "soak" || result.Trials[1].Phases[2].Stability != "NON_STATIONARY" {
		t.Fatalf("soak latency drift did not prevent a passing outcome: outcome=%s trials=%+v err=%v", result.Outcome, result.Trials, err)
	}
}

func TestCampaignSoakMustFitBoundedBudget(t *testing.T) {
	for _, soak := range []time.Duration{time.Second, 4 * time.Minute} {
		if err := Validate(Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: soak, Repeats: 2}); err == nil {
			t.Fatalf("unsafe soak duration accepted: %s", soak)
		}
	}
}

func TestCampaignFailsObservationAndStops(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if v.Duration == 10*time.Second && calls == 1 {
			return report.Result{Outcome: "FAIL"}
		}
		return report.Result{Outcome: "PASS"}
	})
	if err != nil || result.Outcome != "FAIL" || len(result.Trials) != 1 || calls != 1 {
		t.Fatalf("failed observation was not terminal: %+v err=%v calls=%d", result, err, calls)
	}
}

func TestCampaignRunsBoundedSpikeBeforeRecovery(t *testing.T) {
	var users []int
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, SpikeUsers: 3, SpikeDuration: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		users = append(users, v.Scenario.Users)
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(users) != 6 || users[1] != 3 || users[4] != 3 {
		t.Fatalf("spike was not isolated: %+v err=%v users=%v", result, err, users)
	}
}

func TestDefaultCampaignBudgetIncludesBoundedDrain(t *testing.T) {
	options := Options{Warmup: 10 * time.Second, Observation: 20 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	if got := PlannedBudget(options); got != 4*time.Minute+20*time.Second {
		t.Fatalf("default wall-time cap omitted phase drains: %s", got)
	}
	result, err := WithRunner(context.Background(), scenario(t), options, func(_ context.Context, v config.Validated) report.Result {
		return stableResult(v)
	})
	if err != nil || result.Budget != "4m20s" || result.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("effective cap or schema missing: %+v %v", result, err)
	}
	options.Observation = 7 * time.Minute
	if err := Validate(options); err == nil {
		t.Fatal("default cap above 15m accepted")
	}
}

func TestSoakBudgetIncludesOneFinalPhaseDrain(t *testing.T) {
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 30 * time.Second, Repeats: 2}
	if got, want := PlannedBudget(options), 3*time.Minute+40*time.Second; got != want {
		t.Fatalf("final soak drain missing from wall-time cap: got %s want %s", got, want)
	}
	options.Budget = 2*time.Minute + 30*time.Second
	if err := Validate(options); err == nil {
		t.Fatal("explicit budget without final soak drain was accepted")
	}
}

func stableResult(v config.Validated) report.Result {
	run := report.Result{Outcome: "PASS", ElapsedSeconds: v.Duration.Seconds(), ObservationSeconds: v.Duration.Seconds(), Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100}, Journeys: report.MetricSet{Total: 80, Success: 80, P95MS: 100}, Config: report.RunConfig{Users: v.Scenario.Users, Duration: v.Duration.String(), WindowDuration: v.Window.String()}, Target: v.Base.Scheme + "://" + v.Base.Host}
	for i := 0; i < 4; i++ {
		run.Windows = append(run.Windows, report.WindowResult{StartSeconds: float64(i) * v.Duration.Seconds() / 4, EndSeconds: float64(i+1) * v.Duration.Seconds() / 4, Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 100}})
	}
	return run
}

func TestCampaignRejectsDriftingObservation(t *testing.T) {
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		run := stableResult(v)
		run.Windows[3].Metrics.P95MS = 140
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || result.Trials[0].Phases[0].Stability != "NON_STATIONARY" {
		t.Fatalf("latency drift was not reported: %+v %v", result, err)
	}
}

func TestStabilityScreenDetectsThroughputLossAndMissingCoverage(t *testing.T) {
	run := stableResult(scenario(t))
	if got := AssessStability(run); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("stable fixture: %s", got)
	}
	run.Windows[3].Metrics = report.MetricSet{Total: 20, Success: 20, P95MS: 100}
	run.Windows[0].Metrics = report.MetricSet{Total: 40, Success: 40, P95MS: 100}
	if got := AssessStability(run); got != "NON_STATIONARY" {
		t.Fatalf("throughput loss: %s", got)
	}
	run.Windows[3].StartSeconds += 0.5
	if got := AssessStability(run); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("gapped windows: %s", got)
	}
}

func TestNativeEngineCampaignRecordsStableLoopbackWindows(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(20 * time.Millisecond)
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "bounded local campaign", BaseURL: server.URL, Users: 1, Duration: "10s", MaxRPS: 100,
		P95LimitMS: 1000, MaxErrorRate: 0.1, MinSamplesPerStep: 20,
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{204}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), v, Options{Observation: 10 * time.Second, Repeats: 2})
	if err != nil || result.Outcome != "PASS" || len(result.Trials) != 2 || received.Load() < 200 {
		t.Fatalf("bounded local campaign failed: outcome=%s trials=%d received=%d err=%v", result.Outcome, len(result.Trials), received.Load(), err)
	}
	for _, trial := range result.Trials {
		if len(trial.Phases) != 1 || trial.Phases[0].Stability != "NO_LARGE_DRIFT_DETECTED" || trial.Phases[0].EvidenceState != "PASS_OBSERVED" || len(trial.Phases[0].Run.Windows) != 4 {
			t.Fatalf("missing real engine window evidence: %+v", trial.Phases)
		}
	}
	run := result.Trials[0].Phases[0].Run
	run.Generator.MeanProcessCPUUtilizationRatio = report.GeneratorUtilizationGuard
	if state, reason := ClassifyEvidence(run); state != "INVALID_DELIVERY" || reason == "" {
		t.Fatalf("generator-bound native phase was accepted: %s %q", state, reason)
	}
	run.Generator.MeanProcessCPUUtilizationRatio = 0.1
	run.Generator.PeakProcessCPUUtilizationRatio = 1.1
	if state, reason := ClassifyEvidence(run); state != "PASS_OBSERVED" {
		t.Fatalf("transient process CPU peak invalidated native evidence: %s %q", state, reason)
	}
	run.Metrics.Total = int(report.GeneratorUtilizationGuard * float64(run.Config.MaxRPS) * run.WindowSeconds())
	if state, reason := ClassifyEvidence(run); state != "INVALID_DELIVERY" || reason == "" {
		t.Fatalf("client-rate-bound native phase was accepted: %s %q", state, reason)
	}
}

func TestCampaignSeparatesInvalidDeliveryFromObservedFailure(t *testing.T) {
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	result, err := WithRunner(context.Background(), scenario(t), options, func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "FAIL", ArrivalsDropped: 1}
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(result.Trials) != 1 || result.Trials[0].Phases[0].EvidenceState != "INVALID_DELIVERY" {
		t.Fatalf("lost arrival was interpreted as target failure: %+v %v", result, err)
	}
	if state, _ := ClassifyEvidence(report.Result{Outcome: "FAIL"}); state != "FAIL_OBSERVED" {
		t.Fatalf("ordinary observed failure classified as %s", state)
	}
}

func TestCampaignRecordsInterruptionReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := WithRunner(ctx, scenario(t), Options{Observation: 10 * time.Second, Repeats: 2}, func(context.Context, config.Validated) report.Result {
		t.Fatal("cancelled campaign started a phase")
		return report.Result{}
	})
	if err != nil || result.Outcome != "ABORTED" || len(result.Trials) != 1 || result.Trials[0].AbortReason != "The campaign was interrupted by its caller." {
		t.Fatalf("interruption reason missing: %+v %v", result, err)
	}
}
