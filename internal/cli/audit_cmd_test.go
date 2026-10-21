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

func defaultCampaignObjectives() *campaign.ObjectiveContract {
	policy := campaign.DefaultStabilityPolicy()
	return &campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 1000,
		MaxJourneyErrorRate: 0.1, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MaxDroppedArrivals: 0,
		MinimumObservationWindows: policy.MinimumWindows, MinimumSamplesPerWindow: policy.MinimumSamplesPerWindow, WindowDuration: "AUTO_PER_PHASE"}
}

func TestAuditCommandIsOfflineAndProducesMachineReadableEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	out := filepath.Join(t.TempDir(), "audit.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Scenario: "offline", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2, Outcome: "INCONCLUSIVE",
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
		SchemaVersion int `json:"schema_version"`
		Auditor       struct {
			Version          string `json:"version"`
			ExecutableSHA256 string `json:"executable_sha256"`
		} `json:"auditor"`
		State string `json:"state"`
		Audit struct {
			SchemaVersion    int    `json:"schema_version"`
			State            string `json:"state"`
			EvidenceID       string `json:"evidence_id"`
			SourceFileSHA256 string `json:"source_file_sha256"`
		} `json:"audit"`
		Confidence struct {
			Measurement string `json:"measurement_confidence"`
			Workload    string `json:"workload_representativeness"`
			Decision    string `json:"decision_confidence"`
		} `json:"confidence_dimensions"`
		Demand *struct {
			TargetUsers int `json:"target_users"`
		} `json:"demand"`
		DemandInputs *struct {
			PeakUsers      int     `json:"peak_users"`
			GrowthPercent  float64 `json:"growth_percent"`
			ReservePercent float64 `json:"reserve_percent"`
		} `json:"demand_inputs"`
	}
	if err := json.Unmarshal(raw, &audit); err != nil || audit.SchemaVersion != 6 || audit.Auditor.Version != Version || !validAuditorSHA256(audit.Auditor.ExecutableSHA256) || audit.Audit.SchemaVersion != campaign.CurrentSchemaVersion || audit.Audit.State != "INCONCLUSIVE" || audit.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("invalid audit JSON: %+v %v", audit, err)
	}
	if audit.Audit.EvidenceID == "" || !validSHA256EvidenceID(audit.Audit.SourceFileSHA256) || audit.Demand == nil || audit.Demand.TargetUsers != 2 || audit.DemandInputs == nil || audit.DemandInputs.PeakUsers != 2 || audit.DemandInputs.GrowthPercent != 0 || audit.DemandInputs.ReservePercent != 0 {
		t.Fatalf("combined assessment is missing traceability or demand result: %+v", audit)
	}
	if audit.Confidence.Workload != "NOT_ASSESSED" || audit.Confidence.Decision != "NOT_ESTABLISHED" || audit.Confidence.Measurement != "LIMITED" {
		t.Fatalf("combined assessment collapsed confidence dimensions: %+v", audit.Confidence)
	}
}

