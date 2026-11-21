package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCapacityBisectionConvergesToHighestSustainedLevel(t *testing.T) {
	levels := []int{150, 100, 50, 25}
	for name, tc := range map[string]struct {
		sustainable int
		want        []int
	}{
		"150 sustains": {150, []int{100, 150}},
		"100 sustains": {100, []int{100, 150}},
		"50 sustains":  {50, []int{100, 50}},
		"25 sustains":  {25, []int{100, 50, 25}},
		"none":         {0, []int{100, 50, 25}},
	} {
		b := newCapacityBisection(levels)
		var tried []int
		for i := 0; i < capacityMaxStepDowns; i++ {
			level, ok := b.next()
			if !ok {
				break
			}
			tried = append(tried, level)
			b.record(level, level <= tc.sustainable)
		}
		if !reflect.DeepEqual(tried, tc.want) {
			t.Errorf("%s: tried %v, want %v", name, tried, tc.want)
		}
		if _, more := b.next(); more {
			t.Errorf("%s: bisection did not converge within %d campaigns", name, capacityMaxStepDowns)
		}
	}
	if first := capacityFirstStepDown(levels); first != 100 {
		t.Fatalf("first step-down = %d", first)
	}
}

func recoveryExplore(p50 ...float64) explorer.Result {
	var runs []report.Result
	for _, v := range p50 {
		runs = append(runs, report.Result{NetworkBaseline: &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Completed: 20, P50MS: v}})
	}
	return explorer.Result{Probes: []explorer.Probe{
		{Users: 50, Runs: []report.Result{{NetworkBaseline: &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Completed: 20, P50MS: 400}}}},
		{Users: 25, Runs: runs},
	}}
}

func withRecoveryFakes(t *testing.T, probes []float64) *int {
	t.Helper()
	calls := 0
	oldProbe, oldSleep := capacityRecoveryProbe, capacityRecoverySleep
	capacityRecoveryProbe = func(context.Context, *config.NetworkBaselinePlan) *report.NetworkBaseline {
		v := probes[min(calls, len(probes)-1)]
		calls++
		return &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Completed: 20, P50MS: v}
	}
	capacityRecoverySleep = func(context.Context, time.Duration) bool { return true }
	t.Cleanup(func() { capacityRecoveryProbe, capacityRecoverySleep = oldProbe, oldSleep })
	return &calls
}

func TestCapacityRecoveryReferenceUsesLowestLevelMedian(t *testing.T) {
	if got := capacityRecoveryReference(recoveryExplore(12, 10, 30)); got != 12 {
		t.Fatalf("reference = %v, want 12", got)
	}
	if got := capacityRecoveryReference(explorer.Result{}); got != 0 {
		t.Fatalf("empty reference = %v", got)
	}
}

func TestCapacityRecoveryGateWaitsForBaseline(t *testing.T) {
	plan := &config.NetworkBaselinePlan{Requests: 20, WallBudget: 30 * time.Second}
	calls := withRecoveryFakes(t, []float64{300, 120, 40})
	check := capacityRecoveryGate(context.Background(), plan, 10)
	if check.Status != capacityRecoveryRecovered || *calls != 3 || check.LimitP50MS != 45 || check.Waited != "1m0s" {
		t.Fatalf("unexpected recovery: %+v calls=%d", check, *calls)
	}
}

func TestCapacityRecoveryGateStopsWhenTargetStaysSlow(t *testing.T) {
	plan := &config.NetworkBaselinePlan{Requests: 20, WallBudget: 30 * time.Second}
	calls := withRecoveryFakes(t, []float64{900})
	check := capacityRecoveryGate(context.Background(), plan, 10)
	if check.Status != capacityRecoveryNotRecovered || !strings.Contains(check.Reason, "later campaigns would be contaminated") {
		t.Fatalf("unexpected gate: %+v", check)
	}
	if *calls < 2 || *calls > 10 {
		t.Fatalf("gate is not bounded: %d probes", *calls)
	}
}

func TestCapacityRecoveryGateWithoutReferenceIsUnverified(t *testing.T) {
	calls := withRecoveryFakes(t, []float64{1})
	check := capacityRecoveryGate(context.Background(), &config.NetworkBaselinePlan{SkipReason: "no_get"}, 10)
	if check.Status != capacityRecoveryUnverified || *calls != 0 {
		t.Fatalf("unexpected gate: %+v calls=%d", check, *calls)
	}
}

func TestCapacityLadderReportsRecoveryStopAndTimeDependentKnee(t *testing.T) {
	ladder := capacityLadder{SchemaVersion: 1, ExploreStage: "20s", MaxStepDowns: capacityMaxStepDowns,
		StopReason: "target did not recover; later campaigns would be contaminated",
		Entries: []capacityLadderEntry{
			{Users: 175, Campaign: "campaign.json", Outcome: "FAIL"},
			{Users: 100, Outcome: capacityOutcomeNotRecovered, Reason: "target did not recover",
				Recovery: &capacityRecoveryCheck{Status: capacityRecoveryNotRecovered, Waited: "3m0s"}},
		}}
	finishCapacityLadder(&ladder)
	md := renderCapacityLadderMarkdown(ladder)
	for _, want := range []string{"Time-dependent knee: 175 VUs passed 20s explore stages", "falls behind when the load continues", "The ladder stopped early: target did not recover", capacityOutcomeNotRecovered} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "100 VUs passed the short explore stages but FAILED") {
		t.Errorf("unrecovered level reported as FAIL:\n%s", md)
	}
}

func TestCapacityLadderPicksHighestPassAfterBisection(t *testing.T) {
	ladder := capacityLadder{SchemaVersion: 1, Entries: []capacityLadderEntry{
		{Users: 175, Campaign: "campaign.json", Outcome: "FAIL"},
		{Users: 100, Campaign: "campaign-stepdown-100.json", Outcome: "PASS"},
		{Users: 150, Campaign: "campaign-stepdown-150.json", Outcome: "PASS"},
	}}
	finishCapacityLadder(&ladder)
	if ladder.SustainableUsers == nil || *ladder.SustainableUsers != 150 || ladder.AuditedCampaign != "campaign-stepdown-150.json" {
		t.Fatalf("unexpected selection: %+v", ladder)
	}
}
