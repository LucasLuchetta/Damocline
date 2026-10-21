package calibration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
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
	if err := jsonartifact.RejectDuplicateKeys(b); err != nil {
		return Record{}, errors.New("calibration artifact contains ambiguous JSON")
	}
	if err := validateArtifactPresence(b); err != nil {
		return Record{}, err
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

func validateArtifactPresence(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return errors.New("calibration artifact must be a JSON object")
	}
	if err := requireFields(root, "schema_version", "status", "preflight", "scheduler_lag_calibration", "runs"); err != nil {
		return errors.New("calibration artifact omits required evidence")
	}
	var preflight map[string]json.RawMessage
	if err := json.Unmarshal(root["preflight"], &preflight); err != nil || preflight == nil {
		return errors.New("calibration preflight is missing or invalid")
	}
	if err := requireFields(preflight, "schema_version", "kind", "status", "started_at", "finished_at", "generator", "fixture", "scenario_fingerprint_sha256", "healthy_stages", "highest_passing_tested_arrival_rate", "interpretation"); err != nil {
		return errors.New("calibration preflight omits required evidence")
	}
	var version int
	if err := json.Unmarshal(preflight["schema_version"], &version); err != nil || version < minimumSupportedSchemaVersion || version > SchemaVersion {
		return errors.New("calibration preflight schema is invalid")
	}
	var generator, fixture map[string]json.RawMessage
	if json.Unmarshal(preflight["generator"], &generator) != nil || generator == nil ||
		json.Unmarshal(preflight["fixture"], &fixture) != nil || fixture == nil {
		return errors.New("calibration generator or fixture is missing")
	}
	if err := requireFields(generator, "os", "arch", "logical_cpus", "gomaxprocs", "go_version", "runner_version"); err != nil {
		return errors.New("calibration generator omits required evidence")
	}
	if version >= 7 {
		if err := requireFields(generator, "cpu_capacity_cores", "cpu_capacity_source", "cpu_capacity_known"); err != nil {
			return errors.New("calibration generator omits effective CPU evidence")
		}
	}
	if err := requireFields(fixture, "network_mode", "workload_model", "method", "healthy_response_bytes", "healthy_delay", "stage_duration", "soak_duration", "stage_users", "maximum_rps", "repeats", "extended", "healthy_rates"); err != nil {
		return errors.New("calibration fixture omits required controls")
	}
	if version >= 2 {
		if err := requireFields(preflight, "fixture_workload_fingerprint"); err != nil {
			return errors.New("calibration preflight omits fixture workload identity")
		}
	}
	if version >= 3 {
		if err := requireFields(preflight, "workload_observations"); err != nil {
			return errors.New("calibration preflight omits workload observations")
		}
		if err := requireFields(fixture, "workload_profiles"); err != nil {
			return errors.New("calibration fixture omits workload profiles")
		}
	}
	return nil
}

func requireFields(object map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		value, ok := object[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("required field %q is absent", name)
		}
	}
	return nil
}
