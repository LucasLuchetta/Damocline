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
	for _, expected := range []string{"INCONCLUSIVE", "LIMITED", "NOT_ESTABLISHED", "Confidence dimensions:", "NOT_ASSESSED", "Demand target: 2", "No traffic was generated", "audit.json"} {
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
		Confidence struct {
			Measurement string `json:"measurement_confidence"`
			Workload    string `json:"workload_representativeness"`
			Decision    string `json:"decision_confidence"`
		} `json:"confidence_dimensions"`
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
	if audit.Confidence.Workload != "NOT_ASSESSED" || audit.Confidence.Decision != "NOT_ESTABLISHED" || audit.Confidence.Measurement != "LIMITED" {
		t.Fatalf("combined assessment collapsed confidence dimensions: %+v", audit.Confidence)
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

func TestAuditCommandAcceptsMultipleCampaignsAndKeepsExactDemandUnknown(t *testing.T) {
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first.json")
	secondPath := filepath.Join(directory, "second.json")
	out := filepath.Join(directory, "collection.json")
	for i, path := range []string{firstPath, secondPath} {
		evidence := campaign.Result{
			SchemaVersion: campaign.CurrentSchemaVersion, Scenario: "offline", BaseUsers: []int{2, 4}[i],
			Target: "http://127.0.0.1:18080", Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Budget: "2m20s",
			Repeats: 2, Outcome: "ABORTED", Trials: []campaign.Trial{
				{Number: 1, Outcome: "ABORTED", AbortReason: "The campaign was interrupted by its caller."},
			},
		}
		if err := report.WriteAnyJSON(path, evidence); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"audit", "--campaign", firstPath, "--campaign", secondPath, "--peak-users", "3", "--out", out}
	if exit := Run(args, &stdout, &stderr); exit != 0 {
		t.Fatalf("multi-level audit exit: %d %s", exit, stderr.String())
	}
	for _, expected := range []string{"CAMPAIGN LEVEL AUDIT (OFFLINE)", "NOT_COMPARABLE", "2 VUs", "4 VUs", "not interpolated", "No traffic was generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("missing %q in output: %s", expected, stdout.String())
		}
	}
	rendered := strings.Replace(stdout.String(), "Audit JSON: "+out+"\n", "", 1)
	golden, err := os.ReadFile(filepath.Join("testdata", "audit_collection.golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	goldenText := strings.ReplaceAll(string(golden), "\r\n", "\n")
	if rendered != goldenText {
		t.Fatalf("campaign collection CLI output changed:\n%s", rendered)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var assessment struct {
		State          string `json:"state"`
		AssessmentType string `json:"assessment_type"`
		Levels         []struct {
			TestedUsers int `json:"tested_users"`
		} `json:"levels"`
		Demand *struct {
			TargetUsers int    `json:"target_users"`
			State       string `json:"state"`
		} `json:"demand"`
	}
	if err := json.Unmarshal(raw, &assessment); err != nil {
		t.Fatal(err)
	}
	if assessment.AssessmentType != "CAMPAIGN_LEVEL_COLLECTION" || assessment.State != "NOT_COMPARABLE" || len(assessment.Levels) != 2 || assessment.Levels[0].TestedUsers != 2 || assessment.Levels[1].TestedUsers != 4 {
		t.Fatalf("machine-readable level collection is malformed: %+v", assessment)
	}
	if assessment.Demand == nil || assessment.Demand.TargetUsers != 3 || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("missing exact-level demand limitation: %+v", assessment.Demand)
	}
}

func TestAuditCommandBoundsRepeatedCampaignArguments(t *testing.T) {
	args := []string{"audit"}
	for i := 0; i < 21; i++ {
		args = append(args, "--campaign", "unused.json")
	}
	var stdout, stderr bytes.Buffer
	if exit := Run(args, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "at most 20") {
		t.Fatalf("oversized campaign collection was accepted: exit=%d stderr=%s", exit, stderr.String())
	}
}
