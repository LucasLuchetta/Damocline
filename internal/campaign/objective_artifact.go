package campaign

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ValidateObjectiveArtifactJSON checks persisted field presence before typed
// decoding can infer an undeclared zero. Callers own bounded, unambiguous bytes;
// ObjectiveContract.Validate and MatchesRun retain semantic validation.
func ValidateObjectiveArtifactJSON(data []byte) error {
	return validateRecordedPolicy(data, "objectives", "version", "journey_scope", "journey_p95_limit_ms", "max_journey_error_rate", "min_samples_per_step", "min_completed_work_ratio", "max_dropped_arrivals", "minimum_observation_windows", "minimum_samples_per_window", "window_duration")
}

// ValidateStabilityPolicyArtifactJSON requires recorded thresholds rather than
// allowing typed decoding to supply zero values for omitted controls.
func ValidateStabilityPolicyArtifactJSON(data []byte) error {
	return validateRecordedPolicy(data, "stability_policy", "minimum_windows", "minimum_samples_per_window",
		"max_p95_increase_percent", "max_p95_increase_ms", "max_throughput_drop_percent")
}

func validateRecordedPolicy(data []byte, policy string, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("campaign %s must be a JSON object", policy)
	}
	for _, name := range required {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("campaign %s omits required field %q; absence cannot be interpreted as zero", policy, name)
		}
	}
	return nil
}
