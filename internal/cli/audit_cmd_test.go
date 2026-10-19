package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func defaultCampaignStabilityPolicy() *campaign.StabilityPolicy {
	policy := campaign.DefaultStabilityPolicy()
	return &policy
}

func TestAuditCommandIsOfflineAndProducesMachineReadableEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	out := filepath.Join(t.TempDir(), "audit.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(),
		Scenario: "offline", BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Outcome: "INCONCLUSIVE"}},
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
		SchemaVersion int    `json:"schema_version"`
		State         string `json:"state"`
		Audit         struct {
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
	if err := json.Unmarshal(raw, &audit); err != nil || audit.SchemaVersion != 2 || audit.Audit.State != "INCONCLUSIVE" || audit.State != "INSUFFICIENT_EVIDENCE" {
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

func TestAuditObservationSummaryOutputKeepsUnitsAndLimitationsExplicit(t *testing.T) {
	var output bytes.Buffer
	audit := capacityplan.CampaignAudit{
		SpreadConsistency: "MATCHED",
		Repeatability: capacityplan.RepeatabilityEvidence{Status: "CONDITIONAL_INTERVAL", EvaluatedTrials: 2, PassingTrials: 2, Lower95: 0.158, Upper95: 1,
			Method: "EXACT_CLOPPER_PEARSON_BINOMIAL", Assumption: "INDEPENDENT_STATIONARY_REPEAT_TRIALS"},
		ObservationTrials: []capacityplan.ObservationTrial{{
			Trial: 1, Requests: 40, RequestP95MS: 125, RequestErrorRate: floatPointer(0.025),
			CompletedJourneys: 20, JourneyP95MS: 240, JourneyErrorRate: floatPointer(0.05),
			SuccessfulJourneysPerSec: 2,
		}},
		ObservationSpread: &campaign.Variability{
			Runs: 1, P95MinMS: 125, P95MaxMS: 125, ErrorRateMin: 0.025, ErrorRateMax: 0.025,
			ThroughputMinRPS: 4, ThroughputMaxRPS: 4,
			SuccessfulJourneysMinPerSec: 2, SuccessfulJourneysMaxPerSec: 2,
		},
	}
	writeObservationEvidence(&output, audit, "")
	for _, want := range []string{"2/2 evaluable trials passed", "exact 95% interval 15.8%–100.0%", "not capacity confidence", "request-p95=125.00 ms", "request-errors=2.50%", "completed-journeys=20", "successful-journeys=2.00/s", "request-rate 4.00–4.00/s", "not a confidence interval"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("observation summary omitted %q: %s", want, output.String())
		}
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestAuditCommandRequiresPeakWhenDemandOptionsAreSupplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "offline", BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy()}
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
		budget := campaign.PlannedBudget(campaign.Options{Warmup: 0, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2})
		evidence := campaign.Result{
			SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "offline", BaseUsers: []int{2, 4}[i], Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(),
			Target: "http://127.0.0.1:18080", Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Budget: budget.String(),
			Repeats: 2, Outcome: "ABORTED", Trials: []campaign.Trial{
				{RunID: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccc"}[i], Number: 1, Outcome: "ABORTED", AbortReason: "The campaign was interrupted by its caller."},
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
	for _, expected := range []string{"CAMPAIGN LEVEL AUDIT (OFFLINE)", "INCONCLUSIVE", "inconclusive levels: [2 4]", "not interpolated", "No traffic was generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("missing %q in output: %s", expected, stdout.String())
		}
	}
	rendered := strings.Replace(stdout.String(), "Audit JSON: "+out+"\n", "", 1)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(filepath.Join("testdata", "audit_collection.golden.txt"), []byte(rendered), 0o600); err != nil {
			t.Fatal(err)
		}
	}
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
	if assessment.AssessmentType != "CAMPAIGN_LEVEL_COLLECTION" || assessment.State != "INCONCLUSIVE" || len(assessment.Levels) != 2 || assessment.Levels[0].TestedUsers != 2 || assessment.Levels[1].TestedUsers != 4 {
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
