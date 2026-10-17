package main

import "testing"

func TestSchedulerLagCalibrationMeasuresInjectedPauses(t *testing.T) {
	got := calibrateSchedulerLag()
	if got.Status != "PASSED" || !validSchedulerLagCalibration(got) {
		t.Fatalf("scheduler lag calibration failed: %+v", got)
	}
	for _, observation := range got.Observations {
		if observation.Planned != 5 || observation.Admitted+observation.Dropped != observation.Planned {
			t.Fatalf("scheduler lag calibration did not reconcile arrivals: %+v", observation)
		}
	}
}

func TestSchedulerLagCalibrationRejectsEditedEvidence(t *testing.T) {
	calibration := passedSchedulerLagCalibration()
	if !validSchedulerLagCalibration(calibration) {
		t.Fatal("valid fixture calibration rejected")
	}
	calibration.Observations[0].ObservedMaxLagMS += calibration.ToleranceMS + 1
	if validSchedulerLagCalibration(calibration) {
		t.Fatal("out-of-tolerance measurement accepted as valid")
	}
}
