package campaign_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestClosedAndOpenCampaignsMeasureSpikeAndRecoveryAgainstControlledServer(t *testing.T) {
	t.Run("closed-loop", func(t *testing.T) {
		var saturated atomic.Bool
		var served atomic.Int64
		server := newSpikeRecoveryServer(&saturated, &served)
		defer server.Close()
		base := integrationScenario(t, server.URL, 1, 100, "closed", 0)
		base.Window = 1 * time.Second
		base.Scenario.WindowDuration = base.Window.String()
		stability := campaign.DefaultStabilityPolicy()
		stability.MaxP95IncreasePercent = 100
		stability.MaxP95IncreaseMS = 100
		result, err := campaign.WithRunner(context.Background(), base, campaign.Options{
			Observation: 10 * time.Second, SpikeUsers: 4, SpikeDuration: 10 * time.Second,
			Recovery: 10 * time.Second, Repeats: 2, Stability: stability,
		}, func(ctx context.Context, phase config.Validated) report.Result {
			saturated.Store(phase.Scenario.Users == 4)
			defer saturated.Store(false)
			return runWithPhaseLedger(t, ctx, phase, &served)
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Trials) == 1 && len(result.Trials[0].Phases) == 1 {
			phase := result.Trials[0].Phases[0]
			if stoppedByDriftScreen(t, result.Outcome, phase.Name, phase.Stability, phase.EvidenceState, phase.Run, 1, 0) {
				return
			}
		}
		if len(result.Trials) != 1 || len(result.Trials[0].Phases) != 3 {
			t.Fatalf("closed-loop campaign stopped before recovery: %+v", result)
		}
		phases := result.Trials[0].Phases
		assertNativePhase(t, phases[0].Run, 1, 0, "PASS")
		assertNativePhase(t, phases[1].Run, 4, 0, "FAIL")
		assertNativePhase(t, phases[2].Run, 1, 0, "PASS")
		assertObservedSpikeRecovery(t, result.Outcome, []phaseEvidence{
			{name: phases[0].Name, state: phases[0].EvidenceState, outcome: phases[0].Run.Outcome},
			{name: phases[1].Name, state: phases[1].EvidenceState, outcome: phases[1].Run.Outcome},
			{name: phases[2].Name, state: phases[2].EvidenceState, outcome: phases[2].Run.Outcome},
		})
	})

	t.Run("open-arrival", func(t *testing.T) {
		var saturated atomic.Bool
		var served atomic.Int64
		server := newSpikeRecoveryServer(&saturated, &served)
		defer server.Close()
		base := integrationScenario(t, server.URL, 30, 100, "arrival", 24)
		base.Window = 1 * time.Second
		base.Scenario.WindowDuration = base.Window.String()
		stability := campaign.DefaultStabilityPolicy()
		stability.MaxP95IncreasePercent = 100
		stability.MaxP95IncreaseMS = 100
		result, err := arrivalcampaign.WithRunner(context.Background(), base, arrivalcampaign.Options{
			Observation: 10 * time.Second, SpikeRate: 48, SpikeDuration: 10 * time.Second,
			Recovery: 10 * time.Second, Repeats: 2, Stability: stability,
		}, func(ctx context.Context, phase config.Validated) report.Result {
			saturated.Store(phase.Scenario.ArrivalRate == 48)
			defer saturated.Store(false)
			return runWithPhaseLedger(t, ctx, phase, &served)
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Trials) == 1 && len(result.Trials[0].Phases) == 1 {
			phase := result.Trials[0].Phases[0]
			if stoppedByDriftScreen(t, result.Outcome, phase.Name, phase.Stability, phase.EvidenceState, phase.Run, 30, 24) {
				return
			}
		}
		if len(result.Trials) != 1 || len(result.Trials[0].Phases) != 3 {
			t.Fatalf("arrival campaign stopped before recovery: %+v", result)
		}
		phases := result.Trials[0].Phases
		assertNativePhase(t, phases[0].Run, 30, 24, "PASS")
		assertNativePhase(t, phases[1].Run, 30, 48, "FAIL")
		assertNativePhase(t, phases[2].Run, 30, 24, "PASS")
		assertObservedSpikeRecovery(t, result.Outcome, []phaseEvidence{
			{name: phases[0].Name, state: phases[0].EvidenceState, outcome: phases[0].Run.Outcome},
			{name: phases[1].Name, state: phases[1].EvidenceState, outcome: phases[1].Run.Outcome},
			{name: phases[2].Name, state: phases[2].EvidenceState, outcome: phases[2].Run.Outcome},
		})
	})
}

func stoppedByDriftScreen(t *testing.T, outcome, phaseName, stability, evidenceState string, run report.Result, workers, scheduledRate int) bool {
	t.Helper()
	if phaseName != "observation" || stability != "NON_STATIONARY" {
		return false
	}
	if outcome != "INCONCLUSIVE" || evidenceState != "PASS_OBSERVED" {
		t.Fatalf("drift-limited campaign did not stop inconclusively after its passing observation: outcome=%s evidence=%s", outcome, evidenceState)
	}
	assertNativePhase(t, run, workers, scheduledRate, "PASS")
	t.Log("recovery was correctly unplanned after the observation failed the stationarity screen")
	return true
}

func runWithPhaseLedger(t *testing.T, ctx context.Context, phase config.Validated, served *atomic.Int64) report.Result {
	t.Helper()
	before := served.Load()
	run := engine.Run(ctx, phase)
	count := served.Load() - before
	if count == 0 || count != int64(run.Metrics.Total) || count != int64(run.Metrics.Success) ||
		count != run.IterationsCompleted || run.Metrics.Cancelled != 0 {
		t.Fatalf("phase ledger disagrees: server_200=%d client_requests=%d successes=%d cancelled=%d completed_iterations=%d",
			count, run.Metrics.Total, run.Metrics.Success, run.Metrics.Cancelled, run.IterationsCompleted)
	}
	t.Logf("phase ledger reconciled: mode=%s users=%d arrival_rate=%d server_200=%d outcome=%s",
		phase.Scenario.Mode, phase.Scenario.Users, phase.Scenario.ArrivalRate, count, run.Outcome)
	return run
}

func newSpikeRecoveryServer(saturated *atomic.Bool, served *atomic.Int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if saturated.Load() {
			time.Sleep(450 * time.Millisecond)
		} else {
			time.Sleep(30 * time.Millisecond)
		}
		served.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
}

func integrationScenario(t *testing.T, target string, users, maxRPS int, mode string, arrivalRate int) config.Validated {
	t.Helper()
	scenario := config.Scenario{
		Name: "controlled-spike-recovery", BaseURL: target, Users: users, Duration: "10s",
		Timeout: "2s", MaxRPS: maxRPS, P95LimitMS: 120, MaxErrorRate: 0.1,
		Mode: mode, ArrivalRate: arrivalRate, WindowDuration: "2.5s",
		Steps: []config.Step{{Name: "request", Method: http.MethodGet, Path: "/"}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return validated
}

func assertNativePhase(t *testing.T, run report.Result, workers, scheduledRate int, outcome string) {
	t.Helper()
	if run.Config.Users != workers || run.Metrics.Total <= 0 || run.IterationsStarted != run.IterationsCompleted ||
		run.ArrivalsDropped != 0 || run.Journeys.Cancelled != 0 || run.Journeys.Total != int(run.IterationsCompleted) ||
		run.Generator == nil || run.Generator.Samples < 2 || run.Generator.ProcessSamples < 2 || run.Outcome != outcome {
		t.Fatalf("phase did not reconcile planned/achieved work, SLO outcome and generator evidence: config=%+v requests=%d journeys=%+v iterations=%d/%d dropped=%d outcome=%s generator=%+v",
			run.Config, run.Metrics.Total, run.Journeys, run.IterationsStarted, run.IterationsCompleted, run.ArrivalsDropped, run.Outcome, run.Generator)
	}
	if outcome == "PASS" && (run.Metrics.P95MS > run.Config.P95LimitMS || run.Journeys.P95MS > run.Config.P95LimitMS) ||
		outcome == "FAIL" && run.Metrics.P95MS <= run.Config.P95LimitMS && run.Journeys.P95MS <= run.Config.P95LimitMS {
		t.Fatalf("phase SLO evidence does not support its %s result: request p95=%.1f journey p95=%.1f limit=%.1f", outcome, run.Metrics.P95MS, run.Journeys.P95MS, run.Config.P95LimitMS)
	}
	if scheduledRate == 0 {
		return
	}
	if run.Config.ArrivalRate != scheduledRate || run.ArrivalsScheduled != int64(scheduledRate*10) || run.ArrivalsAdmitted == nil ||
		*run.ArrivalsAdmitted != run.ArrivalsScheduled || run.IterationsStarted != run.ArrivalsScheduled {
		t.Fatalf("phase arrival schedule did not reconcile: rate=%d scheduled=%d admitted=%v started=%d",
			run.Config.ArrivalRate, run.ArrivalsScheduled, run.ArrivalsAdmitted, run.IterationsStarted)
	}
}

type phaseEvidence struct {
	name    string
	state   string
	outcome string
}

func TestNativeCampaignCancellationReconcilesServerReceipt(t *testing.T) {
	for _, mode := range []string{"closed", "arrival"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var received atomic.Int64
			handlerDone := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				received.Add(1)
				cancel()
				<-r.Context().Done()
				select {
				case handlerDone <- struct{}{}:
				default:
				}
			}))
			defer server.Close()

			rate := 0
			if mode == "arrival" {
				rate = 1
			}
			base := integrationScenario(t, server.URL, 1, 10, mode, rate)
			calls := 0
			runner := func(ctx context.Context, phase config.Validated) report.Result {
				calls++
				return engine.Run(ctx, phase)
			}
			var run report.Result
			var outcome, phaseName string
			switch mode {
			case "closed":
				result, err := campaign.WithRunner(ctx, base, campaign.Options{
					Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
				}, runner)
				if err != nil || len(result.Trials) != 1 || len(result.Trials[0].Phases) != 1 {
					t.Fatalf("expected one interrupted phase: %+v, %v", result, err)
				}
				phase := result.Trials[0].Phases[0]
				outcome, phaseName, run = result.Outcome, phase.Name, phase.Run
			case "arrival":
				result, err := arrivalcampaign.WithRunner(ctx, base, arrivalcampaign.Options{
					Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
				}, runner)
				if err != nil || len(result.Trials) != 1 || len(result.Trials[0].Phases) != 1 {
					t.Fatalf("expected one interrupted phase: %+v, %v", result, err)
				}
				phase := result.Trials[0].Phases[0]
				outcome, phaseName, run = result.Outcome, phase.Name, phase.Run
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("server did not observe the interrupted request")
			}
			if outcome != "ABORTED" || phaseName != "observation" || run.Outcome != "ABORTED" ||
				calls != 1 || received.Load() != 1 || run.Metrics.Cancelled != 1 ||
				run.Metrics.Success != 0 || run.IterationsStarted != 1 || run.IterationsCompleted != 0 {
				t.Fatalf("interruption ledgers disagree: campaign=%s phase=%s run=%s calls=%d received=%d requests=%+v iterations=%d/%d",
					outcome, phaseName, run.Outcome, calls, received.Load(), run.Metrics, run.IterationsStarted, run.IterationsCompleted)
			}
			if mode == "arrival" && (run.ArrivalsAdmitted == nil || *run.ArrivalsAdmitted != 1 ||
				run.ArrivalsScheduled != *run.ArrivalsAdmitted+run.ArrivalsDropped) {
				t.Fatalf("interrupted arrival schedule did not reconcile: %+v", run)
			}
		})
	}
}

func assertObservedSpikeRecovery(t *testing.T, outcome string, phases []phaseEvidence) {
	t.Helper()
	if outcome != "FAIL" || len(phases) != 3 {
		t.Fatalf("campaign did not finish the failed spike's recovery phase: outcome=%s phases=%+v", outcome, phases)
	}
	if phases[0].name != "observation" || phases[0].outcome != "PASS" ||
		phases[1].name != "spike" || phases[1].outcome != "FAIL" || phases[1].state != "FAIL_OBSERVED" ||
		phases[2].name != "recovery" || phases[2].outcome != "PASS" || phases[2].state != "PASS_OBSERVED" {
		t.Fatalf("controlled overload was not isolated to the spike and recovered at baseline: %+v", phases)
	}
}
