package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestReportExportWritesAllowlistedRedactedRunAndPreservesSource(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "private-run.json")
	output := filepath.Join(dir, "public", "redacted.json")
	secret := "customer-731-secret.example.internal"
	source := `{"schema_version":10,"name":"` + secret + `","target":"https://` + secret + `/private/route","started_at":"2026-04-03T12:30:00Z","elapsed_seconds":2,"observation_seconds":2,"config":{"users":2,"duration":"2s","request_timeout":"1s","max_rps":20,"p95_limit_ms":250,"max_error_rate":0.01,"mode":"closed"},"metrics":{"total":2,"success":2,"failures":0,"p95_ms":35},"steps":[{"name":"checkout-` + secret + `","method":"GET","path":"/private/customer-731","metrics":{"total":2,"success":2,"failures":0}}],"warnings":["` + secret + `"]}`
	if err := os.WriteFile(input, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("export failed: code=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, marker := range []string{secret, "/private/", "customer-731", "checkout-", "2026-04-03", "warnings"} {
		if strings.Contains(text, marker) {
			t.Fatalf("redacted export contains %q: %s", marker, text)
		}
	}
	if !strings.Contains(text, "damocline-redacted-evidence-v1") || !strings.Contains(text, `"total": 2`) || !strings.Contains(text, "step_001") || !strings.Contains(stdout.String(), "not a native assessment artifact") {
		t.Fatalf("export omitted evidence or its limitation: %s | %s", text, stdout.String())
	}
	unchanged, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != source {
		t.Fatal("export modified the source artifact")
	}
}

func TestReportExportRejectsSamePathAndUnsupportedArtifacts(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.json")
	if err := os.WriteFile(input, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(input)
	for _, args := range [][]string{
		{"report", "export", "--file", input, "--out", input},
		{"report", "export", "--file", input, "--out", filepath.Join(dir, "unsupported.json")},
	} {
		var stdout, stderr strings.Builder
		if code := Run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("invalid export request accepted: args=%v code=%d", args, code)
		}
	}
	after, _ := os.ReadFile(input)
	if string(before) != string(after) {
		t.Fatal("rejected export changed the source artifact")
	}
}

func TestReportExportOutputIsSeparateFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.json")
	output := filepath.Join(dir, "export.json")
	if err := os.WriteFile(input, []byte(`{"schema_version":10,"name":"scenario","target":"https://app.example.com","elapsed_seconds":1,"observation_seconds":1,"started_at":"2026-04-03T12:30:00Z","config":{"users":1,"duration":"1s","request_timeout":"1s","max_rps":10,"p95_limit_ms":250,"max_error_rate":0.01,"mode":"closed"},"metrics":{"total":1,"success":1,"failures":0}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("export failed: %s", stderr.String())
	}
	var value map[string]any
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value["format"] != "damocline-redacted-evidence-v1" || value["schema_version"] != nil || value["target"] != nil {
		t.Fatalf("export was not clearly separated from native artifacts: %s", data)
	}
}

func TestReportExportAcceptsClosedExplorerAndKeepsLoadUnit(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "exploration.json"), filepath.Join(dir, "redacted.json")
	secret := "customer-42.private.example"
	run := validExportRun(t, secret, 2, "10s", "closed")
	passed := 2
	saved := explorer.Result{SchemaVersion: 2, Scenario: secret, Target: "https://" + secret, StageDuration: "10s", Repeats: 2, MinUsers: 2,
		MaxUsers: 4, HighestPassingUsers: &passed, Outcome: "PASS", Probes: []explorer.Probe{{Users: 2, Status: "PASS", Runs: []report.Result{run, run}}}}
	writeJSONFile(t, input, saved)
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("exploration export failed: %s", stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "closed_loop_exploration") ||
		!strings.Contains(string(data), "virtual_users") || !strings.Contains(string(data), `"tested_load": 2`) ||
		!strings.Contains(string(data), `"total": 20`) {
		t.Fatalf("exploration export lost unit or exposed metadata: %s", data)
	}
}

func TestReportExportAcceptsOpenArrivalExplorerWithoutUnitConfusion(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "arrival.json"), filepath.Join(dir, "redacted.json")
	secret := "tenant-09.private.example"
	run := report.Result{SchemaVersion: 3, Outcome: "PASS", ObservationSeconds: 10, ElapsedSeconds: 10, Config: report.RunConfig{Mode: "arrival", Users: 5, ArrivalRate: 2, Duration: "10s", WorkloadFingerprint: "synthetic-fingerprint"}, Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 10}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 10}, Steps: []report.StepResult{{Name: "step", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 10}}}}
	saved := arrivalexplorer.Result{SchemaVersion: 1, Scenario: secret, Target: "https://" + secret, StartedAt: time.Now().UTC().Format(time.RFC3339), StageDuration: "10s", Repeats: 2,
		WorkerLimit: 5, MinJourneysPerSecond: 2, MaxJourneysPerSecond: 4, Outcome: "PASS",
		Probes: []arrivalexplorer.Probe{{JourneysPerSecond: 2, Status: "PASS", Runs: []report.Result{run, run}}}}
	writeJSONFile(t, input, saved)
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("arrival exploration export failed: %s", stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, secret) || !strings.Contains(text, "open_arrival_exploration") ||
		!strings.Contains(text, "scheduled_journey_starts_per_second") || !strings.Contains(text, `"journey_worker_limit": 5`) ||
		!strings.Contains(text, `"tested_load": 2`) {
		t.Fatalf("arrival explorer units were omitted or conflated: %s", text)
	}
}

func validExportRun(t *testing.T, name string, users int, duration, mode string) report.Result {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer server.Close()
	scenario := config.Scenario{Name: name, BaseURL: server.URL, Users: users, Duration: duration, WindowDuration: "1s", MaxRPS: 20, P95LimitMS: 250, MaxErrorRate: .01, Mode: mode, Steps: []config.Step{{Name: "step", Path: "/"}}}
	if mode == "arrival" {
		scenario.ArrivalRate = 2
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	run := engine.Run(context.Background(), validated)
	run.Config.WorkloadFingerprint = validated.WorkloadFingerprint
	run.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := insights.ValidateRunReport(run); err != nil {
		t.Fatalf("test engine report invalid: %v", err)
	}
	return run
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
