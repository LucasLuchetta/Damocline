package calibration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

const maxArtifactBytes = 16 << 20

type artifact struct {
	SchemaVersion int          `json:"schema_version"`
	Status        string       `json:"status"`
	Preflight     Record       `json:"preflight"`
	Checks        []string     `json:"checks"`
	SchedulerLag  SchedulerLag `json:"scheduler_lag_calibration"`
	Runs          []stageRun   `json:"runs"`
}

type stageRun struct {
	ArrivalsScheduled   int64  `json:"arrivals_scheduled"`
	IterationsStarted   int64  `json:"iterations_started"`
	IterationsCompleted int64  `json:"iterations_completed"`
	ArrivalsDropped     int64  `json:"arrivals_dropped"`
	Outcome             string `json:"outcome"`
}

// LoadRecord reads a bounded calibration artifact and verifies that its
// embedded run ledgers agree with the independently recorded preflight stages.
func LoadRecord(path string) (Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return Record{}, errors.New("calibration artifact is unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxArtifactBytes+1))
	if err != nil || len(b) > maxArtifactBytes {
		return Record{}, errors.New("calibration artifact exceeds the read limit or cannot be read")
	}
	return ValidateArtifactBytes(b, true)
}

// ValidateArtifactBytes verifies a complete calibration JSON artifact. Failed
// calibrations may be inspected offline, but callers attaching evidence to a
// run must set requirePassing.
func ValidateArtifactBytes(b []byte, requirePassing bool) (Record, error) {
	var result artifact
	if len(b) > maxArtifactBytes {
		return Record{}, errors.New("calibration artifact exceeds the read limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(&result); err != nil {
		return Record{}, errors.New("calibration artifact is not valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Record{}, errors.New("calibration artifact contains trailing data")
	}
	if result.SchemaVersion != 1 || (result.Status != "REFERENCE_SMOKE_PASSED" && result.Status != "CHECKS_FAILED") ||
		result.Status != result.Preflight.Status || ValidateRecord(result.Preflight) != nil ||
		!ValidateSchedulerLag(result.SchedulerLag) || (result.Status == "REFERENCE_SMOKE_PASSED" && (len(result.Checks) != 0 || result.SchedulerLag.Status != "PASSED")) ||
		(requirePassing && result.Status != "REFERENCE_SMOKE_PASSED") ||
		len(result.Runs) != len(result.Preflight.HealthyStages) {
		return Record{}, errors.New("calibration artifact is incomplete or failed validation")
	}
	for i, stage := range result.Preflight.HealthyStages {
		run := result.Runs[i]
		if run.ArrivalsScheduled != stage.Scheduled || run.IterationsStarted != stage.Started ||
			run.IterationsCompleted != stage.Completed || run.ArrivalsDropped != stage.Dropped ||
			run.Outcome != stage.Outcome || int64(stage.ArrivalRate)*5 != stage.Scheduled {
			return Record{}, errors.New("calibration stage ledger does not reconcile")
		}
	}
	return result.Preflight, nil
}
