package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/selfcalibration"
)

// startLoopbackTarget serves a synthetic five-step application on loopback.
func startLoopbackTarget(t *testing.T) string {
	t.Helper()
	routes := []calibration.MirrorRoute{}
	prefixes := []string{}
	for _, name := range []string{"home", "list", "detail", "search", "profile"} {
		routes = append(routes, calibration.MirrorRoute{Method: "GET", PathPrefix: "/" + name, Status: 200, ResponseBytes: 8 << 10, DelayMS: 10})
		prefixes = append(prefixes, "ok")
	}
	target, err := selfcalibration.StartMirror(routes, prefixes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })
	return target.BaseURL()
}

func writeSelfCalibrationScenario(t *testing.T, dir, baseURL string, users int) string {
	t.Helper()
	steps := []map[string]any{}
	for _, name := range []string{"home", "list", "detail", "search", "profile"} {
		steps = append(steps, map[string]any{"name": name, "method": "GET", "path": "/" + name, "must_contain": "ok"})
	}
	scenario := map[string]any{"name": "Synthetic loopback workload", "base_url": baseURL, "users": users, "duration": "2s",
		"request_timeout": "2s", "max_rps": 100, "think_time": "50ms", "p95_limit_ms": 500, "max_error_rate": 0.05,
		"network_baseline_skip_reason": "loopback_test_fixture", "workload_revision": "w1", "application_revision": "a1", "environment": "loopback", "steps": steps}
	path := filepath.Join(dir, "scenario.json")
	b, _ := json.Marshal(scenario)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func calibrateForTest(t *testing.T, scenarioPath, out string, extra ...string) {
	t.Helper()
	previous := selfcalibration.MinLevelDuration
	selfcalibration.MinLevelDuration = time.Second
	t.Cleanup(func() { selfcalibration.MinLevelDuration = previous })
	var stdout, stderr bytes.Buffer
	args := append([]string{"calibrate", "--config", scenarioPath, "--users", "2", "--level-duration", "1500ms", "--response-bytes", "8192", "--server-delay", "10ms", "--out", out}, extra...)
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("calibrate failed (%d): %s %s", code, stdout.String(), stderr.String())
	}
}

func runForTest(t *testing.T, scenarioPath, out string, extra ...string) (int, string, report.Result) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"run", "--config", scenarioPath, "--authorized", "--out", out}, extra...)
	code := Run(args, &stdout, &stderr)
	var result report.Result
	if b, err := os.ReadFile(out); err == nil {
		_ = json.Unmarshal(b, &result)
	}
	return code, stdout.String() + stderr.String(), result
}

func TestSelfCalibrationMovesMatchedCleanRunFromLimitedToValid(t *testing.T) {
	dir := t.TempDir()
	scenario := writeSelfCalibrationScenario(t, dir, startLoopbackTarget(t), 2)
	_, _, uncalibrated := runForTest(t, scenario, filepath.Join(dir, "plain.json"))
	if uncalibrated.Quality == nil || uncalibrated.Quality.State != report.QualityLimited {
		t.Fatalf("uncalibrated run should be LIMITED: %+v", uncalibrated.Quality)
	}
	artifact := filepath.Join(dir, "selfcal.json")
	calibrateForTest(t, scenario, artifact)
	code, output, calibrated := runForTest(t, scenario, filepath.Join(dir, "calibrated.json"), "--calibration", artifact, "--require-calibration")
	if code != 0 || calibrated.ScenarioCalibration == nil {
		t.Fatalf("calibrated run failed (%d): %s", code, output)
	}
	if calibrated.Quality == nil || calibrated.Quality.State != report.QualityValid {
		t.Fatalf("matched clean run should be VALID_WITHIN_ENVELOPE: %+v", calibrated.Quality)
	}
	// The saved report is self-verifying: offline readers recompute the
	// same verdict from the embedded artifact.
	saved, err := insights.ReadRun(filepath.Join(dir, "calibrated.json"))
	if err != nil {
		t.Fatal(err)
	}
	if report.AssessQuality(saved).State != report.QualityValid {
		t.Fatal("offline recomputation disagrees")
	}
	// Tampering with the embedded level fingerprint drops back to LIMITED.
	saved.ScenarioCalibration.Levels[0].WorkloadFingerprint = "sha256:" + strings.Repeat("0", 64)
	if report.AssessQuality(saved).State != report.QualityLimited {
		t.Fatal("a mismatched fingerprint must not qualify the run")
	}
}

func TestRequireSelfCalibrationBlocksMismatchedWorkloadAndLevel(t *testing.T) {
	dir := t.TempDir()
	target := startLoopbackTarget(t)
	scenario := writeSelfCalibrationScenario(t, dir, target, 2)
	artifact := filepath.Join(dir, "selfcal.json")
	calibrateForTest(t, scenario, artifact)

	// Different users level.
	other := writeSelfCalibrationScenario(t, t.TempDir(), target, 3)
	if code, output, _ := runForTest(t, other, filepath.Join(dir, "a.json"), "--calibration", artifact, "--require-calibration"); code != 2 || !strings.Contains(output, "RUN BLOCKED") {
		t.Fatalf("uncalibrated level must block (%d): %s", code, output)
	}
	// Different workload shape (one step removed).
	var raw map[string]any
	b, _ := os.ReadFile(scenario)
	_ = json.Unmarshal(b, &raw)
	raw["steps"] = raw["steps"].([]any)[:4]
	changed := filepath.Join(t.TempDir(), "changed.json")
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(changed, b, 0o600)
	if code, output, _ := runForTest(t, changed, filepath.Join(dir, "b.json"), "--calibration", artifact, "--require-calibration"); code != 2 || !strings.Contains(output, "fingerprint") {
		t.Fatalf("mismatched fingerprint must block (%d): %s", code, output)
	}
	// Without --require-calibration the run proceeds but stays LIMITED.
	_, _, limited := runForTest(t, other, filepath.Join(dir, "c.json"), "--calibration", artifact)
	if limited.Quality == nil || limited.Quality.State != report.QualityLimited {
		t.Fatalf("mismatched calibration must leave the run LIMITED: %+v", limited.Quality)
	}
}

