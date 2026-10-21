package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestOpenArrivalLoopbackCampaignGoalReportReplaysWithoutTraffic(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	dir := t.TempDir()
	executableName := "damocline"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	executablePath := filepath.Join(dir, executableName)
	build := exec.Command("go", "build", "-o", executablePath, "../../cmd/damocline")
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build CLI executable for subprocess replay: %v\n%s", err, buildOutput)
	}
	runCLI := func(args ...string) (int, string, string) {
		cmd := exec.Command(executablePath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if err == nil {
			return 0, stdout.String(), stderr.String()
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode(), stdout.String(), stderr.String()
		}
		t.Fatalf("run CLI executable: %v", err)
		return -1, stdout.String(), stderr.String()
	}
	executableBytes, err := os.ReadFile(executablePath)
	if err != nil {
		t.Fatalf("read built CLI executable: %v", err)
	}
	auditorSHA256 := fmt.Sprintf("sha256:%x", sha256.Sum256(executableBytes))

	scenarioPath := filepath.Join(dir, "scenario.json")
	scenario := config.Scenario{
		Name: "loopback-arrival", WorkloadRevision: "workload-v1", ApplicationRevision: "fixture-v1", Environment: "development",
		BaseURL: server.URL, Users: 8, Duration: "30s", RampUp: "0s", Timeout: "2s", MaxRPS: 100,
		P95LimitMS: 1000, MaxErrorRate: 0.1, Mode: "arrival", ArrivalRate: 10, MinSamplesPerStep: 1, WindowDuration: "10s",
		Steps: []config.Step{{Name: "health", Method: http.MethodGet, Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenarioPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	campaignPath := filepath.Join(dir, "campaign.json")
	code, stdout, stderr := runCLI("arrival-campaign", "--config", scenarioPath, "--warmup", "0s", "--observation", "30s", "--recovery", "10s", "--repeats", "2", "--peak-journeys-per-second", "10", "--stability-min-samples", "20", "--out", campaignPath, "--authorized")
	if code != 0 && code != 1 {
		t.Fatalf("bounded loopback campaign failed before producing a result: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("bounded loopback campaign completed without reaching its local fixture")
	}
	campaignBytes, err := os.ReadFile(campaignPath)
	if err != nil {
		t.Fatalf("campaign artifact was not retained: %v; stderr=%s", err, stderr)
	}
	var saved arrivalcampaign.Result
	if err := json.Unmarshal(campaignBytes, &saved); err != nil {
		t.Fatalf("decode campaign artifact: %v", err)
	}
	if saved.SchemaVersion != arrivalcampaign.CurrentSchemaVersion || (saved.Outcome != "PASS" && saved.Outcome != "INCONCLUSIVE") || len(saved.Trials) != 2 || len(saved.Trials[0].Phases) == 0 {
		t.Fatalf("campaign did not retain complete bounded phase evidence: schema=%d outcome=%s trials=%+v", saved.SchemaVersion, saved.Outcome, saved.Trials)
	}
	for _, trial := range saved.Trials {
		if trial.Outcome != "PASS" && trial.Outcome != "INCONCLUSIVE" {
			t.Fatalf("loopback campaign did not preserve a terminal phase outcome: trial=%+v", trial)
		}
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			if phase.EvidenceState == "PASS_OBSERVED" {
				if phase.Run.Outcome != "PASS" {
					t.Fatalf("passing loopback phase does not retain a passing run: phase=%+v", phase)
				}
			} else if phase.EvidenceState == "INVALID_DELIVERY" {
				if phase.Run.Outcome != "INCONCLUSIVE" || phase.Run.ArrivalsDropped == 0 {
					t.Fatalf("loopback phase hid or misclassified a delivery shortfall: phase=%+v", phase)
				}
			} else {
				t.Fatalf("loopback phase retained an unexpected evidence state: phase=%+v", phase)
			}
			if phase.Name == "observation" && phase.Stability != "NO_LARGE_DRIFT_DETECTED" {
				t.Fatalf("loopback observation omitted its stability screen: phase=%+v", phase)
			}
			cfg := phase.Run.Config
			if cfg.WorkloadRevision != scenario.WorkloadRevision || cfg.ApplicationRevision != scenario.ApplicationRevision || cfg.Environment != scenario.Environment {
				t.Fatalf("phase lost declared scenario identity: phase=%s config=%+v", phase.Name, cfg)
			}
		}
	}

	sourceDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(campaignBytes))
	assessmentPath := filepath.Join(dir, "assessment.json")
	reportPath := filepath.Join(dir, "capacity.md")
	goalArgs := []string{"goal", "--file", campaignPath, "--expect-source-sha256", sourceDigest, "--expect-auditor-sha256", auditorSHA256,
		"--peak-journeys-per-second", "10", "--out", assessmentPath, "--report-out", reportPath}
	if code, stdout, stderr := runCLI(goalArgs...); code != 0 {
		t.Fatalf("offline open-arrival assessment failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	assessmentBytes, err := os.ReadFile(assessmentPath)
	if err != nil {
		t.Fatalf("assessment JSON was not written: %v", err)
	}
	markdown, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("capacity Markdown was not written: %v", err)
	}
	for _, expected := range []string{
		"Open-arrival capacity assessment", "Scenario identity consistency:** CONSISTENT",
		"Workload revision:** workload-v1", "Application revision:** fixture-v1", "Declared environment:** development",
		"Auditor executable SHA-256:** `" + auditorSHA256 + "`", "--expect-source-sha256 '" + sourceDigest + "'",
		"--expect-auditor-sha256 '" + auditorSHA256 + "'", "--peak-journeys-per-second 10",
	} {
		if !strings.Contains(string(markdown), expected) {
			t.Fatalf("open-arrival report omitted %q:\n%s", expected, markdown)
		}
	}
	if !strings.Contains(string(assessmentBytes), `"scenario_identity_status": "CONSISTENT"`) ||
		!strings.Contains(string(assessmentBytes), `"State": "INSUFFICIENT_EVIDENCE"`) ||
		!strings.Contains(string(assessmentBytes), "measurement quality does not meet the capacity-assessment threshold") ||
		!strings.Contains(string(assessmentBytes), `"auditor_executable_sha256": "`+auditorSHA256+`"`) ||
		!strings.Contains(string(assessmentBytes), `"source_evidence_id": "`+sourceDigest+`"`) {
		t.Fatalf("assessment did not retain identity and replay provenance: %s", assessmentBytes)
	}

	requestsBeforeReplay := requests.Load()
	replayAssessmentPath := filepath.Join(dir, "replayed-assessment.json")
	replayReportPath := filepath.Join(dir, "replayed-capacity.md")
	replayArgs := []string{"goal", "--file", campaignPath, "--expect-source-sha256", sourceDigest, "--expect-auditor-sha256", auditorSHA256,
		"--peak-journeys-per-second", "10", "--out", replayAssessmentPath, "--report-out", replayReportPath}
	if code, stdout, stderr := runCLI(replayArgs...); code != 0 {
		t.Fatalf("offline replay failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	replayedAssessment, err := os.ReadFile(replayAssessmentPath)
	if err != nil || !bytes.Equal(replayedAssessment, assessmentBytes) {
		t.Fatalf("replayed assessment changed JSON: err=%v first=%s replay=%s", err, assessmentBytes, replayedAssessment)
	}
	replayedMarkdown, err := os.ReadFile(replayReportPath)
	if err != nil || !bytes.Equal(replayedMarkdown, markdown) {
		t.Fatalf("replayed assessment changed Markdown: err=%v first=%s replay=%s", err, markdown, replayedMarkdown)
	}
	if got := requests.Load(); got != requestsBeforeReplay {
		t.Fatalf("offline goal replay sent %d loopback request(s)", got-requestsBeforeReplay)
	}

	blockedAssessment := filepath.Join(dir, "blocked-assessment.json")
	blockedReport := filepath.Join(dir, "blocked-capacity.md")
	badSourceArgs := []string{"goal", "--file", campaignPath, "--expect-source-sha256", "sha256:" + strings.Repeat("0", 64),
		"--expect-auditor-sha256", auditorSHA256, "--peak-journeys-per-second", "10", "--out", blockedAssessment, "--report-out", blockedReport}
	if code, _, stderr := runCLI(badSourceArgs...); code != 2 || !strings.Contains(stderr, "source SHA-256 check failed") {
		t.Fatalf("wrong campaign digest was not rejected before replay: code=%d stderr=%s", code, stderr)
	}
	for _, path := range []string{blockedAssessment, blockedReport} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("digest mismatch wrote %s (stat error %v)", path, err)
		}
	}
	if got := requests.Load(); got != requestsBeforeReplay {
		t.Fatalf("rejected offline goal command sent %d loopback request(s)", got-requestsBeforeReplay)
	}
}
