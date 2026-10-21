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

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
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
	source := `{"schema_version":10,"name":"` + secret + `","target":"https://` + secret + `","started_at":"2026-04-03T12:30:00Z","elapsed_seconds":2,"observation_seconds":2,"config":{"users":2,"duration":"2s","request_timeout":"1s","max_rps":20,"p95_limit_ms":250,"max_error_rate":0.01,"mode":"closed"},"metrics":{"total":2,"success":2,"failures":0,"p95_ms":35},"steps":[{"name":"checkout-` + secret + `","method":"GET","path":"/private/customer-731","metrics":{"total":2,"success":2,"failures":0}}],"warnings":["` + secret + `"]}`
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
	if !strings.Contains(text, "damocline-redacted-evidence-v2") || !strings.Contains(text, `"total": 2`) || !strings.Contains(text, "step_001") || !strings.Contains(stdout.String(), "not a native assessment artifact") {
		t.Fatalf("export omitted evidence or its limitation: %s | %s", text, stdout.String())
	}
	unchanged, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != source {
		t.Fatal("export modified the source artifact")
	}
	assertNoPrivateExportFields(t, data, secret)
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

func TestReportExportRejectsDuplicateJSONKeys(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "duplicate.json")
	output := filepath.Join(dir, "redacted.json")
	if err := os.WriteFile(input, []byte(`{"schema_version":1,"schema_\u0076ersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "ambiguous JSON") {
		t.Fatalf("export accepted ambiguous JSON: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("ambiguous source produced an export: stat err=%v", err)
	}
}

func TestReportExportRejectsMissingMetricCounterBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "missing-counter.json")
	output := filepath.Join(dir, "redacted.json")
	if err := os.WriteFile(input, []byte(`{"schema_version":1,"metrics":{"success":0,"failures":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "omits required total counter") {
		t.Fatalf("missing metric counter was not rejected: code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid evidence wrote redacted output (stat error %v)", err)
	}
}

func TestReportExportRejectsMissingCampaignTimestampBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "campaign.json")
	output := filepath.Join(dir, "redacted.json")
	if err := os.WriteFile(input, []byte(`{"schema_version":15,"scenario":"synthetic","base_users":2,"target":"http://127.0.0.1:18080","warmup":"0s","observation":"10s","recovery":"10s","repeats":2,"trials":[],"outcome":"INCONCLUSIVE"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"started_at"`) {
		t.Fatalf("campaign export accepted missing start timestamp: code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid campaign wrote redacted output (stat error %v)", err)
	}
}

func TestReportExportValidHistoricalCampaignsThroughCLIWithoutChangingSources(t *testing.T) {
	dir := t.TempDir()
	marker := "private-marker-731.example.invalid"
	policy := campaign.DefaultStabilityPolicy()
	objectives := campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 500,
		MaxJourneyErrorRate: .1, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MaxDroppedArrivals: 0,
		MinimumObservationWindows: policy.MinimumWindows, MinimumSamplesPerWindow: policy.MinimumSamplesPerWindow, WindowDuration: "AUTO_PER_PHASE"}

	closedV9 := campaign.Result{SchemaVersion: 9, Scenario: marker, Target: "https://app.example.com/private", StartedAt: "2026-01-02T03:04:05Z",
		Warmup: "0s", Observation: "10s", Recovery: "0s", Cooldown: "0s", StabilityPolicy: &policy,
		BaseUsers: 2, Repeats: 2, Outcome: "INCONCLUSIVE", Trials: []campaign.Trial{{RunID: strings.Repeat("a", 32), Number: 1, Phases: []campaign.Phase{}, Outcome: "INCONCLUSIVE"}}}
	closedV13 := closedV9
	closedV13.SchemaVersion = campaign.CurrentSchemaVersion
	closedV13.ExecutionState = "COMPLETED"
	closedV13.Objectives = &objectives
	arrivalV4 := arrivalcampaign.Result{SchemaVersion: 4, Scenario: marker, Target: "https://app.example.com/private", StartedAt: "2026-01-02T03:04:05Z",
		WorkloadFingerprint: "sha256:synthetic-workload", WorkerLimit: 2, RequestTimeout: "1s", RequestRateCeiling: 20,
		P95LimitMS: 500, MaxErrorRate: .1, MinSamplesPerStep: 1, WindowDuration: "1s", BaseStartsPerSecond: 1,
		Warmup: "0s", Observation: "10s", Recovery: "10s", Budget: "5m", Repeats: 2, Outcome: "ABORTED",
		Trials: []arrivalcampaign.Trial{{Number: 1, Outcome: "ABORTED", AbortReason: "synthetic interruption"}}}
	arrivalV8 := arrivalV4
	arrivalV8.SchemaVersion = arrivalcampaign.CurrentSchemaVersion
	arrivalV8.ExecutionState = "COMPLETED"
	arrivalV8.Cooldown = "0s"
	arrivalV8.Objectives = &objectives
	arrivalV8.StabilityPolicy = &policy
	arrivalV8.Trials = []arrivalcampaign.Trial{{RunID: strings.Repeat("b", 32), Number: 1, Outcome: "ABORTED", AbortReason: "synthetic interruption"}}

	for _, tc := range []struct {
		name string
		data any
		kind string
	}{
		{name: "closed-loop schema v9", data: closedV9, kind: "closed_loop_campaign"},
		{name: "closed-loop current schema", data: closedV13, kind: "closed_loop_campaign"},
		{name: "open-arrival schema v4", data: arrivalV4, kind: "open_arrival_campaign"},
		{name: "open-arrival current schema", data: arrivalV8, kind: "open_arrival_campaign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".json")
			output := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+"-redacted.json")
			writeJSONFile(t, input, tc.data)
			before, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 0 {
				t.Fatalf("historical campaign did not pass CLI validation/export: code=%d stderr=%s", code, stderr.String())
			}
			assertExportSourceUnchanged(t, input, before)
			exported, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(exported), `"kind": "`+tc.kind+`"`) || strings.Contains(string(exported), marker) {
				t.Fatalf("export omitted campaign kind or retained private marker: %s", exported)
			}
			assertNoPrivateExportFields(t, exported, marker)
		})
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
	if value["format"] != "damocline-redacted-evidence-v2" || value["schema_version"] != nil || value["target"] != nil {
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
		MaxUsers: 4, HighestPassingUsers: &passed, Outcome: "PASS", Probes: []explorer.Probe{{Users: 2, Status: "PASS", Runs: []report.Result{run, run}}}, Notes: []string{}}
	writeJSONFile(t, input, saved)
	sourceBefore, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
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
	assertExportSourceUnchanged(t, input, sourceBefore)
	assertNoPrivateExportFields(t, data, secret)
}