func TestAuditReportOutContainsExecutiveTechnicalReplayWithoutConflictingVerdicts(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "campaign.json")
	reportPath := filepath.Join(directory, "capacity.md")
	assessmentPath := filepath.Join(directory, "assessment.json")
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Scenario: "synthetic local fixture", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Outcome: "INCONCLUSIVE"}},
	}
	if err := report.WriteAnyJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	expectedID := capacityplan.AuditCampaign(evidence).EvidenceID
	auditor, err := currentAuditorIdentity()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"audit", "--campaign", path, "--expect-evidence-id", expectedID, "--expect-campaign-file-sha256", campaignFileSHA256(t, path), "--expect-auditor-sha256", auditor.ExecutableSHA256, "--peak-users", "8", "--growth-percent", "25", "--reserve-percent", "10", "--out", assessmentPath, "--report-out", reportPath}
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("audit report command failed: %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	fileSHA := campaignFileSHA256(t, path)
	for _, expected := range []string{"## Executive summary", "## Technical evidence", "Predeclared objectives", "maximum journey error rate 10.00000%", "Completed-work ratio: 100%", "maximum dropped arrivals: 0", "peak 8 active users, growth 25%, reserve 10%", "Calculated target:", "Campaign evidence ID", "Campaign source file SHA-256", "Auditor version", auditor.Version, "Auditor executable SHA-256", auditor.ExecutableSHA256, "Offline replay", "--expect-evidence-id '" + expectedID + "'", "--expect-campaign-file-sha256 '" + fileSHA + "'", "--expect-auditor-sha256 '" + auditor.ExecutableSHA256 + "'", "--peak-users 8", "--growth-percent 25", "--reserve-percent 10", "canonical decoded campaign fields", "exact input bytes", "Neither digest is a signature", "INCONCLUSIVE", "INSUFFICIENT_EVIDENCE"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("capacity report omits %q: %s", expected, content)
		}
	}
	assessmentData, err := os.ReadFile(assessmentPath)
	if err != nil {
		t.Fatal(err)
	}
	var assessment struct {
		State string `json:"state"`
		Audit struct {
			State            string `json:"state"`
			SourceFileSHA256 string `json:"source_file_sha256"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(assessmentData, &assessment); err != nil {
		t.Fatal(err)
	}
	if assessment.State != "INSUFFICIENT_EVIDENCE" || assessment.Audit.State != "INCONCLUSIVE" || assessment.Audit.SourceFileSHA256 != fileSHA || !strings.Contains(content, "**Assessment:** "+assessment.State) || !strings.Contains(content, "**Observed campaign:** "+assessment.Audit.State) {
		t.Fatalf("Markdown and JSON used different assessment verdicts: markdown=%s json=%+v", content, assessment)
	}
	assessmentIndex := strings.Index(content, "**Assessment:** INSUFFICIENT_EVIDENCE")
	observedIndex := strings.Index(content, "**Observed campaign:** INCONCLUSIVE")
	if assessmentIndex < 0 || observedIndex < 0 || assessmentIndex > observedIndex {
		t.Fatal("executive report state and observed campaign state were reordered or conflated")
	}
	var repeated bytes.Buffer
	if code := Run(args, &repeated, &stderr); code != 2 || !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("report output replaced an existing artifact: code=%d stderr=%s", code, stderr.String())
	}
}

func TestAuditCommandRejectsChangedCampaignBeforeWritingAssessment(t *testing.T) {
	directory := t.TempDir()
	campaignPath := filepath.Join(directory, "campaign.json")
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic local fixture", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2,
		Outcome: "INCONCLUSIVE", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Outcome: "INCONCLUSIVE"}},
	}
	if err := report.WriteAnyJSON(campaignPath, evidence); err != nil {
		t.Fatal(err)
	}
	expectedID := capacityplan.AuditCampaign(evidence).EvidenceID
	evidence.Notes = []string{"changed after report generation"}
	changed, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(campaignPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	whitespacePath := filepath.Join(directory, "campaign-whitespace-only.json")
	whitespace := append([]byte(" \n"), changed...)
	if err := os.WriteFile(whitespacePath, whitespace, 0600); err != nil {
		t.Fatal(err)
	}
	var unchanged campaign.Result
	if err := json.Unmarshal(whitespace, &unchanged); err != nil || capacityplan.AuditCampaign(unchanged).EvidenceID != capacityplan.AuditCampaign(evidence).EvidenceID {
		t.Fatalf("whitespace-only encoding changed decoded-model evidence identity: err=%v", err)
	}
	assessmentPath := filepath.Join(directory, "assessment.json")
	reportPath := filepath.Join(directory, "capacity.md")
	var stdout, stderr bytes.Buffer
	args := []string{"audit", "--campaign", campaignPath, "--expect-evidence-id", expectedID, "--out", assessmentPath, "--report-out", reportPath}
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "campaign evidence ID check failed") || !strings.Contains(stderr.String(), "expected "+expectedID) {
		t.Fatalf("changed source campaign was not rejected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, output := range []string{assessmentPath, reportPath} {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("ID mismatch left an assessment artifact at %s: err=%v", output, err)
		}
	}
}

func TestAuditCommandRejectsChangedSourceBytesBeforeWritingAssessment(t *testing.T) {
	directory := t.TempDir()
	campaignPath := filepath.Join(directory, "campaign.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Scenario: "synthetic local fixture", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Outcome: "INCONCLUSIVE"}},
	}
	original, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(campaignPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	expectedID := capacityplan.AuditCampaign(evidence).EvidenceID
	expectedFileSHA := campaignFileSHA256(t, campaignPath)
	changedBytes := append([]byte("  \n"), original...)
	if err := os.WriteFile(campaignPath, changedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var decoded campaign.Result
	if err := json.Unmarshal(changedBytes, &decoded); err != nil || capacityplan.AuditCampaign(decoded).EvidenceID != expectedID {
		t.Fatalf("byte-only change should preserve semantic ID: err=%v", err)
	}
	assessmentPath := filepath.Join(directory, "assessment.json")
	reportPath := filepath.Join(directory, "capacity.md")
	var stdout, stderr bytes.Buffer
	args := []string{"audit", "--campaign", campaignPath, "--expect-evidence-id", expectedID, "--expect-campaign-file-sha256", expectedFileSHA, "--out", assessmentPath, "--report-out", reportPath}
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "campaign file SHA-256 check failed") {
		t.Fatalf("changed source bytes were not rejected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, output := range []string{assessmentPath, reportPath} {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("byte digest mismatch left output at %s: %v", output, err)
		}
	}
}

func TestAuditCommandRejectsDifferentAuditorBeforeReadingCampaignOrWritingOutputs(t *testing.T) {
	directory := t.TempDir()
	assessmentPath := filepath.Join(directory, "assessment.json")
	reportPath := filepath.Join(directory, "capacity.md")
	var stdout, stderr bytes.Buffer
	args := []string{"audit", "--campaign", filepath.Join(directory, "missing.json"), "--expect-auditor-sha256", "sha256:" + strings.Repeat("0", 64), "--out", assessmentPath, "--report-out", reportPath}
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "auditor executable SHA-256 is") {
		t.Fatalf("different auditor binary was not rejected first: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, output := range []string{assessmentPath, reportPath} {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("auditor mismatch left output at %s: %v", output, err)
		}
	}
}

func TestAuditExpectedEvidenceIDsRequireOneValidIDPerCampaign(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "wrong count", args: []string{"--campaign", "one.json", "--campaign", "two.json", "--expect-evidence-id", "sha256:" + strings.Repeat("a", 64)}, want: "provide one ID per --campaign"},
		{name: "malformed digest", args: []string{"--campaign", "one.json", "--expect-evidence-id", "sha256:not-a-digest"}, want: "64 lowercase hexadecimal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(append([]string{"audit"}, test.args...), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("invalid expected evidence ID arguments were accepted: code=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestAuditExpectedCampaignFileSHA256RequiresOneValidHashPerCampaign(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "wrong count", args: []string{"--campaign", "one.json", "--campaign", "two.json", "--expect-campaign-file-sha256", "sha256:" + strings.Repeat("a", 64)}, want: "provide one hash per --campaign"},
		{name: "malformed digest", args: []string{"--campaign", "one.json", "--expect-campaign-file-sha256", "sha256:not-a-digest"}, want: "64 lowercase hexadecimal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(append([]string{"audit"}, test.args...), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("invalid expected file SHA arguments were accepted: code=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func campaignFileSHA256(t *testing.T, path string) string {
	t.Helper()
	_, digest, err := capacityplan.ReadCampaignWithFileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return digest
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
	for _, want := range []string{"2/2 evaluable trials passed", "exact 95% interval 15.8%–100.0%", "not capacity confidence", "request-p95=125.00 ms", "request-errors=2.50%", "completed-journeys=20", "successful-journeys=2.00/s", "Single completed observation phase: request-p95 125.00 ms", "request-rate 4.00/s", "Robust request-p95 repeat summary unavailable"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("observation summary omitted %q: %s", want, output.String())
		}
	}
	for _, misleading := range []string{"independent observation phases", "request-rate 4.00–4.00/s", "MAD 0.00 ms", "band 125.00-125.00 ms"} {
		if strings.Contains(output.String(), misleading) {
			t.Fatalf("single-phase output implied repeat spread with %q: %s", misleading, output.String())
		}
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestAuditCommandRequiresPeakWhenDemandOptionsAreSupplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.json")
	evidence := campaign.Result{SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "offline", BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives()}
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
	markdownOut := filepath.Join(directory, "collection.md")
	expectedIDs := make([]string, 0, 2)
	for i, path := range []string{firstPath, secondPath} {
		budget := campaign.PlannedBudget(campaign.Options{Warmup: 0, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2})
		evidence := campaign.Result{
			SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "offline", BaseUsers: []int{2, 4}[i], StartedAt: "2026-01-02T03:04:05Z", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
			Target: "http://127.0.0.1:18080", Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Budget: budget.String(),
			Repeats: 2, Outcome: "ABORTED", Trials: []campaign.Trial{
				{RunID: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccc"}[i], Number: 1, Outcome: "ABORTED", AbortReason: "The campaign was interrupted by its caller."},
			},
		}
		if err := report.WriteAnyJSON(path, evidence); err != nil {
			t.Fatal(err)
		}
		expectedIDs = append(expectedIDs, capacityplan.AuditCampaign(evidence).EvidenceID)
	}
	expectedFileSHA256s := []string{campaignFileSHA256(t, secondPath), campaignFileSHA256(t, firstPath)}
	var stdout, stderr bytes.Buffer
	args := []string{"audit", "--campaign", secondPath, "--campaign", firstPath, "--expect-evidence-id", expectedIDs[1], "--expect-evidence-id", expectedIDs[0], "--expect-campaign-file-sha256", expectedFileSHA256s[0], "--expect-campaign-file-sha256", expectedFileSHA256s[1], "--peak-users", "3", "--out", out, "--report-out", markdownOut}
	if exit := Run(args, &stdout, &stderr); exit != 0 {
		t.Fatalf("multi-level audit exit: %d %s", exit, stderr.String())
	}
	for _, expected := range []string{"CAMPAIGN LEVEL AUDIT (OFFLINE)", "NOT_COMPARABLE", "Generator environment consistency: UNKNOWN", "Scenario identity consistency: UNKNOWN", "workload revision, application revision, or declared environment is missing", "not interpolated", "No traffic was generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("missing %q in output: %s", expected, stdout.String())
		}
	}
	rendered := strings.Replace(stdout.String(), "Audit JSON: "+out+"\n", "", 1)
	rendered = strings.Replace(rendered, "Executive and technical report: "+markdownOut+"\n", "", 1)
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
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			return
		}
		t.Fatalf("campaign collection CLI output changed:\n%s", rendered)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var assessment struct {
		SchemaVersion int `json:"schema_version"`
		Auditor       struct {
			Version          string `json:"version"`
			ExecutableSHA256 string `json:"executable_sha256"`
		} `json:"auditor"`
		State                      string `json:"state"`
		GeneratorEnvironmentStatus string `json:"generator_environment_consistency"`
		AssessmentType             string `json:"assessment_type"`
		Levels                     []struct {
			TestedUsers int `json:"tested_users"`
			Assessment  struct {
				Audit struct {
					SourceFileSHA256 string `json:"source_file_sha256"`
				} `json:"audit"`
			} `json:"assessment"`
		} `json:"levels"`
		Demand *struct {
			TargetUsers int    `json:"target_users"`
			State       string `json:"state"`
		} `json:"demand"`
		DemandInputs *struct {
			PeakUsers int `json:"peak_users"`
		} `json:"demand_inputs"`
	}
	if err := json.Unmarshal(raw, &assessment); err != nil {
		t.Fatal(err)
	}
	if assessment.SchemaVersion != 5 || assessment.AssessmentType != "CAMPAIGN_LEVEL_COLLECTION" || assessment.State != "NOT_COMPARABLE" || assessment.GeneratorEnvironmentStatus != "UNKNOWN" || assessment.Auditor.Version != Version || !validAuditorSHA256(assessment.Auditor.ExecutableSHA256) || len(assessment.Levels) != 2 || assessment.Levels[0].TestedUsers != 2 || assessment.Levels[1].TestedUsers != 4 || assessment.Levels[0].Assessment.Audit.SourceFileSHA256 != expectedFileSHA256s[1] || assessment.Levels[1].Assessment.Audit.SourceFileSHA256 != expectedFileSHA256s[0] {
		t.Fatalf("machine-readable level collection is malformed: %+v", assessment)
	}
	if assessment.Demand == nil || assessment.Demand.TargetUsers != 3 || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" || assessment.DemandInputs == nil || assessment.DemandInputs.PeakUsers != 3 {
		t.Fatalf("missing exact-level demand limitation: %+v", assessment.Demand)
	}
	markdown, err := os.ReadFile(markdownOut)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"## Executive summary", "## Technical evidence", "Campaigns by tested level", "Generator environment consistency across levels and required phases", "UNKNOWN", "Source file SHA-256", "Auditor executable SHA-256", assessment.Auditor.ExecutableSHA256, "2 | ABORTED", "4 | ABORTED", "--campaign '" + secondPath + "' --expect-evidence-id '" + expectedIDs[1] + "' --expect-campaign-file-sha256 '" + expectedFileSHA256s[0] + "' --campaign '" + firstPath + "' --expect-evidence-id '" + expectedIDs[0] + "' --expect-campaign-file-sha256 '" + expectedFileSHA256s[1] + "' --expect-auditor-sha256 '" + assessment.Auditor.ExecutableSHA256 + "'", "--peak-users 3", firstPath, secondPath} {
		if !strings.Contains(string(markdown), expected) {
			t.Fatalf("multi-level report omits %q: %s", expected, markdown)
		}
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
