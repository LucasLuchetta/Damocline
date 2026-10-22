package arrivalcampaign

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
)

// ValidateArtifactJSON rejects omitted schema-defined evidence before JSON
// decoding can turn missing fields into plausible Go zero values. Semantic
// campaign validation remains the responsibility of the offline assessor.
func ValidateArtifactJSON(data []byte) error {
	if err := jsonartifact.RejectDuplicateKeys(data); err != nil {
		return fmt.Errorf("arrival campaign contains ambiguous JSON: %w", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return errors.New("arrival campaign must be a JSON object")
	}
	if err := requireArtifactFields(root, "arrival_campaign_schema_version", "scenario", "target", "started_at",
		"journey_worker_limit", "workload_fingerprint", "request_timeout", "max_rps", "p95_limit_ms",
		"max_error_rate", "min_samples_per_step", "window_duration", "base_scheduled_journey_starts_per_second",
		"warmup", "observation", "recovery", "budget", "repeats", "trials", "outcome"); err != nil {
		return err
	}
	var schema int
	if err := json.Unmarshal(root["arrival_campaign_schema_version"], &schema); err != nil || schema < 1 || schema > CurrentSchemaVersion {
		return errors.New("arrival campaign has an unsupported schema version")
	}
	if schema >= 5 {
		if err := requireArtifactFields(root, "cooldown"); err != nil {
			return err
		}
	}
	if schema >= 6 {
		if err := requireArtifactFields(root, "execution_state"); err != nil {
			return err
		}
	}
	if schema >= 8 {
		if err := requireArtifactFields(root, "objectives", "stability_policy"); err != nil {
			return err
		}
	}
	var trials []json.RawMessage
	if err := json.Unmarshal(root["trials"], &trials); err != nil || trials == nil || !bytes.HasPrefix(bytes.TrimSpace(root["trials"]), []byte("[")) {
		return errors.New("arrival campaign trials must be a JSON array")
	}
	for index, rawTrial := range trials {
		if err := validateArrivalTrialArtifact(rawTrial, schema); err != nil {
			return fmt.Errorf("arrival campaign trial %d: %w", index+1, err)
		}
	}
	if pending, ok := root["pending_trial"]; ok && !bytes.Equal(bytes.TrimSpace(pending), []byte("null")) {
		if schema < 6 {
			return errors.New("arrival campaign pending trial is unsupported by this schema")
		}
		if err := validateArrivalTrialArtifact(pending, schema); err != nil {
			return fmt.Errorf("arrival campaign pending trial: %w", err)
		}
	}
	return nil
}

func validateArrivalTrialArtifact(data []byte, schema int) error {
	var trial map[string]json.RawMessage
	if err := json.Unmarshal(data, &trial); err != nil || trial == nil {
		return errors.New("trial must be a JSON object")
	}
	if err := requireArtifactFields(trial, "number", "outcome"); err != nil {
		return err
	}
	phasesRaw, hasPhases := trial["phases"]
	if !hasPhases {
		return errors.New("arrival campaign omits required field \"phases\"; absence cannot be interpreted as zero")
	}
	if schema >= 5 {
		if err := requireArtifactFields(trial, "run_id"); err != nil {
			return err
		}
	}
	var phases []json.RawMessage
	trimmedPhases := bytes.TrimSpace(phasesRaw)
	if bytes.Equal(trimmedPhases, []byte("null")) && schema < 6 {
		// Historical aborted trials can explicitly record no completed phases as
		// null. Current checkpoint schemas initialize this ledger as an array.
		return nil
	}
	if err := json.Unmarshal(phasesRaw, &phases); err != nil || phases == nil || !bytes.HasPrefix(trimmedPhases, []byte("[")) {
		return errors.New("trial phases must be a JSON array")
	}
	for index, rawPhase := range phases {
		var phase map[string]json.RawMessage
		if err := json.Unmarshal(rawPhase, &phase); err != nil || phase == nil {
			return fmt.Errorf("phase %d must be a JSON object", index+1)
		}
		if err := requireArtifactFields(phase, "name", "evidence_state", "run"); err != nil {
			return fmt.Errorf("phase %d: %w", index+1, err)
		}
		var run map[string]json.RawMessage
		if err := json.Unmarshal(phase["run"], &run); err != nil || run == nil {
			return fmt.Errorf("phase %d run must be a JSON object", index+1)
		}
		if err := requireArtifactFields(run, "schema_version"); err != nil {
			return fmt.Errorf("phase %d run: %w", index+1, err)
		}
	}
	return nil
}

func requireArtifactFields(fields map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("arrival campaign omits required field %q; absence cannot be interpreted as zero", name)
		}
	}
	return nil
}
