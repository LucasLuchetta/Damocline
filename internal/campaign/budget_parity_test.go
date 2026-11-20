package campaign

import (
	"strings"
	"testing"
	"time"
)

// A pilot campaign ran repeated (1m warm-up + 2m observation + 1m recovery) with
// 30s cooldowns under a 15m budget. The budget covered every phase but not
// the bounded drain allowance, so the offline audit rejected the evidence
// after the traffic had already run. Validate must reject it up front.
func TestValidateRejectsBudgetWithoutDrainAllowanceBeforeTraffic(t *testing.T) {
	options := Options{
		Warmup:      time.Minute,
		Observation: 2 * time.Minute,
		Recovery:    time.Minute,
		Cooldown:    30 * time.Second,
		Repeats:     2,
		Budget:      9 * time.Minute,
	}
	err := Validate(options)
	if err == nil || !strings.Contains(err.Error(), "drain allowance") {
		t.Fatalf("expected drain allowance rejection, got %v", err)
	}
	if !strings.Contains(err.Error(), defaultBudget(options).String()) {
		t.Fatalf("error should state the required budget %s: %v", defaultBudget(options), err)
	}
	options.Budget = defaultBudget(options)
	if err := Validate(options); err != nil {
		t.Fatalf("budget equal to the bounded default must be accepted: %v", err)
	}
	options.Budget = 0
	if err := Validate(options); err != nil {
		t.Fatalf("an omitted budget uses the bounded default: %v", err)
	}
}
