package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func syntheticHTMLRun(users int, p95 float64, success, failures int, outcome string) report.Result {
	return report.Result{
		Target: "http://127.0.0.1:18080", ElapsedSeconds: 10, Outcome: outcome,
		Metrics:         report.MetricSet{Total: success + failures, Success: success, Failures: failures, P95MS: p95},
		Config:          report.RunConfig{Users: users, Duration: "10s", Mode: "closed", MaxRPS: 200, P95LimitMS: 500, MaxErrorRate: .01},
		NetworkBaseline: &report.NetworkBaseline{Status: "PASS", Planned: 10, Completed: 10, P50MS: 0.4, P95MS: 0.9},
		Generator:       &generatorhealth.Summary{ProcessCPUValid: true, PeakProcessCPUUtilizationRatio: 0.21},
	}
}

func syntheticHTMLInput(kind string) htmlReportInput {
	users, target := 8, 8
	observation := syntheticHTMLRun(8, 210, 800, 0, "PASS")
	auditState, state, passing, failing := "OBSERVED_PASS_ONLY", "GOAL_MET_UNDER_TESTED_CONDITIONS", []int{8}, []int{}
	var blockers []capacityplan.DecisionBlocker
	var breaches []capacityplan.WorkflowBreach
	limiting := []string{}
	switch kind {
	case "failing":
		observation = syntheticHTMLRun(8, 740, 700, 30, "FAIL")
		auditState, state, passing, failing = "OBSERVED_FAIL", "INSUFFICIENT_EVIDENCE", []int{}, []int{8}
		blockers = []capacityplan.DecisionBlocker{{Category: capacityplan.BlockerEvidence, Finding: "A required phase breached a configured SLO.", Action: "Investigate the breach, then repeat the campaign at the exact target."}}
		breaches = []capacityplan.WorkflowBreach{{Trial: 1, Phase: "observation", TestedUsers: 8, Workflow: "checkout", Reasons: []string{"p95"}, P95MS: 740, P95LimitMS: 500, Failures: 30, Total: 730, ErrorRate: 30.0 / 730, MaxErrorRate: .01}}
		limiting = []string{"checkout"}
	case "insufficient":
		target = 12
		auditState, state = "OBSERVED_PASS_ONLY", "INSUFFICIENT_EVIDENCE"
		blockers = []capacityplan.DecisionBlocker{{Category: capacityplan.BlockerConfiguration, Finding: "The tested base level (8 VUs) differs from the required target (12 VUs). Test the exact target before deciding on capacity.", Action: "Run the campaign at exactly 12 VUs."}}
	}
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic <local> fixture", Target: "http://127.0.0.1:18080",
		BaseUsers: users, Observation: "10s", Repeats: 2, Outcome: map[bool]string{true: "FAIL", false: "PASS"}[kind == "failing"],
		Trials: []campaign.Trial{
			{Number: 1, Phases: []campaign.Phase{{Name: "observation", Run: observation}}},
			{Number: 2, Phases: []campaign.Phase{{Name: "observation", Run: observation}}},
		},
	}
	explore := &explorer.Result{Scenario: evidence.Scenario, Target: evidence.Target, Probes: []explorer.Probe{
		{Users: 2, Status: "PASS", Runs: []report.Result{syntheticHTMLRun(2, 80, 300, 0, "PASS"), syntheticHTMLRun(2, 85, 298, 0, "PASS")}},
		{Users: 4, Status: "PASS", Runs: []report.Result{syntheticHTMLRun(4, 120, 560, 0, "PASS")}},
		{Users: 16, Status: "FAIL", Runs: []report.Result{syntheticHTMLRun(16, 900, 820, 40, "FAIL")}},
	}}
	highest := 4
	explore.HighestPassingUsers = &highest
	assessment := capacityplan.Assessment{
		SchemaVersion: 6, Auditor: capacityplan.AuditorIdentity{Version: "test", ExecutableSHA256: "sha256:auditor-fixture"},
		Audit: capacityplan.CampaignAudit{
			EvidenceID: "sha256:campaign-fixture", SourceFileSHA256: "sha256:source-fixture", ScenarioFingerprint: "sha256:workload-fixture",
			Scenario: evidence.Scenario, Target: evidence.Target, DeclaredEnvironment: "development", StartedAt: "2026-10-19T12:00:00Z",
			TestedBaseUsers: users, HostType: "NOT_RECORDED", GeneratorRuntimeStatus: "CONSISTENT", GeneratorCapacityStatus: "CONSISTENT",
			PassingLevels: passing, FailingLevels: failing, TestedDuration: "20s", LimitingWorkflows: limiting, WorkflowBreaches: breaches,
			ObservationTrials: []capacityplan.ObservationTrial{{Trial: 1, Requests: observation.Metrics.Total, RequestP95MS: observation.Metrics.P95MS, JourneyP95MS: observation.Metrics.P95MS, SuccessfulJourneysPerSec: 80}},
			Repeatability:     capacityplan.RepeatabilityEvidence{Status: "CONDITIONAL_INTERVAL", EvaluatedTrials: 2, PassingTrials: len(passing) * 2, Lower95: 0.158, Upper95: 1, Method: "EXACT_CLOPPER_PEARSON_BINOMIAL", Assumption: "INDEPENDENT_STATIONARY_REPEAT_TRIALS"},
			Unknowns:          []string{"Physical host identity is not verified."}, State: auditState, MeasurementQuality: report.QualityLimited,
			Findings: []string{"Observed results apply only to the recorded workload."},
			Phases: []capacityplan.PhaseAudit{
				{Trial: 1, Name: "observation", EvidenceState: "PASS_OBSERVED", Measurement: report.QualityLimited, Stability: "NO_LARGE_DRIFT_DETECTED"},
				{Trial: 2, Name: "observation", EvidenceState: "PASS_OBSERVED", Measurement: report.QualityLimited, Stability: "NO_LARGE_DRIFT_DETECTED"},
			},
		},
		Confidence:   capacityplan.ConfidenceDimensions{Measurement: "LIMITED", WorkloadRepresentativeness: "NOT_ASSESSED", Decision: "NOT_ESTABLISHED", Findings: []string{"The workload mix is not independently validated."}},
		DemandInputs: &capacitygoal.Goal{PeakUsers: target},
		Demand:       &capacityplan.Result{State: state, TargetUsers: target, TestedUsers: users, Blockers: blockers},
		State:        state,
	}
	return htmlReportInput{Assessment: assessment, Source: "campaign.json", Evidence: evidence, Explore: explore, ExploreSource: "explore.json", ExploreFileSHA256: "sha256:explore-fixture", ActionsPerUserMinute: map[bool]float64{true: 4}[kind == "passing"]}
}

