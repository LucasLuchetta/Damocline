package campaign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func setWindowTotal(window *report.WindowResult, total int, p95 float64) {
	window.Metrics = report.MetricSet{Total: total, Success: total, P95MS: p95}
	window.Journeys = report.MetricSet{Total: total, Success: total, P95MS: p95}
	window.Steps = []report.WindowMetric{{Name: "page", Metrics: report.MetricSet{Total: total, Success: total, P95MS: p95}}}
}

func TestCampaignAppliesScenarioRampOnlyToFirstPhaseAndSpike(t *testing.T) {
	v := scenario(t)
	v.Scenario.RampUp = "2s"
	v.RampUp = 2 * time.Second
	var ramps []time.Duration
	var configured []string
	result, err := WithRunner(context.Background(), v, Options{
		Warmup: 10 * time.Second, Stabilization: 10 * time.Second, Observation: 10 * time.Second,
		SpikeUsers: 2, SpikeDuration: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
	}, func(_ context.Context, phase config.Validated) report.Result {
		ramps = append(ramps, phase.RampUp)
		configured = append(configured, phase.Scenario.RampUp)
		run := stableResult(phase)
		run.Config.RampUp = phase.Scenario.RampUp
		return run
	})
	if err != nil || result.Outcome != "PASS" || result.SchemaVersion != 17 || result.DriftBaseline != DriftBaselineSkipLightLeadingWindow {
		t.Fatalf("campaign failed: %+v %v", result, err)
	}
	want := []time.Duration{2 * time.Second, 0, 0, 2 * time.Second, 0}
	if len(ramps) != 10 {
		t.Fatalf("unexpected phase count: %v", ramps)
	}
	for i, ramp := range ramps {
		if ramp != want[i%5] {
			t.Fatalf("phase %d ramp = %s, want %s (all %v)", i, ramp, want[i%5], ramps)
		}
		if parsed, err := time.ParseDuration(configured[i]); err != nil || parsed != ramp {
			t.Fatalf("phase %d scenario ramp %q disagrees with %s", i, configured[i], ramp)
		}
		phase := result.Trials[i/5].Phases[i%5]
		if phase.RampUp != ramp.String() {
			t.Fatalf("phase %s recorded ramp %q, want %s", phase.Name, phase.RampUp, ramp)
		}
	}
	if EffectivePhaseRampUp(0, "observation", time.Second) != time.Second {
		t.Fatal("a trial without warm-up or stabilization must ramp its observation")
	}
}

func TestDriftBaselineExcludesOnlyALightLeadingWindow(t *testing.T) {
	policy := DefaultStabilityPolicy()
	run := stableResult(scenario(t))
	// A ramp-biased leading window: about 16% fewer requests and lower p95,
	// followed by uniform steady windows.
	setWindowTotal(&run.Windows[0], 21, 66)
	for i := 1; i < 4; i++ {
		setWindowTotal(&run.Windows[i], 30, 85)
	}
	if got := AssessStabilityWithPolicy(run, policy); got != "NON_STATIONARY" {
		t.Fatalf("historical first-window rule changed: %s", got)
	}
	if got, reference := AssessStabilityWithBaseline(run, policy, DriftBaselineSkipLightLeadingWindow); got != "NO_LARGE_DRIFT_DETECTED" || reference != 1 {
		t.Fatalf("light leading window remained the reference: %s %d", got, reference)
	}

	degrading := stableResult(scenario(t))
	for i, total := range []int{40, 36, 30, 24} {
		setWindowTotal(&degrading.Windows[i], total, 100+float64(i)*20)
	}
	if got, reference := AssessStabilityWithBaseline(degrading, policy, DriftBaselineSkipLightLeadingWindow); got != "NON_STATIONARY" || reference != 0 {
		t.Fatalf("degrading target was not flagged: %s %d", got, reference)
	}

	lateDegrading := stableResult(scenario(t))
	setWindowTotal(&lateDegrading.Windows[0], 21, 66)
	setWindowTotal(&lateDegrading.Windows[1], 30, 85)
	setWindowTotal(&lateDegrading.Windows[2], 30, 85)
	setWindowTotal(&lateDegrading.Windows[3], 30, 140)
	if got, _ := AssessStabilityWithBaseline(lateDegrading, policy, DriftBaselineSkipLightLeadingWindow); got != "NON_STATIONARY" {
		t.Fatalf("degradation after an excluded light window was hidden: %s", got)
	}

	tooFew := stableResult(scenario(t))
	tooFew.Windows = tooFew.Windows[:3]
	tooFew.ObservationSeconds = tooFew.Windows[2].EndSeconds
	setWindowTotal(&tooFew.Windows[0], 21, 66)
	setWindowTotal(&tooFew.Windows[1], 30, 85)
	setWindowTotal(&tooFew.Windows[2], 30, 85)
	if got, _ := AssessStabilityWithBaseline(tooFew, policy, DriftBaselineSkipLightLeadingWindow); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("excluding the reference must still leave the minimum windows: %s", got)
	}
	if got, _ := AssessStabilityWithBaseline(run, policy, "unknown"); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("unknown baseline rule was accepted: %s", got)
	}
	if (Result{SchemaVersion: 16}).ExpectedDriftBaseline() != DriftBaselineFirstWindow || (Result{SchemaVersion: 17}).ExpectedDriftBaseline() != DriftBaselineSkipLightLeadingWindow {
		t.Fatal("drift baseline rule is not tied to the campaign schema")
	}
}

