package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestGoalCommandUsesSavedExploreWithoutGeneratingTraffic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "capacity.json")
	current := report.Result{
		SchemaVersion: 3, Outcome: "PASS", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: 30, Mode: "closed", MaxRPS: 100, SeparateSessions: true},
		Metrics: report.MetricSet{Total: 30, Success: 30},
		Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.22,
			ProcessSamples: 4, ProcessCPUSamples: 3, ProcessCPUValid: true, PeakProcessRSSBytes: 1024},
	}
	highest := 30
	r := explorer.Result{SchemaVersion: 1, Scenario: "synthetic app", StageDuration: "30s",
		MinUsers: 2, MaxUsers: 50, Repeats: 2, HighestPassingUsers: &highest,
		Probes: []explorer.Probe{{Users: 30, Status: "PASS", Runs: []report.Result{current, current}}},
	}
	if err := report.WriteAnyJSON(file, r); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	exit := Run([]string{"goal", "--file", file, "--peak-users", "20", "--growth-percent", "25", "--reserve-percent", "20"}, &out, &errOut)
	if exit != 0 {
		t.Fatalf("exit %d: %s", exit, errOut.String())
	}
	for _, want := range []string{"DEMAND TO VALIDATE: 30", "SHORT_TRIAL_TARGET_PASSED", "not a certified server limit", "No load was generated", "upgrade is NOT justified"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q: %s", want, out.String())
		}
	}
}

