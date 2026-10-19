package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestAuditCommandIsOfflineAndProducesMachineReadableEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	out := filepath.Join(t.TempDir(), "audit.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion,
		Scenario: "offline", BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{Number: 1, Outcome: "INCONCLUSIVE"}, {Number: 2, Outcome: "INCONCLUSIVE"}},
	}
	if err := report.WriteAnyJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"audit", "--campaign", path, "--peak-users", "2", "--out", out}, &stdout, &stderr); exit != 0 {
		t.Fatalf("audit command exit: %d %s", exit, stderr.String())
	}
	for _, expected := range []string{"INCONCLUSIVE", "LIMITED", "NOT_ESTABLISHED", "Demand target: 2", "No traffic was generated", "audit.json"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("missing %q in output: %s", expected, stdout.String())
		}
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		State string `json:"state"`
		Audit struct {
			State      string `json:"state"`
			EvidenceID string `json:"evidence_id"`
		} `json:"audit"`
		Demand *struct {
			TargetUsers int `json:"target_users"`
		} `json:"demand"`
	}
	if err := json.Unmarshal(raw, &audit); err != nil || audit.Audit.State != "INCONCLUSIVE" || audit.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("invalid audit JSON: %+v %v", audit, err)
	}
	if audit.Audit.EvidenceID == "" || audit.Demand == nil || audit.Demand.TargetUsers != 2 {
		t.Fatalf("combined assessment is missing traceability or demand result: %+v", audit)
	}
}

func TestAuditCommandRejectsMissingCampaign(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"audit"}, &stdout, &stderr); exit != 2 {
		t.Fatalf("missing file was accepted with exit %d", exit)
	}
}

func TestAuditCommandRequiresPeakWhenDemandOptionsAreSupplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, Scenario: "offline", BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE"}
	if err := report.WriteAnyJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"audit", "--campaign", path, "--growth-percent", "0"}, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "--peak-users must be positive") {
		t.Fatalf("demand option without peak target was accepted: exit=%d stderr=%s", exit, stderr.String())
	}
}