func TestCapacityHTMLReportGoldens(t *testing.T) {
	cases := []struct {
		kind     string
		expected []string
		absent   []string
	}{
		{"passing", []string{"DEMAND MET UNDER TESTED CONDITIONS", "The highest approved level is 8 modeled active users", ">8 modeled active users</span>", "assumption", "about 1200.0 people"}, []string{"none approved", "INSUFFICIENT EVIDENCE"}},
		{"failing", []string{"DEMAND NOT MET", "was not met", "none approved", "checkout", "A breach location is not a root cause", "No number in this report"}, []string{"DEMAND MET", "Declared usage assumption"}},
		{"insufficient", []string{"INSUFFICIENT EVIDENCE: NO CAPACITY DECISION", "neither confirmed nor refuted", "none approved", "No number in this report is an approved capacity", "Run the campaign at exactly 12 VUs."}, []string{"DEMAND MET", "Declared usage assumption"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := renderCampaignCapacityHTML(syntheticHTMLInput(tc.kind))
			if again := renderCampaignCapacityHTML(syntheticHTMLInput(tc.kind)); again != got {
				t.Fatal("HTML report is not deterministic")
			}
			for _, expected := range append(tc.expected, "<svg", "measured fact", "hypothesis", "unknown", "--html-out", "sha256:explore-fixture", "synthetic &lt;local&gt; fixture", "prefers-color-scheme:dark", "@media print") {
				if !strings.Contains(got, expected) {
					t.Fatalf("HTML report omits %q", expected)
				}
			}
			for _, absent := range append(tc.absent, "<script", "src=\"http", "href=\"http", "url(", "<local>") {
				if strings.Contains(got, absent) {
					t.Fatalf("HTML report unexpectedly contains %q", absent)
				}
			}
			path := filepath.Join("testdata", "capacity_report_"+tc.kind+".golden.html")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(got), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Fatalf("HTML report changed; rerun with UPDATE_GOLDEN=1 after review")
			}
		})
	}
}

func TestAuditHTMLOutWritesPrivateReportWithoutOverwriting(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "campaign.json")
	htmlPath := filepath.Join(directory, "capacity.html")
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, DriftBaseline: campaign.DriftBaselineSkipLightLeadingWindow, ExecutionState: "COMPLETED", Stabilization: "0s", Cooldown: "0s", StabilityPolicy: defaultCampaignStabilityPolicy(), Objectives: defaultCampaignObjectives(),
		Scenario: "synthetic local fixture", BaseUsers: 2, Target: "http://127.0.0.1:18080", StartedAt: "2026-01-02T03:04:05Z", Warmup: "0s", Observation: "10s", Recovery: "10s", Repeats: 2, Outcome: "INCONCLUSIVE",
		Trials: []campaign.Trial{{RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 1, Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}, {RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Number: 2, Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}},
	}
	if err := report.WriteAnyJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	args := []string{"audit", "--campaign", path, "--peak-users", "2", "--html-out", htmlPath}
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("audit --html-out failed: %d %s", code, stderr.String())
	}
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{"<!DOCTYPE html>", "INSUFFICIENT EVIDENCE: NO CAPACITY DECISION", "none approved", "Decision blockers and remediation"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("HTML report omits %q", expected)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(htmlPath); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("HTML report mode is not 0600: %v %v", info, err)
		}
	}
	stderr.Reset()
	if code := Run(args, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("HTML report overwrote an existing file: %d %s", code, stderr.String())
	}
	for _, bad := range [][]string{
		{"audit", "--campaign", path, "--explore-file", path},
		{"audit", "--campaign", path, "--campaign", path, "--html-out", filepath.Join(directory, "multi.html")},
		{"audit", "--campaign", path, "--html-out", filepath.Join(directory, "bad.html"), "--actions-per-user-minute", "-1"},
	} {
		if code := Run(bad, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
			t.Fatalf("invalid HTML options accepted: %v", bad)
		}
	}
}
