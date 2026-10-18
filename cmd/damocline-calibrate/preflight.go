package main

import (
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
)

const (
	preflightSchemaVersion  = calibration.SchemaVersion
	preflightInterpretation = calibration.Interpretation
)

type preflightGenerator = calibration.Generator
type preflightFixture = calibration.Fixture
type preflightStage = calibration.Stage
type preflightRecord = calibration.Record
type preflightWorkloadObservation = calibration.WorkloadObservation

func sourceRunnerVersion() string { return calibration.SourceRunnerVersion() }

func preflightFingerprint(fixture preflightFixture) (string, error) {
	return calibration.Fingerprint(fixture)
}

func newPreflightRecord(start, end time.Time, generator preflightGenerator, fixture preflightFixture, stages []preflightStage, status string, observations ...[]calibration.WorkloadObservation) (preflightRecord, error) {
	return calibration.NewRecord(start, end, generator, fixture, stages, status, observations...)
}

func validatePreflightRecord(rec preflightRecord) error {
	return calibration.ValidateRecord(rec)
}
