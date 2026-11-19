package campaign

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateObjectiveArtifactJSON checks persisted field presence before typed
// decoding can infer an undeclared zero. Callers own bounded, unambiguous bytes;
// ObjectiveContract.Validate and MatchesRun retain semantic validation.
func ValidateObjectiveArtifactJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("campaign objectives must be a JSON object")
	}
	for _, name := range []string{"version", "journey_scope", "journey_p95_limit_ms", "max_journey_error_rate", "min_samples_per_step", "min_completed_work_ratio", "max_dropped_arrivals", "minimum_observation_windows", "minimum_samples_per_window", "window_duration"} {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("campaign objectives omit required field %q; absence cannot be interpreted as zero", name)
		}
	}
	return nil
}
