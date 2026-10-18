package main

import (
	"context"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/scheduler"
)

const (
	schedulerLagMethod    = calibration.SchedulerLagMethod
	schedulerLagRate      = calibration.SchedulerLagRate
	schedulerLagDuration  = calibration.SchedulerLagDuration
	schedulerLagTolerance = calibration.SchedulerLagTolerance
)

type schedulerLagCalibration = calibration.SchedulerLag
type schedulerLagObservation = calibration.SchedulerLagObservation

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
	return calibration.ValidateSchedulerLag(c)
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