func TestStaleSelfCalibrationIsRejected(t *testing.T) {
	dir := t.TempDir()
	scenario := writeSelfCalibrationScenario(t, dir, startLoopbackTarget(t), 2)
	artifact := filepath.Join(dir, "selfcal.json")
	calibrateForTest(t, scenario, artifact)
	record, err := calibration.LoadScenarioRecord(artifact)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-30 * time.Hour)
	record.StartedAt = old.Format(time.RFC3339Nano)
	record.FinishedAt = old.Add(time.Minute).Format(time.RFC3339Nano)
	if err := report.WriteAnyJSON(artifact, record); err != nil {
		t.Fatal(err)
	}
	code, output, _ := runForTest(t, scenario, filepath.Join(dir, "stale.json"), "--calibration", artifact, "--require-calibration")
	if code != 2 || !strings.Contains(output, "24-hour") {
		t.Fatalf("stale artifact must block (%d): %s", code, output)
	}
	_, _, result := runForTest(t, scenario, filepath.Join(dir, "stale-attached.json"), "--calibration", artifact)
	if result.Quality == nil || result.Quality.State != report.QualityLimited {
		t.Fatalf("stale calibration must not qualify a run: %+v", result.Quality)
	}
}

func TestAuditConsumesSelfCalibratedCampaign(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a bounded 40-second loopback campaign")
	}
	dir := t.TempDir()
	scenario := writeSelfCalibrationScenario(t, dir, startLoopbackTarget(t), 4)
	artifact := filepath.Join(dir, "selfcal.json")
	calibrateForTest(t, scenario, artifact, "--users", "4")
	campaignOut := filepath.Join(dir, "campaign.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"campaign", "--config", scenario, "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--stability-max-p95-increase-ms", "100",
		"--calibration", artifact, "--require-calibration", "--authorized", "--out", campaignOut}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("campaign failed (%d): %s %s", code, stdout.String(), stderr.String())
	}
	auditOut := filepath.Join(dir, "audit.json")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"audit", "--campaign", campaignOut, "--peak-users", "4", "--out", auditOut}, &stdout, &stderr); code != 0 {
		t.Fatalf("audit failed (%d): %s %s", code, stdout.String(), stderr.String())
	}
	raw, err := os.ReadFile(auditOut)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		State      string `json:"state"`
		Confidence struct {
			Measurement string `json:"measurement_confidence"`
		} `json:"confidence_dimensions"`
	}
	if err := json.Unmarshal(raw, &audit); err != nil {
		t.Fatal(err)
	}
	// Every phase recomputes to VALID_WITHIN_ENVELOPE from the embedded
	// artifact, so the audit's measurement dimension is the controlled
	// envelope instead of LIMITED. The demand verdict still depends on its
	// separate statistical gates, which generator calibration does not touch.
	if audit.Confidence.Measurement != "CONTROLLED_ENVELOPE_ONLY" || strings.Contains(stdout.String(), "measurement LIMITED") {
		t.Fatalf("audit did not consume the self-calibration: state=%s measurement=%s\n%s", audit.State, audit.Confidence.Measurement, stdout.String())
	}
}

func TestExtendedRunAcceptsMatchingSelfCalibrationAndRefusesWithout(t *testing.T) {
	base, hits := extendedTestTarget(t)
	configPath := writeExtendedScenario(t, base, 60, "1s")
	previous := selfcalibration.MinLevelDuration
	selfcalibration.MinLevelDuration = time.Second
	t.Cleanup(func() { selfcalibration.MinLevelDuration = previous })
	dir := t.TempDir()
	matching := filepath.Join(dir, "selfcal-60.json")
	other := filepath.Join(dir, "selfcal-55.json")
	for _, c := range []struct{ users, out string }{{"60", matching}, {"55", other}} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"calibrate", "--config", configPath, "--users", c.users, "--level-duration", "1500ms", "--response-bytes", "2",
			"--extended-envelope", "--out", c.out}, &stdout, &stderr); code != 0 {
			t.Fatalf("calibrate %s VUs failed (%d): %s %s", c.users, code, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(extendedArgs(configPath, other), &stdout, &stderr); code != 2 || hits.Load() != 0 || !strings.Contains(stderr.String(), "EXTENDED RUN BLOCKED") {
		t.Fatalf("an artifact without the exact level must refuse the extended run (%d, hits %d): %s %s", code, hits.Load(), stdout.String(), stderr.String())
	}
	out := filepath.Join(dir, "run.json")
	stdout.Reset()
	stderr.Reset()
	Run(extendedArgs(configPath, matching, "--out", out), &stdout, &stderr)
	if hits.Load() == 0 || !strings.Contains(stdout.String(), "calibration coverage READY") {
		t.Fatalf("matching self-calibration did not admit the extended run: %s %s", stdout.String(), stderr.String())
	}
	r, err := insights.ReadRun(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.ScenarioCalibration == nil || r.Envelope == nil || r.Envelope.CalibrationCoverage != "READY" {
		t.Fatalf("extended run did not record the self-calibration: %+v", r.Envelope)
	}
}
