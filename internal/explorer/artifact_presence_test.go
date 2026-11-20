package explorer

import (
	"encoding/json"
	"strings"
	"testing"
)

const presenceFixture = `{"schema_version":2,"scenario":"synthetic","target":"http://127.0.0.1:18080","started_at":"2026-01-02T03:04:05Z","stage_duration":"1s","repeats":0,"min_users":0,"max_users":0,"outcome":"INCONCLUSIVE","probes":[{"users":0,"status":"PASS","runs":[{"schema_version":34}]}],"notes":[]}`

func mutatePresenceFixture(t *testing.T, mutate func(root, probe, run map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(presenceFixture), &root); err != nil {
		t.Fatal(err)
	}
	probe := root["probes"].([]any)[0].(map[string]any)
	run := probe["runs"].([]any)[0].(map[string]any)
	mutate(root, probe, run)
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestValidateArtifactJSONRequiresEveryPersistedExplorationField(t *testing.T) {
	if err := ValidateArtifactJSON([]byte(presenceFixture)); err != nil {
		t.Fatalf("explicit zero exploration evidence was rejected: %v", err)
	}
	for _, field := range []string{"schema_version", "scenario", "target", "started_at", "stage_duration", "repeats", "min_users", "max_users", "outcome", "probes", "notes"} {
		for _, null := range []bool{false, true} {
			data := mutatePresenceFixture(t, func(root, _, _ map[string]any) {
				if null {
					root[field] = nil
				} else {
					delete(root, field)
				}
			})
			if err := ValidateArtifactJSON(data); err == nil || !strings.Contains(err.Error(), field) && !strings.Contains(err.Error(), "schema_version") {
				t.Fatalf("exploration without %q (null=%v) was accepted or unclear: %v", field, null, err)
			}
		}
	}
	for _, field := range []string{"users", "status", "runs"} {
		data := mutatePresenceFixture(t, func(_, probe, _ map[string]any) { delete(probe, field) })
		if err := ValidateArtifactJSON(data); err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("exploration probe without %q was accepted: %v", field, err)
		}
	}
	data := mutatePresenceFixture(t, func(_, _, run map[string]any) { delete(run, "schema_version") })
	if err := ValidateArtifactJSON(data); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("exploration run without schema_version was accepted: %v", err)
	}
	for _, version := range []any{0, 3, "2"} {
		data := mutatePresenceFixture(t, func(root, _, _ map[string]any) { root["schema_version"] = version })
		if err := ValidateArtifactJSON(data); err == nil {
			t.Fatalf("unsupported exploration schema_version %v was accepted", version)
		}
	}
}
