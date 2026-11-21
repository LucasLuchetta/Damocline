package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestCapacityWorstCaseProbesCountRefinement(t *testing.T) {
	// Standard: failing at 16 leaves 7 levels inside 8-16, three probes.
	if got := capacityWorstCaseProbes([]int{2, 4, 8, 16, 20}, nil, false); got != 7 {
		t.Fatalf("standard worst case %d", got)
	}
	// Extended: only calibrated levels inside a bracket can be probed.
	if got := capacityWorstCaseProbes([]int{2, 4, 8}, []int{2, 4, 8}, true); got != 3 {
		t.Fatalf("extended without fill-ins %d", got)
	}
	if got := capacityWorstCaseProbes([]int{2, 4, 8}, []int{2, 3, 4, 5, 6, 7, 8}, true); got != 5 {
		t.Fatalf("extended with fill-ins %d", got)
	}
}

func TestCapacityStageOverheadUsesEngineBounds(t *testing.T) {
	s := config.Scenario{Name: "fixture", BaseURL: "http://127.0.0.1:18089", Users: 2, Duration: "20s", Timeout: "4s",
		P95LimitMS: 750, MaxErrorRate: .01, MaxRPS: 50, BaselinePath: "/baseline",
		Steps: []config.Step{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}}}
	overhead, err := capacityStageOverhead(s, 2, nil, false)
	want := config.NetworkBaselineWallBudget + 8*time.Second + capacityStageSetupAllowance
	if err != nil || overhead != want {
		t.Fatalf("overhead %s, want %s (%v)", overhead, want, err)
	}
	s.BaselinePath, s.NetworkBaselineSkipReason = "", "synthetic-loopback-test"
	if overhead, err := capacityStageOverhead(s, 2, nil, false); err != nil || overhead != want-config.NetworkBaselineWallBudget {
		t.Fatalf("skipped baseline overhead %s (%v)", overhead, err)
	}
}

// When only the worst case (every bound at its cap, plus refinement) exceeds
// the explore cap, the plan proceeds with a warning: the explorer enforces
// its own budget and an early stop continues from the highest passing level.
func TestCapacityWarnsWhenOnlyWorstCaseExploreExceedsCap(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "128", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "WARNING: the worst-case explore plan") || !strings.Contains(out.String(), "continues from the highest repeatedly passing level") {
		t.Fatalf("worst-case-only overrun should warn and proceed: %d %s %s", code, &out, &errOut)
	}
}

// When even the expected plan cannot fit, refuse before traffic.
func TestCapacityRefusesExploreWhenExpectedPlanExceedsCap(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "500", "--calibration-level-duration", "3m", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("an expected plan above the cap must be refused: %s %s", &out, &errOut)
	}
}
