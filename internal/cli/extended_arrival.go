package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func loadExtendedArrivalCalibration(path string) (*calibration.ScenarioRecord, error) {
	loaded, err := loadAnyCalibration(path)
	if err != nil {
		return nil, err
	}
	if loaded.scenario == nil || loaded.scenario.Controls.Mode != "arrival" {
		return nil, errors.New("extended arrival requires an open-arrival self-calibration artifact")
	}
	return loaded.scenario, nil
}

func gateExtendedArrivalRates(v *config.Validated, record *calibration.ScenarioRecord, rates []int) error {
	if len(rates) == 0 || record.Controls.MaxRPS != v.Scenario.MaxRPS || record.Controls.RequestTimeout != v.Timeout.String() {
		return errors.New("calibration must cover the planned rates with the same request ceiling and timeout")
	}
	runtime, now := currentCalibrationRuntime(), time.Now().UTC()
	envelope := calibration.Record{SelfCalibration: record, SelfCalibrationFingerprint: v.WorkloadFingerprint}
	for _, rate := range rates {
		s := v.Scenario
		s.ArrivalRate = rate
		coverage := assessExtendedCoverage(s, envelope, runtime, now)
		if coverage.Status != "READY" {
			return fmt.Errorf("calibration does not cover %d scheduled starts/s at %d workers: %s", rate, s.Users, coverage.Reason)
		}
	}
	v.Envelope.CalibrationCoverage = "READY"
	return nil
}
