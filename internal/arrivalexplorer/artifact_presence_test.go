package arrivalexplorer

import (
	"encoding/json"
	"strings"
	"testing"
)

var baseArrivalFields = []string{"schema_version", "scenario", "target", "started_at", "stage_duration", "repeats", "journey_worker_limit", "min_scheduled_journey_starts_per_second", "max_scheduled_journey_starts_per_second", "outcome", "probes", "notes"}

var schemaTwoArrivalFields = []string{"requested_min_scheduled_starts_per_second", "requested_max_scheduled_starts_per_second", "calibrated_probe_rates"}

func arrivalPresenceFixture(t *testing.T, schema int) map[string]any {
	t.Helper()
	raw := `{"scenario":"synthetic","target":"http://127.0.0.1:18080","started_at":"2026-01-02T03:04:05Z","stage_duration":"1s","repeats":0,"journey_worker_limit":0,"min_scheduled_journey_starts_per_second":0,"max_scheduled_journey_starts_per_second":0,"outcome":"INCONCLUSIVE","probes":[{"scheduled_journey_starts_per_second":0,"status":"PASS","runs":[{"schema_version":34}]}],"notes":[]}`
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	root["schema_version"] = schema
	if schema >= 2 {
		root["requested_min_scheduled_starts_per_second"] = 0
		root["requested_max_scheduled_starts_per_second"] = 0
		root["calibrated_probe_rates"] = []any{}
	}
	return root
}

func encodeArrival(t *testing.T, root map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestValidateArtifactJSONRequiresEveryPersistedArrivalExplorationField(t *testing.T) {
	for _, schema := range []int{1, 2} {
		if err := ValidateArtifactJSON(encodeArrival(t, arrivalPresenceFixture(t, schema))); err != nil {
			t.Fatalf("schema-v%d explicit zero arrival exploration was rejected: %v", schema, err)
		}
		fields := baseArrivalFields
		if schema >= 2 {
			fields = append(append([]string{}, baseArrivalFields...), schemaTwoArrivalFields...)
		}
		for _, field := range fields {
			for _, null := range []bool{false, true} {
				root := arrivalPresenceFixture(t, schema)
				if null {
					root[field] = nil
				} else {
					delete(root, field)
				}
				if err := ValidateArtifactJSON(encodeArrival(t, root)); err == nil || !strings.Contains(err.Error(), field) && !strings.Contains(err.Error(), "schema_version") {
					t.Fatalf("schema-v%d arrival exploration without %q (null=%v) was accepted or unclear: %v", schema, field, null, err)
				}
			}
		}
		for _, field := range []string{"scheduled_journey_starts_per_second", "status", "runs"} {
			root := arrivalPresenceFixture(t, schema)
			delete(root["probes"].([]any)[0].(map[string]any), field)
			if err := ValidateArtifactJSON(encodeArrival(t, root)); err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("schema-v%d arrival probe without %q was accepted: %v", schema, field, err)
			}
		}
		root := arrivalPresenceFixture(t, schema)
		delete(root["probes"].([]any)[0].(map[string]any)["runs"].([]any)[0].(map[string]any), "schema_version")
		if err := ValidateArtifactJSON(encodeArrival(t, root)); err == nil || !strings.Contains(err.Error(), "schema_version") {
			t.Fatalf("schema-v%d arrival run without schema_version was accepted: %v", schema, err)
		}
	}
	legacy := arrivalPresenceFixture(t, 1)
	if err := ValidateArtifactJSON(encodeArrival(t, legacy)); err != nil {
		t.Fatalf("schema-v1 arrival exploration must not require schema-v2 calibration fields: %v", err)
	}
}
