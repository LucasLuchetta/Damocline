package selfcalibration

import (
	"context"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
)

// A scenario observation window longer than a calibration level must not
// prevent calibration; the capacity protocol uses short levels while target
// scenarios commonly declare 30s windows.
func TestSelfCalibrationClampsWindowLongerThanLevel(t *testing.T) {
	s := authenticatedScenario()
	s.Duration = "1m"
	s.WindowDuration = "30s"
	record, err := Run(context.Background(), s, shortOptions(2))
	if err != nil {
		t.Fatalf("calibration refused a long scenario window: %v", err)
	}
	if record.Status != calibration.ScenarioCalibrationPassed {
		t.Fatalf("unexpected status: %+v", record)
	}
}