func TestCampaignRecordsDriftBaselineWindowAndFlagsDegradation(t *testing.T) {
	light := func(_ context.Context, v config.Validated) report.Result {
		run := stableResult(v)
		setWindowTotal(&run.Windows[0], 21, 66)
		for i := 1; i < 4; i++ {
			setWindowTotal(&run.Windows[i], 30, 85)
		}
		return run
	}
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, light)
	if err != nil || result.Outcome != "PASS" || result.Trials[0].Phases[0].StabilityBaselineWindow != 1 || result.Trials[0].Phases[1].StabilityBaselineWindow != 0 {
		t.Fatalf("stable target with a light leading window was not accepted: %+v %v", result, err)
	}
	result, err = WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		run := stableResult(v)
		for i, total := range []int{40, 36, 30, 24} {
			setWindowTotal(&run.Windows[i], total, 100+float64(i)*20)
		}
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || result.Trials[0].Phases[0].Stability != "NON_STATIONARY" {
		t.Fatalf("degrading target was not flagged: %+v %v", result, err)
	}
}

func TestNativeCampaignObservationAfterWarmupHasNoRampWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "bounded local ramp campaign", BaseURL: server.URL, Users: 2, Duration: "10s", RampUp: "5s",
		MaxRPS: 100, P95LimitMS: 1000, MaxErrorRate: 0.1, MinSamplesPerStep: 20, WindowDuration: "2.5s",
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{204}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	stability := DefaultStabilityPolicy()
	stability.MaxP95IncreasePercent = 100
	stability.MaxP95IncreaseMS = 50
	result, err := Run(context.Background(), v, Options{Warmup: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2, Stability: stability})
	if err != nil || len(result.Trials) == 0 {
		t.Fatalf("campaign failed: %+v %v", result, err)
	}
	for _, trial := range result.Trials {
		if len(trial.Phases) < 2 || trial.Phases[0].RampUp != "5s" || trial.Phases[1].RampUp != "0s" || trial.Phases[1].Run.Config.RampUp != "0s" {
			t.Fatalf("phase ramps were not recorded: %+v", trial.Phases)
		}
		windows := trial.Phases[1].Run.Windows
		if len(windows) != 4 {
			t.Fatalf("unexpected observation windows: %+v", windows)
		}
		rates := make([]float64, 0, 3)
		for _, window := range windows[1:] {
			rates = append(rates, float64(window.Metrics.Total))
		}
		sort.Float64s(rates)
		// With the scenario ramp re-applied, the first 2.5s window would hold
		// roughly half of each later window's requests. At steady load it is
		// within ordinary scheduling noise of the others.
		if float64(windows[0].Metrics.Total) < 0.8*rates[1] {
			t.Fatalf("observation started with a light ramp window: %d vs median %.0f", windows[0].Metrics.Total, rates[1])
		}
		if trial.Phases[1].StabilityBaselineWindow != 0 {
			t.Fatalf("a steady observation excluded its leading window: %+v", windows)
		}
	}
}
