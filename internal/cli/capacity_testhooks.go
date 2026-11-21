package cli

import (
	"time"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

// SetCapacityStageFloorsForTest shortens the capacity protocol stage floors
// for bounded end-to-end tests in other packages and returns a restore
// function. The explore and campaign validators still apply. Production code
// must never call it.
func SetCapacityStageFloorsForTest(exploreStage, warmup, recovery time.Duration) func() {
	previous := capacityTiming
	capacityTiming.ExploreStage, capacityTiming.Warmup, capacityTiming.Recovery = exploreStage, warmup, recovery
	capacityTiming.SettleWindowMin = 2 * time.Second
	return func() { capacityTiming = previous }
}

// CapacityCalibrationLevels exposes the extended calibration level plan for
// end-to-end tests.
func CapacityCalibrationLevels(minUsers, maxUsers int, levelDuration time.Duration) []int {
	return capacityCalibrationLevels(explorer.Options{MinUsers: minUsers, MaxUsers: maxUsers}, levelDuration)
}
