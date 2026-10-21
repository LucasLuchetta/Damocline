package campaign

import (
	"context"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestObjectiveContractRejectsMissingCriteriaBeforeTraffic(t *testing.T) {
	base := scenario(t)
	base.Scenario.MaxErrorRate = 1
	calls := 0
	_, err := WithRunner(context.Background(), base, Options{Observation: 10 * time.Second, Repeats: 2}, func(context.Context, config.Validated) report.Result {
		calls++
		return report.Result{}
	})
	if err == nil || calls != 0 {
		t.Fatalf("campaign with an unbounded 100%% journey-error objective reached the runner: err=%v calls=%d", err, calls)
	}
}

func TestObjectiveContractIsPersistedAndReplayedAgainstCoverage(t *testing.T) {
	base := scenario(t)
	base.Window = 2500 * time.Millisecond
	base.Scenario.WindowDuration = base.Window.String()
	result, err := WithRunner(context.Background(), base, Options{Observation: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || result.Objectives == nil || result.Objectives.JourneyP95LimitMS != 1000 || result.Objectives.MaxJourneyErrorRate != 0 || result.Objectives.MinCompletedWorkRatio != 1 || result.Objectives.MaxDroppedArrivals != 0 {
		t.Fatalf("campaign did not retain its declared objective contract: result=%+v err=%v", result, err)
	}
	run := stableResult(base)
	state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation")
	if state != "PASS_OBSERVED" {
		t.Fatalf("valid objective coverage was rejected: %s", state)
	}
	run.IterationsStarted++
	if state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation"); state != "INVALID_DELIVERY" {
		t.Fatalf("incomplete started work met the 100%% completion objective: %s", state)
	}
	run = stableResult(base)
	run.ArrivalsDropped = 1
	if state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation"); state != "INVALID_DELIVERY" {
		t.Fatalf("dropped work met the zero-drop policy: %s", state)
	}
	run = stableResult(base)
	run.Windows[0].Journeys.Total = result.Objectives.MinimumSamplesPerWindow - 1
	if state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation"); state != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("under-sampled objective window was accepted: %s", state)
	}
	run = stableResult(base)
	run.Config.Profiles = []report.ProfileConfig{{Name: "read", Weight: 100}}
	for i := range run.Windows {
		run.Windows[i].Profiles = []report.WindowMetric{{Name: "read", Metrics: report.MetricSet{Total: 20, Success: 20}}}
	}
	run.Windows[0].Profiles[0].Metrics.Total--
	if state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation"); state != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("under-sampled configured workflow was accepted: %s", state)
	}
	run = stableResult(base)
	run.Config.Profiles = []report.ProfileConfig{{Name: "read", Weight: 100}}
	run.Profiles = []report.ProfileResult{{Name: "read", Journeys: report.MetricSet{Total: 80, Success: 80, P95MS: 100}}}
	for i := range run.Windows {
		run.Windows[i].Profiles = []report.WindowMetric{{Name: "read", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}}}
	}
	run.Profiles[0].Journeys.P95MS = result.Objectives.JourneyP95LimitMS + 1
	if state, _ := ClassifyEvidenceWithObjectives(run, *result.Objectives, "observation"); state != "FAIL_OBSERVED" {
		t.Fatalf("workflow-level p95 breach was hidden by aggregate SLO evidence: %s", state)
	}
}

func TestObjectiveContractMatchesPhaseCappedFixedWindows(t *testing.T) {
	base := scenario(t)
	base.Window = 20 * time.Second
	base.Scenario.WindowDuration = base.Window.String()
	policy := DefaultStabilityPolicy()
	contract := NewObjectiveContract(base, policy)
	run := stableResult(base)
	run.Config.Duration = "10s"
	run.Config.WindowDuration = "10s"
	if !contract.MatchesRun(run) {
		t.Fatal("phase-capped window was not matched to the predeclared fixed window")
	}
	run.Config.WindowDuration = "5s"
	if contract.MatchesRun(run) {
		t.Fatal("phase window shorter than the declared fixed window was accepted")
	}
}
