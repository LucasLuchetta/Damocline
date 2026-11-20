package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func extendedTestTarget(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func writeExtendedScenario(t *testing.T, base string, users int, duration string) string {
	t.Helper()
	s := config.Scenario{Name: "extended synthetic", BaseURL: base, Mode: "closed", Users: users, Duration: duration, Timeout: "8s", MaxRPS: 150, ConnectionPolicy: "reuse", P95LimitMS: 2000, MaxErrorRate: 0.1,
		NetworkBaselineSkipReason: "synthetic-loopback-test", Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubExtendedCoverage(t *testing.T, status string) {
	t.Helper()
	previous := assessExtendedCoverage
	assessExtendedCoverage = func(config.Scenario, calibration.Record, calibration.Runtime, time.Time) calibration.ExtendedCoverage {
		return calibration.ExtendedCoverage{Status: status, Reason: "test stub"}
	}
	t.Cleanup(func() { assessExtendedCoverage = previous })
}

func extendedArgs(configPath, calibrationPath string, extra ...string) []string {
	return append([]string{"run", "--config", configPath, "--authorized", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab", "--calibration", calibrationPath, "--require-calibration"}, extra...)
}

func TestExtendedRunRefusedWithoutOptIn(t *testing.T) {
	base, hits := extendedTestTarget(t)
	configPath := writeExtendedScenario(t, base, 60, "1s")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--config", configPath, "--authorized"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "users must be between 1 and 50") {
		t.Fatalf("standard envelope accepted 60 VUs: code=%d stderr=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run([]string{"run", "--config", configPath, "--authorized", "--extended-envelope", "--target-class", "lab", "--calibration", "x.json", "--require-calibration"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--extended-load-authorized") {
		t.Fatalf("extended envelope accepted without acknowledgement: code=%d stderr=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run([]string{"run", "--config", configPath, "--authorized", "--target-class", "lab"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "require --extended-envelope") {
		t.Fatalf("extended options accepted without opt-in: code=%d stderr=%s", code, stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("refused runs sent %d requests", hits.Load())
	}
}

func TestExtendedRunRefusedWithoutCalibrationCoverage(t *testing.T) {
	base, hits := extendedTestTarget(t)
	configPath := writeExtendedScenario(t, base, 60, "1s")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--config", configPath, "--authorized", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--require-calibration") {
		t.Fatalf("extended envelope accepted without calibration: code=%d stderr=%s", code, stderr.String())
	}
	stderr.Reset()
	// A real, fresh calibration that has no exact 60-VU profile.
	calibrationPath := writePassingCalibration(t, nil)
	if code := Run(extendedArgs(configPath, calibrationPath), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "EXTENDED RUN BLOCKED") {
		t.Fatalf("uncovered extended level was not blocked: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("blocked extended run sent %d requests", hits.Load())
	}
}

func TestExtendedRunRefusedAboveHardMaximum(t *testing.T) {
	base, hits := extendedTestTarget(t)
	stubExtendedCoverage(t, "READY")
	calibrationPath := writePassingCalibration(t, nil)
	for _, configPath := range []string{writeExtendedScenario(t, base, 501, "1s"), writeExtendedScenario(t, base, 60, "15m1s")} {
		var stdout, stderr bytes.Buffer
		if code := Run(extendedArgs(configPath, calibrationPath), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "configuration error") {
			t.Fatalf("hard maximum exceeded: code=%d stderr=%s", code, stderr.String())
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("refused runs sent %d requests", hits.Load())
	}
}

func TestExtendedRunAcceptedWithCoverageRecordsEnvelope(t *testing.T) {
	base, hits := extendedTestTarget(t)
	stubExtendedCoverage(t, "READY")
	calibrationPath := writePassingCalibration(t, nil)
	configPath := writeExtendedScenario(t, base, 60, "1s")
	out := filepath.Join(t.TempDir(), "run.json")
	var stdout, stderr bytes.Buffer
	Run(extendedArgs(configPath, calibrationPath, "--out", out), &stdout, &stderr)
	if hits.Load() == 0 || !strings.Contains(stdout.String(), "calibration coverage READY") {
		t.Fatalf("covered extended run did not execute: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	r, err := insights.ReadRun(out)
	if err != nil {
		t.Fatalf("extended report rejected by reader: %v", err)
	}
	e := r.Envelope
	if r.SchemaVersion != report.EnvelopeSchemaVersion || e == nil || e.Mode != report.EnvelopeModeExtended || !e.ExceedsStandard || e.TargetClass != "lab" || !e.Acknowledged || e.CalibrationCoverage != "READY" || e.MaxUsers != 500 {
		t.Fatalf("extended envelope not recorded: %+v", e)
	}
	if r.Generator == nil || r.Generator.Guardrails == nil || !r.Generator.Guardrails.Enabled() {
		t.Fatalf("extended run did not enforce default generator guardrails: %+v", r.Generator)
	}
	if !strings.Contains(stdout.String(), "produced under the extended envelope") {
		t.Fatalf("insights did not state the extended envelope: %s", stdout.String())
	}
	tampered := r
	tampered.Envelope = &report.Envelope{Mode: report.EnvelopeModeStandard, MaxUsers: 50, MaxRPS: 100, MaxArrivalRate: 100, MaxDurationSeconds: 180}
	if report.ValidEnvelope(tampered) {
		t.Fatal("a 60-VU report relabelled as standard envelope was accepted")
	}
}

func TestExtendedRunGuardrailStopsAsGeneratorLimited(t *testing.T) {
	base, _ := extendedTestTarget(t)
	stubExtendedCoverage(t, "READY")
	calibrationPath := writePassingCalibration(t, nil)
	configPath := writeExtendedScenario(t, base, 60, "5s")
	out := filepath.Join(t.TempDir(), "run.json")
	var stdout, stderr bytes.Buffer
	// Any live Go process exceeds 1 MiB RSS, so the guardrail must trip.
	code := Run(extendedArgs(configPath, calibrationPath, "--max-generator-rss-mb", "1", "--guardrail-samples", "2", "--out", out), &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "GENERATOR_LIMITED") {
		t.Fatalf("guardrail did not stop the run: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	r, err := insights.ReadRun(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "ABORTED" || r.Envelope == nil || r.Envelope.StopReason != report.EnvelopeStopGeneratorLimited || r.Generator.GuardrailTrigger == nil {
		t.Fatalf("generator-limited stop not recorded: outcome=%s envelope=%+v", r.Outcome, r.Envelope)
	}
}
