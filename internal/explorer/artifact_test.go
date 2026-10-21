package explorer

import (
	"encoding/json"
	"testing"
)

func TestValidateArtifactJSONRequiresPersistedExplorationEvidence(t *testing.T) {
	valid := []byte(`{"schema_version":1,"scenario":"synthetic","target":"http://127.0.0.1","started_at":"2026-01-01T00:00:00Z","stage_duration":"10s","repeats":2,"min_users":1,"max_users":2,"outcome":"INCONCLUSIVE","probes":[{"users":1,"status":"INCONCLUSIVE","runs":[{"schema_version":32}]}],"notes":[]}`)
	if err := ValidateArtifactJSON(valid); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(valid, &original); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"scenario", "target", "started_at", "stage_duration", "repeats", "min_users", "max_users", "outcome", "probes", "notes"} {
		t.Run("missing_"+field, func(t *testing.T) {
			candidate := cloneRawFields(t, original)
			delete(candidate, field)
			data, _ := json.Marshal(candidate)
			if err := ValidateArtifactJSON(data); err == nil {
				t.Fatalf("artifact missing %q was accepted", field)
			}
		})
	}
	for _, field := range []string{"users", "status", "runs"} {
		t.Run("missing_probe_"+field, func(t *testing.T) {
			candidate := cloneRawFields(t, original)
			var probes []map[string]json.RawMessage
			if err := json.Unmarshal(candidate["probes"], &probes); err != nil {
				t.Fatal(err)
			}
			delete(probes[0], field)
			candidate["probes"], _ = json.Marshal(probes)
			data, _ := json.Marshal(candidate)
			if err := ValidateArtifactJSON(data); err == nil {
				t.Fatalf("probe missing %q was accepted", field)
			}
		})
	}
	t.Run("missing_embedded_run_schema", func(t *testing.T) {
		candidate := cloneRawFields(t, original)
		var probes []map[string]json.RawMessage
		if err := json.Unmarshal(candidate["probes"], &probes); err != nil {
			t.Fatal(err)
		}
		var runs []map[string]json.RawMessage
		if err := json.Unmarshal(probes[0]["runs"], &runs); err != nil {
			t.Fatal(err)
		}
		delete(runs[0], "schema_version")
		probes[0]["runs"], _ = json.Marshal(runs)
		candidate["probes"], _ = json.Marshal(probes)
		data, _ := json.Marshal(candidate)
		if err := ValidateArtifactJSON(data); err == nil {
			t.Fatal("embedded run missing schema_version was accepted")
		}
	})
	if err := ValidateArtifactJSON([]byte(`{"schema_version":1,"scenario":"","target":"","started_at":"","stage_duration":"","repeats":0,"min_users":0,"max_users":0,"outcome":"","probes":[],"notes":[]}`)); err != nil {
		t.Fatalf("explicit zero values and empty arrays must remain representable: %v", err)
	}
}

func cloneRawFields(t *testing.T, fields map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	cloned := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
