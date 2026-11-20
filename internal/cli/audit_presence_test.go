package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
)

// TestAuditCommandRejectsCampaignsMissingRequiredEvidence verifies that the
// offline audit reader applies the same presence checks as the other campaign
// readers before typed decoding, for every required current top-level field.
func TestAuditCommandRejectsCampaignsMissingRequiredEvidence(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Scenario: "offline", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}},
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, data []byte) (int, string) {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "campaign.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		exit := Run([]string{"audit", "--campaign", path, "--peak-users", "2", "--out", filepath.Join(dir, "audit.json")}, &stdout, &stderr)
		return exit, stderr.String()
	}
	if exit, stderr := run(t, encoded); exit != 0 {
		t.Fatalf("baseline campaign was not auditable: %d %s", exit, stderr)
	}
	for _, field := range []string{"schema_version", "scenario", "base_users", "target", "started_at", "warmup", "observation", "recovery", "repeats", "trials", "outcome", "cooldown", "execution_state", "objectives", "stability_policy"} {
		for _, null := range []bool{false, true} {
			var root map[string]any
			if err := json.Unmarshal(encoded, &root); err != nil {
				t.Fatal(err)
			}
			if _, ok := root[field]; !ok {
				t.Fatalf("fixture lacks %q", field)
			}
			if null {
				root[field] = nil
			} else {
				delete(root, field)
			}
			mutated, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			exit, stderr := run(t, mutated)
			if exit == 0 {
				t.Fatalf("audit accepted a campaign without %q (null=%v)", field, null)
			}
			// Without "trials" the artifact no longer has a campaign shape, so
			// the shared metric-presence gate rejects it as an unrecognized run
			// before campaign presence checks name the field.
			if field == "trials" && strings.Contains(stderr, "omits required metrics") {
				continue
			}
			if !strings.Contains(stderr, field) && !strings.Contains(stderr, "objectives") && !strings.Contains(stderr, "stability") {
				t.Fatalf("audit rejection for missing %q does not name the field: %s", field, stderr)
			}
		}
	}
}
