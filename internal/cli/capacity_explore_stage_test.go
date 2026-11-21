package cli

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/insights"
)

// Explore stages must be long enough for the lowest level to reach the
// per-step sample floor; a fixed 20s stage gave a think-time workload only
// 6 samples per step and the explore reported no passing level.
func TestCapacityExploreStageReachesSampleFloor(t *testing.T) {
	reference := filepath.Join("..", "insights", "testdata", "reports", "schema-v39.json")
	recorded, err := insights.ReadRun(reference)
	if err != nil || recorded.ElapsedSeconds <= 0 || recorded.Metrics.Total <= 0 {
		t.Skipf("reference fixture has no usable rate: %v", err)
	}
	s := config.Scenario{MinSamplesPerStep: 10, RampUp: "10s", Steps: make([]config.Step, 5)}
	stage, ok := capacityExploreStage(s, 2, reference)
	if !ok {
		t.Fatal("expected a sized stage")
	}
	perStep := float64(recorded.Metrics.Total) / recorded.ElapsedSeconds / float64(recorded.Config.Users) * 2 / 5
	if stage < 3*time.Minute && (stage-10*time.Second).Seconds()*perStep < 10 {
		t.Fatalf("stage %s collects only %.1f samples per step", stage, (stage-10*time.Second).Seconds()*perStep)
	}
	if stage < 20*time.Second || stage > 3*time.Minute {
		t.Fatalf("stage %s outside bounds", stage)
	}
	if _, ok := capacityExploreStage(s, 2, ""); ok {
		t.Fatal("no reference must keep the default")
	}
}

func TestCapacityExploreStageAccountsForPlannedRateCeiling(t *testing.T) {
	reference := filepath.Join("..", "insights", "testdata", "reports", "schema-v39.json")
	scenario := config.Scenario{MinSamplesPerStep: 40, MaxRPS: 1, Steps: make([]config.Step, 1)}
	stage, ok := capacityExploreStage(scenario, 20, reference)
	if !ok || stage != 50*time.Second {
		t.Fatalf("stage = %s, available = %v; want 50s including headroom at 1 request/s", stage, ok)
	}
}
