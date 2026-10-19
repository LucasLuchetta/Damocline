package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func attachCalibration(result *report.Result, scenario config.Scenario, path string, now time.Time) error {
	record, err := loadCalibration(path)
	if err != nil {
		return err
	}
	attachCalibrationRecord(result, scenario, record, now)
	return nil
}

// selectCalibratedArrivalRates restricts an adaptive search to exact profiles
// that match the declared workload. The first requested rate must be covered;
// an uncovered maximum safely lowers the effective ceiling. In-range exact
// demand targets must also be directly calibrated.
func selectCalibratedArrivalRates(scenario config.Scenario, minRate, maxRate, target int, record calibration.Record, now time.Time) ([]int, calibration.ArrivalEnvelope, error) {
	rates := make([]int, 0, maxRate-minRate+1)
	for rate := minRate; rate <= maxRate; rate++ {
		rates = append(rates, rate)
	}
	envelope := calibration.AssessArrivalEnvelope(scenario, rates, record, calibration.CurrentRuntime(), now.UTC())
	ready := make([]int, 0, len(envelope.Rates))
	for _, rate := range envelope.Rates {
		if rate.Status == "READY" {
			ready = append(ready, rate.ArrivalRate)
		}
	}
	if len(ready) == 0 || ready[0] != minRate {
		return nil, envelope, fmt.Errorf("the requested minimum rate %d/s has no exact matching calibration profile", minRate)
	}
	if target >= minRate && target <= maxRate {
		found := false
		for _, rate := range ready {
			if rate == target {
				found = true
				break
			}
		}
		if !found {
			return nil, envelope, fmt.Errorf("the in-range exact demand target %d/s has no exact matching calibration profile", target)
		}
	}
	return ready, envelope, nil
}

func printArrivalExploreEnvelope(stdout io.Writer, envelope calibration.ArrivalEnvelope) {
	ready := make([]int, 0, len(envelope.Rates))
	blocked := 0
	for _, rate := range envelope.Rates {
		if rate.Status == "READY" {
			ready = append(ready, rate.ArrivalRate)
		} else {
			blocked++
		}
	}
	fmt.Fprintf(stdout, "Local generator preflight: %d exact scheduled-start profiles are eligible; %d requested levels are outside the measured matrix. This is fixture evidence, not target capacity or duration certification.\n", len(ready), blocked)
	if len(ready) > 0 {
		fmt.Fprintf(stdout, "  Exact compatible rates: %v/s\n", ready)
	}
}

func loadCalibration(path string) (calibration.Record, error) {
	return calibration.LoadRecord(path)
}

func attachCalibrationRecord(result *report.Result, scenario config.Scenario, record calibration.Record, now time.Time) {
	coverage := calibration.AssessScenario(scenario, record, calibration.CurrentRuntime(), now.UTC())
	coverage.Dimensions = append(
		coverage.Dimensions,
		report.AssessObservedResponseSize(*result, int64(record.Fixture.HealthyResponseBytes)),
		report.AssessObservedResponseLatency(*result),
	)
	result.Calibration = &report.CalibrationEvidence{Record: record, Coverage: coverage}
	result.Quality = report.AssessQuality(*result)
}