func TestGoalCommandRejectsDuplicateJSONKeys(t *testing.T) {
	file := filepath.Join(t.TempDir(), "duplicate.json")
	if err := os.WriteFile(file, []byte(`{"schema_version":1,"schema_\u0076ersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"goal", "--file", file, "--peak-users", "1"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "ambiguous JSON") {
		t.Fatalf("goal accepted ambiguous JSON: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestGoalCommandKeepsArrivalDemandInScheduledStartsPerSecond(t *testing.T) {
	file := filepath.Join(t.TempDir(), "arrival-capacity.json")
	saved := arrivalexplorer.Result{
		SchemaVersion: 1, Scenario: "synthetic arrival", StageDuration: "10s", Repeats: 2,
		WorkerLimit: 4, MinJourneysPerSecond: 1, MaxJourneysPerSecond: 1,
		Outcome: "PASSED_TO_CONFIGURED_CEILING",
		Probes:  []arrivalexplorer.Probe{{JourneysPerSecond: 1, Status: "PASS_OBSERVED", Runs: []report.Result{}}},
	}
	if err := report.WriteAnyJSON(file, saved); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"goal", "--file", file, "--peak-journeys-per-second", "2", "--growth-percent", "25", "--reserve-percent", "20"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit %d: %s", exit, stderr.String())
	}
	for _, want := range []string{"DEMAND TO VALIDATE: 3 scheduled journey starts/s", "OUTSIDE_TESTED_CEILING", "VUs, admitted journeys, completed journeys, and HTTP requests/s are separate", "No load was generated"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q: %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if exit = Run([]string{"goal", "--file", file, "--peak-users", "2"}, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "VUs cannot be substituted") {
		t.Fatalf("arrival artifact accepted VU demand: exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

func TestGoalCommandWritesOpenArrivalObjectiveReportOffline(t *testing.T) {
	dir := t.TempDir()
	campaignPath := filepath.Join(dir, "arrival-campaign.json")
	contract := campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 500,
		MaxJourneyErrorRate: .1, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MinimumObservationWindows: 3,
		MinimumSamplesPerWindow: 20, WindowDuration: "10s"}
	policy := campaign.DefaultStabilityPolicy()
	saved := arrivalcampaign.Result{SchemaVersion: arrivalcampaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic", Target: "http://127.0.0.1:18080",
		StartedAt: "2026-10-20T12:00:00Z", WorkloadFingerprint: "sha256:synthetic", WorkerLimit: 2, RequestTimeout: "2s", RequestRateCeiling: 20,
		P95LimitMS: 500, MaxErrorRate: .1, MinSamplesPerStep: 1, WindowDuration: "2s", BaseStartsPerSecond: 1,
		Warmup: "0s", Stabilization: "0s", Observation: "10s", Recovery: "10s", Soak: "0s", Cooldown: "0s", Budget: "2m40s", Repeats: 2, Outcome: "ABORTED",
		Objectives: &contract, StabilityPolicy: &policy,
		Trials: []arrivalcampaign.Trial{{Number: 1, RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Outcome: "ABORTED", AbortReason: "The campaign was interrupted by its caller."}}}
	if err := report.WriteAnyJSON(campaignPath, saved); err != nil {
		t.Fatal(err)
	}
	assessmentPath := filepath.Join(dir, "assessment.json")
	markdownPath := filepath.Join(dir, "capacity.md")
	sourceBytes, err := os.ReadFile(campaignPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(sourceBytes))
	auditor, err := currentAuditorIdentity()
	if err != nil {
		t.Fatalf("identify auditor: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"goal", "--file", campaignPath, "--expect-source-sha256", expectedDigest, "--expect-auditor-sha256", auditor.ExecutableSHA256, "--peak-journeys-per-second", "1", "--out", assessmentPath, "--report-out", markdownPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("goal exited %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Open-arrival capacity assessment", "Predeclared objectives", "maximum dropped arrivals: 0", "maximum p95 increase", "SHA-256 of the exact source bytes: `" + expectedDigest + "`", "digest identifies the saved campaign bytes", "Scenario identity consistency:** UNKNOWN", "Auditor executable SHA-256:** `" + auditor.ExecutableSHA256 + "`", "Offline replay", "--expect-source-sha256 '" + expectedDigest + "'", "--expect-auditor-sha256 '" + auditor.ExecutableSHA256 + "'", "--peak-journeys-per-second 1"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("capacity report missing %q:\n%s", want, data)
		}
	}
	assessmentData, err := os.ReadFile(assessmentPath)
	if err != nil || !strings.Contains(string(assessmentData), `"predeclared_objectives"`) || !strings.Contains(string(assessmentData), `"stability_policy"`) || !strings.Contains(string(assessmentData), `"source_evidence_id": "`+expectedDigest+`"`) || !strings.Contains(string(assessmentData), `"auditor_executable_sha256": "`+auditor.ExecutableSHA256+`"`) {
		t.Fatalf("assessment did not retain its replay criteria: err=%v json=%s", err, assessmentData)
	}
	replayAssessmentPath := filepath.Join(dir, "replay-assessment.json")
	replayMarkdownPath := filepath.Join(dir, "replay-capacity.md")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"goal", "--file", campaignPath, "--expect-source-sha256", expectedDigest, "--expect-auditor-sha256", auditor.ExecutableSHA256, "--peak-journeys-per-second", "1", "--out", replayAssessmentPath, "--report-out", replayMarkdownPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("open-arrival report replay failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	replayAssessment, err := os.ReadFile(replayAssessmentPath)
	if err != nil || string(replayAssessment) != string(assessmentData) {
		t.Fatalf("open-arrival assessment replay changed JSON: err=%v first=%s replay=%s", err, assessmentData, replayAssessment)
	}
	replayMarkdown, err := os.ReadFile(replayMarkdownPath)
	if err != nil || string(replayMarkdown) != string(data) {
		t.Fatalf("open-arrival assessment replay changed Markdown: err=%v first=%s replay=%s", err, data, replayMarkdown)
	}

	blockedAssessment := filepath.Join(dir, "must-not-write-assessment.json")
	blockedReport := filepath.Join(dir, "must-not-write-report.md")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"goal", "--file", campaignPath, "--expect-source-sha256", "sha256:" + strings.Repeat("0", 64), "--peak-journeys-per-second", "1", "--out", blockedAssessment, "--report-out", blockedReport}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "source SHA-256 check failed") {
		t.Fatalf("mismatched source digest was accepted: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{blockedAssessment, blockedReport} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("source mismatch wrote %s (stat error %v)", path, err)
		}
	}
	blockedAssessment = filepath.Join(dir, "wrong-auditor-assessment.json")
	blockedReport = filepath.Join(dir, "wrong-auditor-report.md")
	stdout.Reset()
	stderr.Reset()
	wrongAuditor := "sha256:" + strings.Repeat("0", 64)
	if code := Run([]string{"goal", "--file", campaignPath, "--expect-auditor-sha256", wrongAuditor, "--peak-journeys-per-second", "1", "--out", blockedAssessment, "--report-out", blockedReport}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "auditor executable SHA-256") {
		t.Fatalf("mismatched auditor digest was accepted: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{blockedAssessment, blockedReport} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("auditor mismatch wrote %s (stat error %v)", path, err)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"goal", "--file", campaignPath, "--expect-source-sha256", "sha256:invalid", "--peak-journeys-per-second", "1"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "64 lowercase hexadecimal") {
		t.Fatalf("malformed expected source digest was accepted: exit=%d stderr=%s", code, stderr.String())
	}
}

func TestGoalRejectsAuditorPinForNonCampaignArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exploration.json")
	if err := report.WriteAnyJSON(path, map[string]any{"journey_worker_limit": 2}); err != nil {
		t.Fatal(err)
	}
	auditor, err := currentAuditorIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"goal", "--file", path, "--peak-journeys-per-second", "1", "--expect-auditor-sha256", auditor.ExecutableSHA256}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "supported only for open-arrival campaign") {
		t.Fatalf("auditor pin was silently accepted for another artifact: code=%d stderr=%s", code, stderr.String())
	}
}

func TestGoalRejectsMissingMetricCounterInExplorerProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-probe-counter.json")
	saved := `{"schema_version":1,"scenario":"synthetic","target":"http://127.0.0.1","stage_duration":"10s","repeats":2,"min_users":2,"max_users":2,"outcome":"INCONCLUSIVE","probes":[{"users":2,"status":"INCONCLUSIVE","runs":[{"metrics":{"success":0,"failures":0}}]}],"notes":[]}`
	if err := os.WriteFile(path, []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"goal", "--file", path, "--peak-users", "2"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "omits required total counter") {
		t.Fatalf("missing explorer metric counter was not rejected: code=%d stderr=%s", code, stderr.String())
	}
}

func TestGoalCommandRejectsInvalidJSONAndMissingFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if c := Run([]string{"goal"}, &out, &errOut); c != 2 {
		t.Fatalf("missing flags: exit %d", c)
	}
	out.Reset()
	errOut.Reset()
	p := filepath.Join(t.TempDir(), "not-a-report.json")
	if err := report.WriteAnyJSON(p, map[string]any{"hello": "world"}); err != nil {
		t.Fatal(err)
	}
	if c := Run([]string{"goal", "--file", p, "--peak-users", "20"}, &out, &errOut); c != 2 || !strings.Contains(errOut.String(), "invalid or empty capacity exploration") {
		t.Fatalf("invalid JSON schema not rejected: exit %d stderr %s", c, errOut.String())
	}
}
