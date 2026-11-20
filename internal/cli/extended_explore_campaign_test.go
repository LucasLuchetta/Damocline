package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// stubExtendedLevels marks only the listed VU levels READY.
func stubExtendedLevels(t *testing.T, levels ...int) {
	t.Helper()
	previous := assessExtendedCoverage
	assessExtendedCoverage = func(s config.Scenario, _ calibration.Record, _ calibration.Runtime, _ time.Time) calibration.ExtendedCoverage {
		if slices.Contains(levels, s.Users) {
			return calibration.ExtendedCoverage{Status: "READY", Reason: "test stub: exact level self-calibrated"}
		}
		return calibration.ExtendedCoverage{Status: "BLOCKED", Reason: "test stub: level not self-calibrated"}
	}
	t.Cleanup(func() { assessExtendedCoverage = previous })
}

// writePacedScenario keeps loopback request rates far below max_rps so a
// level above 50 VUs is not limited by the client rate cap.
func writePacedScenario(t *testing.T, base string, users int) string {
	t.Helper()
	s := config.Scenario{Name: "extended synthetic", BaseURL: base, Mode: "closed", Users: users, Duration: "10s", Timeout: "5s", MaxRPS: 1000, ThinkTime: "1s",
		ConnectionPolicy: "reuse", P95LimitMS: 2000, MaxErrorRate: 0.1, NetworkBaselineSkipReason: "synthetic-loopback-test",
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func extendedFlags(calibrationPath string) []string {
	return []string{"--authorized", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab", "--calibration", calibrationPath, "--require-calibration"}
}

func TestExtendedExploreRefusedWithoutOptInOrCoverage(t *testing.T) {
	base, hits := extendedTestTarget(t)
	configPath := writePacedScenario(t, base, 40)
	calibrationPath := writePassingCalibration(t, nil)
	cases := []struct {
		name, want string
		args       []string
	}{
		{"standard ceiling", "at most 50", []string{"--authorized"}},
		{"no acknowledgement", "--extended-load-authorized", []string{"--authorized", "--extended-envelope", "--target-class", "lab", "--calibration", calibrationPath, "--require-calibration"}},
		{"production class", "target class", []string{"--authorized", "--extended-envelope", "--extended-load-authorized", "--target-class", "production", "--calibration", calibrationPath, "--require-calibration"}},
		{"no calibration", "--require-calibration", []string{"--authorized", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}},
		{"real calibration without exact levels", "EXTENDED ENVELOPE BLOCKED", extendedFlags(calibrationPath)},
		{"above hard maximum", "at most 500", append(extendedFlags(calibrationPath), "--max-users", "501")},
	}
	for _, tc := range cases {
		args := append([]string{"explore", "--config", configPath, "--min-users", "40", "--max-users", "80", "--stage-duration", "10s"}, tc.args...)
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("%s: code=%d stdout=%s stderr=%s", tc.name, code, stdout.String(), stderr.String())
		}
	}
	// A ladder level (80) without READY coverage blocks before traffic.
	stubExtendedLevels(t, 40)
	var stdout, stderr bytes.Buffer
	if code := Run(append([]string{"explore", "--config", configPath, "--min-users", "40", "--max-users", "80"}, extendedFlags(calibrationPath)...), &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), "80 VUs: calibration coverage BLOCKED") {
		t.Fatalf("uncovered ladder level was not blocked: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	// An exact demand-goal stage is a planned level and must be covered too.
	stubExtendedLevels(t, 40, 80)
	stdout.Reset()
	stderr.Reset()
	if code := Run(append([]string{"explore", "--config", configPath, "--min-users", "40", "--max-users", "80", "--peak-users", "60"}, extendedFlags(calibrationPath)...), &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), "60 VUs: calibration coverage BLOCKED") {
		t.Fatalf("uncovered demand-goal stage was not blocked: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("refused explorations sent %d requests", hits.Load())
	}
}

func TestExtendedExploreAcceptedWithCoverageForEveryLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("runs four 10s loopback stages")
	}
	// A 200ms loopback response keeps 80 closed-loop VUs near 400 req/s,
	// well below the 1000 req/s client cap, so the cap cannot limit the run.
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	configPath := writePacedScenario(t, srv.URL, 40)
	calibrationPath := writePassingCalibration(t, nil)
	stubExtendedLevels(t, 40, 80)
	out := filepath.Join(t.TempDir(), "explore.json")
	var stdout, stderr bytes.Buffer
	args := append([]string{"explore", "--config", configPath, "--min-users", "40", "--max-users", "80", "--repeats", "2", "--stage-duration", "10s", "--out", out}, extendedFlags(calibrationPath)...)
	code := Run(args, &stdout, &stderr)
	if hits.Load() == 0 || !strings.Contains(stdout.String(), "Extended envelope READY for ladder [40 80]") {
		t.Fatalf("covered extended exploration did not run: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := explorer.ValidateArtifactJSON(data); err != nil {
		t.Fatalf("extended exploration artifact rejected: %v", err)
	}
	var saved explorer.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if code != 0 || saved.Outcome != "PASSED_TO_CONFIGURED_CEILING" || len(saved.Probes) != 2 || saved.Probes[1].Users != 80 {
		t.Fatalf("unexpected loopback exploration: code=%d outcome=%s reason=%s probes=%d stdout=%s", code, saved.Outcome, saved.Reason, len(saved.Probes), stdout.String())
	}
	if saved.SchemaVersion != explorer.EnvelopeSchemaVersion || saved.LoadEnvelope == nil || saved.LoadEnvelope.Mode != report.EnvelopeModeExtended || !saved.LoadEnvelope.ExceedsStandard {
		t.Fatalf("envelope not recorded: %+v", saved.LoadEnvelope)
	}
	for _, p := range saved.Probes {
		for _, r := range p.Runs {
			if r.Envelope == nil || r.Envelope.Mode != report.EnvelopeModeExtended || r.Generator == nil || r.Generator.Guardrails == nil || !r.Generator.Guardrails.Enabled() {
				t.Fatalf("probe %d run lacks extended envelope or guardrails", p.Users)
			}
		}
	}
	assessment, err := capacitygoal.Evaluate(saved, capacitygoal.Goal{PeakUsers: 80})
	if err != nil || assessment.State != "SHORT_TRIAL_TARGET_PASSED" || !strings.Contains(strings.Join(assessment.Findings, " "), "above the standard 50 VU ceiling") {
		t.Fatalf("offline goal assessment: %+v %v", assessment, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"goal", "--file", out, "--peak-users", "80"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "extended load envelope") {
		t.Fatalf("goal command did not state the extended envelope: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	tampered := saved
	tampered.LoadEnvelope = report.NewAggregateEnvelope(false, "", 80)
	if _, err := capacitygoal.Evaluate(tampered, capacitygoal.Goal{PeakUsers: 80}); err == nil {
		t.Fatal("80-VU exploration relabelled as standard was accepted")
	}
}

func TestExtendedExploreGuardrailStopIsGeneratorLimited(t *testing.T) {
	base, _ := extendedTestTarget(t)
	configPath := writePacedScenario(t, base, 60)
	calibrationPath := writePassingCalibration(t, nil)
	stubExtendedLevels(t, 60)
	out := filepath.Join(t.TempDir(), "explore.json")
	var stdout, stderr bytes.Buffer
	// Any live Go process exceeds 1 MiB RSS, so the guardrail must trip.
	args := append([]string{"explore", "--config", configPath, "--min-users", "60", "--max-users", "60", "--stage-duration", "10s", "--max-generator-rss-mb", "1", "--guardrail-samples", "2", "--out", out}, extendedFlags(calibrationPath)...)
	if code := Run(args, &stdout, &stderr); code != 1 || !strings.Contains(stdout.String(), "GENERATOR_LIMITED") {
		t.Fatalf("guardrail did not stop the exploration: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var saved explorer.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Outcome != "INCONCLUSIVE" || saved.Probes[0].Reason != explorer.GeneratorLimitedReason || saved.HighestPassingUsers != nil || saved.FirstFailingUsers != nil {
		t.Fatalf("generator-limited probe was not non-passing: %+v", saved)
	}
	assessment, err := capacitygoal.Evaluate(saved, capacitygoal.Goal{PeakUsers: 60})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("generator-limited target stage was not insufficient evidence: %+v %v", assessment, err)
	}
}

func campaignArgs(configPath, out string, extra ...string) []string {
	return append([]string{"campaign", "--config", configPath, "--warmup", "0s", "--observation", "10s", "--recovery", "1s", "--stability-min-samples", "20", "--out", out}, extra...)
}

func TestExtendedCampaignRefusedWithoutOptInOrCoverage(t *testing.T) {
	base, hits := extendedTestTarget(t)
	configPath := writePacedScenario(t, base, 40)
	calibrationPath := writePassingCalibration(t, nil)
	out := filepath.Join(t.TempDir(), "campaign.json")
	cases := []struct {
		name, want string
		args       []string
	}{
		{"standard spike ceiling", "spike-users must be 1-50", []string{"--authorized", "--spike-users", "80", "--spike-duration", "10s"}},
		{"no acknowledgement", "--extended-load-authorized", []string{"--authorized", "--extended-envelope", "--target-class", "staging", "--calibration", calibrationPath, "--require-calibration"}},
		{"guardrail loosened", "may only tighten", append(extendedFlags(calibrationPath), "--max-generator-cpu-percent", "95")},
		{"real calibration without exact levels", "EXTENDED ENVELOPE BLOCKED", append(extendedFlags(calibrationPath), "--spike-users", "80", "--spike-duration", "10s")},
		{"above hard maximum", "spike-users must be 1-500", append(extendedFlags(calibrationPath), "--spike-users", "501", "--spike-duration", "10s")},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(campaignArgs(configPath, out, tc.args...), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("%s: code=%d stdout=%s stderr=%s", tc.name, code, stdout.String(), stderr.String())
		}
	}
	stubExtendedLevels(t, 40)
	var stdout, stderr bytes.Buffer
	if code := Run(campaignArgs(configPath, out, append(extendedFlags(calibrationPath), "--spike-users", "80", "--spike-duration", "10s")...), &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), "80 VUs: calibration coverage BLOCKED") {
		t.Fatalf("uncovered spike level was not blocked: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("refused campaigns sent %d requests", hits.Load())
	}
	// Covered base and spike levels pass the pre-traffic gate.
	stubExtendedLevels(t, 40, 80)
	stdout.Reset()
	stderr.Reset()
	if code := Run(campaignArgs(configPath, out, append(extendedFlags(calibrationPath), "--spike-users", "80", "--spike-duration", "10s", "--dry-run")...), &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "Levels above the standard 50 VU ceiling") || !strings.Contains(stdout.String(), "spike: 10s at 80 VUs") {
		t.Fatalf("covered extended campaign dry run failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("dry run sent %d requests", hits.Load())
	}
}

func TestExtendedCampaignGuardrailStopIsGeneratorLimitedAndAuditable(t *testing.T) {
	base, _ := extendedTestTarget(t)
	configPath := writePacedScenario(t, base, 60)
	calibrationPath := writePassingCalibration(t, nil)
	stubExtendedLevels(t, 60)
	out := filepath.Join(t.TempDir(), "campaign.json")
	var stdout, stderr bytes.Buffer
	code := Run(campaignArgs(configPath, out, append(extendedFlags(calibrationPath), "--max-generator-rss-mb", "1")...), &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "GENERATOR_LIMITED") {
		t.Fatalf("guardrail did not stop the campaign: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var saved campaign.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != campaign.EnvelopeSchemaVersion || saved.LoadEnvelope == nil || !saved.LoadEnvelope.ExceedsStandard || saved.Outcome != "ABORTED" ||
		!strings.HasPrefix(saved.Trials[0].AbortReason, "GENERATOR_LIMITED:") {
		t.Fatalf("generator-limited campaign evidence: schema=%d envelope=%+v outcome=%s trials=%+v", saved.SchemaVersion, saved.LoadEnvelope, saved.Outcome, saved.Trials)
	}
	if err := capacityplan.ValidateCampaignArtifact(data); err != nil {
		t.Fatalf("offline validation rejected the extended campaign: %v", err)
	}
	audit := capacityplan.AuditCampaign(saved)
	findings := strings.Join(audit.Findings, " ")
	if audit.State == "PASS" || len(audit.PassingLevels) != 0 || len(audit.FailingLevels) != 0 ||
		!strings.Contains(findings, "GENERATOR_LIMITED") || !strings.Contains(findings, "above the standard 50 VU ceiling") {
		t.Fatalf("audit did not recompute a generator-limited extended campaign: state=%s passing=%v failing=%v findings=%s", audit.State, audit.PassingLevels, audit.FailingLevels, findings)
	}
	stdout.Reset()
	stderr.Reset()
	Run([]string{"audit", "--campaign", out}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "extended load envelope") {
		t.Fatalf("audit command did not state the extended envelope: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	for name, mutate := range map[string]func(*campaign.Result){
		"envelope removed":    func(r *campaign.Result) { r.LoadEnvelope = nil },
		"standard envelope":   func(r *campaign.Result) { r.LoadEnvelope = report.NewAggregateEnvelope(false, "", 60) },
		"understated levels":  func(r *campaign.Result) { r.LoadEnvelope = report.NewAggregateEnvelope(true, "lab", 40) },
		"guardrails disabled": func(r *campaign.Result) { r.GeneratorGuardrails = nil },
	} {
		tampered := saved
		mutate(&tampered)
		if err := capacityplan.ValidateCampaignEnvelope(tampered); err == nil {
			t.Fatalf("%s: tampered extended campaign envelope was accepted", name)
		}
	}
}
