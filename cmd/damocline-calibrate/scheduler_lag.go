package main

import (
	"context"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/scheduler"
)

const (
	schedulerLagMethod    = "monotonic planned-slot to scheduler-processing time"
	schedulerLagRate      = 20
	schedulerLagDuration  = 250 * time.Millisecond
	schedulerLagTolerance = 50 * time.Millisecond
)

type schedulerLagCalibration struct {
	Method       string                    `json:"method"`
	Rate         int                       `json:"arrival_rate_per_second"`
	Duration     string                    `json:"duration"`
	ToleranceMS  float64                   `json:"absolute_tolerance_ms"`
	Observations []schedulerLagObservation `json:"observations"`
	Status       string                    `json:"status"`
}

type schedulerLagObservation struct {
	InjectedPauseMS  float64 `json:"injected_pause_ms"`
	ExpectedMaxLagMS float64 `json:"expected_max_lag_ms"`
	ObservedMaxLagMS float64 `json:"observed_max_lag_ms"`
	Planned          int64   `json:"planned"`
	Admitted         int64   `json:"admitted"`
	Dropped          int64   `json:"dropped"`
	Outcome          string  `json:"outcome"`
}

// calibrateSchedulerLag injects bounded scheduler-thread pauses. It measures
// planned-slot lateness, not HTTP transport latency or real-host capacity.
func calibrateSchedulerLag() schedulerLagCalibration {
	result := schedulerLagCalibration{
		Method: schedulerLagMethod, Rate: schedulerLagRate,
		Duration:    schedulerLagDuration.String(),
		ToleranceMS: float64(schedulerLagTolerance.Microseconds()) / 1000,
		Status:      "PASSED",
	}
	interval := time.Second / schedulerLagRate
	for _, pause := range []time.Duration{90, 130, 210} {
		for range 3 {
			calls := 0
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			stats := scheduler.Drive(ctx, schedulerLagDuration, schedulerLagRate, func() bool {
				calls++
				if calls == 1 {
					time.Sleep(pause * time.Millisecond)
				}
				return true
			})
			cancel()
			expected := pause*time.Millisecond - interval
			deviation := math.Abs(float64(stats.MaxLag-expected) / float64(time.Millisecond))
			observation := schedulerLagObservation{
				InjectedPauseMS:  float64(pause),
				ExpectedMaxLagMS: float64(expected.Microseconds()) / 1000,
				ObservedMaxLagMS: float64(stats.MaxLag.Microseconds()) / 1000,
				Planned:          stats.Planned, Admitted: stats.Admitted, Dropped: stats.Dropped(),
				Outcome: "PASSED",
			}
			if stats.Planned != scheduler.DueCount(schedulerLagDuration, interval, schedulerLagDuration) ||
				!stats.Consistent() || calls == 0 || deviation > float64(schedulerLagTolerance)/float64(time.Millisecond) {
				observation.Outcome = "FAILED"
				result.Status = "FAILED"
			}
			result.Observations = append(result.Observations, observation)
		}
	}
	return result
}

func validSchedulerLagCalibration(c schedulerLagCalibration) bool {
	if c.Method != schedulerLagMethod || c.Rate != schedulerLagRate ||
		c.Duration != schedulerLagDuration.String() ||
		c.ToleranceMS != float64(schedulerLagTolerance.Microseconds())/1000 ||
		len(c.Observations) != 9 || (c.Status != "PASSED" && c.Status != "FAILED") {
		return false
	}
	index := 0
	allPassed := true
	for _, pause := range []time.Duration{90, 130, 210} {
		expected := pause*time.Millisecond - time.Second/schedulerLagRate
		for range 3 {
			o := c.Observations[index]
			deviation := math.Abs(float64(o.ObservedMaxLagMS - float64(expected.Microseconds())/1000))
			withinTolerance := deviation <= c.ToleranceMS
			if !withinTolerance {
				allPassed = false
			}
			if o.InjectedPauseMS != float64(pause) ||
				o.ExpectedMaxLagMS != float64(expected.Microseconds())/1000 ||
				math.IsNaN(o.ObservedMaxLagMS) || math.IsInf(o.ObservedMaxLagMS, 0) || o.ObservedMaxLagMS < 0 ||
				o.Planned != scheduler.DueCount(schedulerLagDuration, time.Second/schedulerLagRate, schedulerLagDuration) ||
				o.Admitted < 0 || o.Dropped < 0 || o.Admitted+o.Dropped != o.Planned ||
				(o.Outcome != "PASSED" && o.Outcome != "FAILED") ||
				(o.Outcome == "PASSED") != withinTolerance {
				return false
			}
			index++
		}
	}
	return (c.Status == "PASSED") == allPassed
}

func passedSchedulerLagCalibration() schedulerLagCalibration {
	c := schedulerLagCalibration{Method: schedulerLagMethod, Rate: schedulerLagRate,
		Duration:    schedulerLagDuration.String(),
		ToleranceMS: float64(schedulerLagTolerance.Microseconds()) / 1000,
		Status:      "PASSED"}
	for _, pause := range []time.Duration{90, 130, 210} {
		expected := pause*time.Millisecond - time.Second/schedulerLagRate
		for range 3 {
			observed := float64(expected.Microseconds()) / 1000
			planned := scheduler.DueCount(schedulerLagDuration, time.Second/schedulerLagRate, schedulerLagDuration)
			c.Observations = append(c.Observations, schedulerLagObservation{
				InjectedPauseMS: float64(pause), ExpectedMaxLagMS: observed, ObservedMaxLagMS: observed,
				Planned: planned, Admitted: planned, Outcome: "PASSED",
			})
		}
	}
	return c
}
