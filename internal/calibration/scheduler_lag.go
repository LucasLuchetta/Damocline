package calibration

import (
	"math"
	"time"
)

const (
	SchedulerLagMethod    = "monotonic planned-slot to scheduler-processing time"
	SchedulerLagRate      = 20
	SchedulerLagDuration  = 250 * time.Millisecond
	SchedulerLagTolerance = 50 * time.Millisecond
)

type SchedulerLag struct {
	Method       string                    `json:"method"`
	Rate         int                       `json:"arrival_rate_per_second"`
	Duration     string                    `json:"duration"`
	ToleranceMS  float64                   `json:"absolute_tolerance_ms"`
	Observations []SchedulerLagObservation `json:"observations"`
	Status       string                    `json:"status"`
}

type SchedulerLagObservation struct {
	InjectedPauseMS  float64 `json:"injected_pause_ms"`
	ExpectedMaxLagMS float64 `json:"expected_max_lag_ms"`
	ObservedMaxLagMS float64 `json:"observed_max_lag_ms"`
	Planned          int64   `json:"planned"`
	Admitted         int64   `json:"admitted"`
	Dropped          int64   `json:"dropped"`
	Outcome          string  `json:"outcome"`
}

func ValidateSchedulerLag(c SchedulerLag) bool {
	if c.Method != SchedulerLagMethod || c.Rate != SchedulerLagRate || c.Duration != SchedulerLagDuration.String() ||
		c.ToleranceMS != float64(SchedulerLagTolerance.Microseconds())/1000 || len(c.Observations) != 9 ||
		(c.Status != "PASSED" && c.Status != "FAILED") {
		return false
	}
	index := 0
	allPassed := true
	for _, pause := range []time.Duration{90, 130, 210} {
		expected := pause*time.Millisecond - time.Second/SchedulerLagRate
		for range 3 {
			o := c.Observations[index]
			deviation := math.Abs(o.ObservedMaxLagMS - float64(expected.Microseconds())/1000)
			within := deviation <= c.ToleranceMS
			if !within {
				allPassed = false
			}
			planned := int64(SchedulerLagDuration / (time.Second / SchedulerLagRate))
			if o.InjectedPauseMS != float64(pause) || o.ExpectedMaxLagMS != float64(expected.Microseconds())/1000 ||
				math.IsNaN(o.ObservedMaxLagMS) || math.IsInf(o.ObservedMaxLagMS, 0) || o.ObservedMaxLagMS < 0 ||
				o.Planned != planned || o.Admitted < 0 || o.Dropped < 0 || o.Admitted+o.Dropped != o.Planned ||
				(o.Outcome != "PASSED" && o.Outcome != "FAILED") || (o.Outcome == "PASSED") != within {
				return false
			}
			index++
		}
	}
	return (c.Status == "PASSED") == allPassed
}
