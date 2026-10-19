package cli

import (
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
