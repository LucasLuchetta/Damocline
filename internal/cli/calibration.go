package cli

import (
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func attachCalibration(result *report.Result, scenario config.Scenario, path string, now time.Time) error {
	record, err := calibration.LoadRecord(path)
	if err != nil {
		return err
	}
	coverage := calibration.AssessScenario(scenario, record, calibration.CurrentRuntime(), now.UTC())
	coverage.Dimensions = append(
		coverage.Dimensions,
		report.AssessObservedResponseSize(*result, int64(record.Fixture.HealthyResponseBytes)),
	)
	result.Calibration = &report.CalibrationEvidence{Record: record, Coverage: coverage}
	result.Quality = report.AssessQuality(*result)
	return nil
}
