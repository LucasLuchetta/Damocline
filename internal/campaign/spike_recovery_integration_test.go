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
	var saturated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if saturated.Load() {
			time.Sleep(450 * time.Millisecond)
		} else {
			time.Sleep(30 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Run("closed-loop", func(t *testing.T) {
		base := integrationScenario(t, server.URL, 1, 100, "closed", 0)
		result, err := campaign.WithRunner(context.Background(), base, campaign.Options{
			Observation: 10 * time.Second, SpikeUsers: 4, SpikeDuration: 10 * time.Second,
			Recovery: 10 * time.Second, Repeats: 2,
		}, func(ctx context.Context, phase config.Validated) report.Result {
			saturated.Store(phase.Scenario.Users == 4)
			defer saturated.Store(false)
			return engine.Run(ctx, phase)
		})
		if err != nil {
			t.Fatal(err)
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
		base := integrationScenario(t, server.URL, 30, 100, "arrival", 12)
		result, err := arrivalcampaign.WithRunner(context.Background(), base, arrivalcampaign.Options{
			Observation: 10 * time.Second, SpikeRate: 24, SpikeDuration: 10 * time.Second,
			Recovery: 10 * time.Second, Repeats: 2,
		}, func(ctx context.Context, phase config.Validated) report.Result {
			saturated.Store(phase.Scenario.ArrivalRate == 24)
			defer saturated.Store(false)
			return engine.Run(ctx, phase)
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Trials) != 1 || len(result.Trials[0].Phases) != 3 {
			t.Fatalf("arrival campaign stopped before recovery: %+v", result)
		}
		phases := result.Trials[0].Phases
		assertNativePhase(t, phases[0].Run, 30, 12, "PASS")
		assertNativePhase(t, phases[1].Run, 30, 24, "FAIL")
		assertNativePhase(t, phases[2].Run, 30, 12, "PASS")
		assertObservedSpikeRecovery(t, result.Outcome, []phaseEvidence{
			{name: phases[0].Name, state: phases[0].EvidenceState, outcome: phases[0].Run.Outcome},
			{name: phases[1].Name, state: phases[1].EvidenceState, outcome: phases[1].Run.Outcome},
			{name: phases[2].Name, state: phases[2].EvidenceState, outcome: phases[2].Run.Outcome},
		})
	})
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
