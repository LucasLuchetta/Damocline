package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCapacityWarmupIsSizedFromMeasuredJourneyCycle(t *testing.T) {
	run := report.Result{ElapsedSeconds: 20, Journeys: report.MetricSet{Total: 200}} // 100 VUs: 10s per-VU cycle
	result := explorer.Result{Probes: []explorer.Probe{{Users: 100, Status: "PASS", Runs: []report.Result{run, run}}}}
	warmup, floor, cycle, policy := capacityWarmup(result, 100, 5*time.Second, 33*time.Second)
	if cycle != 10 || policy.WindowSeconds != 11 || floor != 33*time.Second {
		t.Fatalf("cycle=%v policy=%+v floor=%s", cycle, policy, floor)
	}
	// ramp 5s + 3 x 10s = 35s, rounded up to whole 11s windows.
	if warmup != 44*time.Second || campaign.ValidateWarmupSettlingPolicy(policy, warmup) != nil {
		t.Fatalf("warmup=%s", warmup)
	}
	slow := report.Result{ElapsedSeconds: 20, Journeys: report.MetricSet{Total: 10}}
	result.Probes[0].Runs = []report.Result{slow, slow}
	if warmup, _, _, _ := capacityWarmup(result, 100, 0, 33*time.Second); warmup != 88*time.Second {
		t.Fatalf("warm-up must be capped below 90s in whole windows: %s", warmup)
	}
	if warmup, floor, cycle, _ := capacityWarmup(explorer.Result{}, 10, 0, 6*time.Second); cycle != 0 || warmup != floor || warmup != 15*time.Second {
		t.Fatalf("unmeasured cycle must use the floor: %s %s %v", warmup, floor, cycle)
	}
}

func TestWarmupSettlingSentencesExplainNonStationaryWarmup(t *testing.T) {
	policy := campaign.DefaultWarmupSettlingPolicy(5 * time.Second)
	settled := campaign.SettlingAssessment{State: campaign.SettlingStabilized, StabilizedAfterSeconds: new(float64), Windows: []campaign.SettlingWindow{{RequestP95MS: 900}, {RequestP95MS: 120}}}
	*settled.StabilizedAfterSeconds = 5
	unsettled := campaign.SettlingAssessment{State: campaign.SettlingNonStationary, Reason: "still changing", Windows: []campaign.SettlingWindow{{RequestP95MS: 100}, {RequestP95MS: 300}}}
	evidence := campaign.Result{Warmup: "30s", WarmupSettling: &policy, Trials: []campaign.Trial{
		{Number: 1, Phases: []campaign.Phase{{Name: "warmup", Settling: &settled}}},
		{Number: 2, Phases: []campaign.Phase{{Name: "warmup", Settling: &unsettled}}},
	}}
	text := strings.Join(warmupSettlingSentences(evidence), "\n")
	for _, want := range []string{"settled after about 5s", "900ms, 120ms", "did NOT settle", "INCONCLUSIVE", "not a pass", "every observation window still is"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if warmupSettlingSentences(campaign.Result{}) != nil {
		t.Fatal("campaigns without settling must not report it")
	}
	if line := warmupSettlingPlanLine(campaign.Options{Warmup: 30 * time.Second, WarmupSettling: policy}); !strings.Contains(line, "5s windows") || !strings.Contains(line, "INCONCLUSIVE") {
		t.Fatal(line)
	}
}
