package arrivalexplorer

import (
	"encoding/json"
	"testing"
)

func TestValidateArtifactJSONRequiresPersistedOpenArrivalEvidence(t *testing.T) {
	valid := []byte(`{"schema_version":1,"scenario":"synthetic","target":"http://127.0.0.1","started_at":"2026-01-01T00:00:00Z","stage_duration":"10s","repeats":2,"journey_worker_limit":2,"min_scheduled_journey_starts_per_second":1,"max_scheduled_journey_starts_per_second":2,"outcome":"INCONCLUSIVE","probes":[{"scheduled_journey_starts_per_second":1,"status":"INCONCLUSIVE","runs":[{"schema_version":32}]}],"notes":[]}`)
	if err := ValidateArtifactJSON(valid); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(valid, &original); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"scenario", "target", "started_at", "stage_duration", "repeats", "journey_worker_limit", "min_scheduled_journey_starts_per_second", "max_scheduled_journey_starts_per_second", "outcome", "probes", "notes"} {
		t.Run("missing_"+field, func(t *testing.T) {
			candidate := cloneArrivalRawFields(t, original)
			delete(candidate, field)
			data, _ := json.Marshal(candidate)
			if err := ValidateArtifactJSON(data); err == nil {
				t.Fatalf("artifact missing %q was accepted", field)
			}
		})
	}
	for _, field := range []string{"scheduled_journey_starts_per_second", "status", "runs"} {
		t.Run("missing_probe_"+field, func(t *testing.T) {
			candidate := cloneArrivalRawFields(t, original)
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
		candidate := cloneArrivalRawFields(t, original)
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
	calibrated := []byte(`{"schema_version":2,"scenario":"synthetic","target":"http://127.0.0.1","started_at":"2026-01-01T00:00:00Z","stage_duration":"10s","repeats":2,"journey_worker_limit":2,"min_scheduled_journey_starts_per_second":1,"max_scheduled_journey_starts_per_second":2,"requested_min_scheduled_starts_per_second":1,"requested_max_scheduled_starts_per_second":4,"calibrated_probe_rates":[],"outcome":"INCONCLUSIVE","probes":[],"notes":[]}`)
	if err := ValidateArtifactJSON(calibrated); err != nil {
		t.Fatalf("valid calibrated schema-v2 artifact rejected: %v", err)
	}
	var calibratedFields map[string]json.RawMessage
	if err := json.Unmarshal(calibrated, &calibratedFields); err != nil {
		t.Fatal(err)
	}
	delete(calibratedFields, "calibrated_probe_rates")
	calibrated, _ = json.Marshal(calibratedFields)
	if err := ValidateArtifactJSON(calibrated); err == nil {
		t.Fatal("schema-v2 artifact missing calibrated_probe_rates was accepted")
	}
	if err := ValidateArtifactJSON([]byte(`{"schema_version":1,"scenario":"","target":"","started_at":"","stage_duration":"","repeats":0,"journey_worker_limit":0,"min_scheduled_journey_starts_per_second":0,"max_scheduled_journey_starts_per_second":0,"outcome":"","probes":[],"notes":[]}`)); err != nil {
		t.Fatalf("explicit zero values and empty arrays must remain representable: %v", err)
	}
}

func cloneArrivalRawFields(t *testing.T, fields map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	cloned := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