func TestReportExportAcceptsOpenArrivalExplorerWithoutUnitConfusion(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "arrival.json"), filepath.Join(dir, "redacted.json")
	secret := "tenant-09.private.example"
	run := report.Result{SchemaVersion: 3, Outcome: "PASS", ObservationSeconds: 10, ElapsedSeconds: 10, Config: report.RunConfig{Mode: "arrival", Users: 5, ArrivalRate: 2, Duration: "10s", WorkloadFingerprint: "synthetic-fingerprint"}, Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 10}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 10}, Steps: []report.StepResult{{Name: "step", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 10}}}}
	saved := arrivalexplorer.Result{SchemaVersion: 1, Scenario: secret, Target: "https://" + secret, StartedAt: time.Now().UTC().Format(time.RFC3339), StageDuration: "10s", Repeats: 2,
		WorkerLimit: 5, MinJourneysPerSecond: 2, MaxJourneysPerSecond: 4, Outcome: "PASS",
		Probes: []arrivalexplorer.Probe{{JourneysPerSecond: 2, Status: "PASS", Runs: []report.Result{run, run}}}, Notes: []string{}}
	writeJSONFile(t, input, saved)
	sourceBefore, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
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
	assertExportSourceUnchanged(t, input, sourceBefore)
	assertNoPrivateExportFields(t, data, secret)
}

func TestReportExportRejectsExplorationWithOmittedTopLevelEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "closed loop", data: `{"schema_version":1,"probes":[{}]}`},
		{name: "open arrival", data: `{"schema_version":1,"journey_worker_limit":2,"probes":[{}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "exploration.json"), filepath.Join(dir, "redacted.json")
			if err := os.WriteFile(input, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			if code := Run([]string{"report", "export", "--file", input, "--out", output}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "missing required field") {
				t.Fatalf("export accepted incomplete exploration: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("export wrote output for invalid evidence: stat err=%v", err)
			}
		})
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

func assertExportSourceUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("export changed source bytes: err=%v", err)
	}
}

func assertNoPrivateExportFields(t *testing.T, data []byte, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if strings.Contains(string(data), marker) {
			t.Fatalf("redacted export retained private marker %q: %s", marker, data)
		}
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"target": true, "scenario": true, "started_at": true, "abort_reason": true, "run_id": true, "workload_fingerprint": true, "notes": true}
	var inspect func(any)
	inspect = func(item any) {
		switch typed := item.(type) {
		case map[string]any:
			for key, nested := range typed {
				if forbidden[key] {
					t.Errorf("redacted export retained private or non-allowlisted field %q: %s", key, data)
				}
				inspect(nested)
			}
		case []any:
			for _, nested := range typed {
				inspect(nested)
			}
		}
	}
	inspect(value)
}
