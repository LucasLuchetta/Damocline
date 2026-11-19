package cli

import (
	"bytes"
	"encoding/json"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditRejectsUndeclaredZeroObjectivesBeforeOutputs(t *testing.T) {
	objectives := defaultCampaignObjectives()
	objectives.MaxJourneyErrorRate = 0
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic", BaseUsers: 2, Target: "https://app.example.com", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Stabilization: "0s", Observation: "10s", Recovery: "10s", Cooldown: "0s", Repeats: 2, Outcome: "INCONCLUSIVE", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: objectives, Trials: []campaign.Trial{{Number: 1, RunID: strings.Repeat("a", 32), Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}, {Number: 2, RunID: strings.Repeat("b", 32), Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}}}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "campaign.json")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"audit", "--campaign", source}, &stdout, &stderr); exit != 0 {
		t.Fatalf("complete zero criteria rejected: %d %s", exit, stderr.String())
	}
	for _, field := range []string{"max_journey_error_rate", "max_dropped_arrivals"} {
		t.Run(field, func(t *testing.T) {
			var root map[string]json.RawMessage
			_ = json.Unmarshal(data, &root)
			var rawObjectives map[string]json.RawMessage
			_ = json.Unmarshal(root["objectives"], &rawObjectives)
			delete(rawObjectives, field)
			root["objectives"], _ = json.Marshal(rawObjectives)
			changed, _ := json.Marshal(root)
			if err := os.WriteFile(source, changed, 0600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "assessment.json")
			markdown := filepath.Join(t.TempDir(), "capacity.md")
			stdout.Reset()
			stderr.Reset()
			if exit := Run([]string{"audit", "--campaign", source, "--out", out, "--report-out", markdown}, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), field) {
				t.Fatalf("undeclared criterion was assessed: exit=%d stderr=%s", exit, stderr.String())
			}
			for _, path := range []string{out, markdown} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid criteria created output: %s error=%v", filepath.Base(path), err)
				}
			}
		})
	}
}
