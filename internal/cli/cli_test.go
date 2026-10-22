package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestSeededWeightedExampleDryRunShowsTimingPolicy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "seeded-weighted-readonly.json")
	code := Run([]string{"run", "--config", path, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"Profile dashboard", "Profile search", "Profile review", "uniform think time=200ms..800ms (seed=42)", "sampled think time=[100ms 100ms 250ms 500ms] (seed=42)", "think time=400ms", "DRY RUN: no requests were sent"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("dry run omitted %q: %s", expected, output)
		}
	}
}

func TestMigrateScenarioCommandWritesVersionedOutputWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "legacy.json")
	output := filepath.Join(dir, "versioned.json")
	legacy := config.Scenario{Name: "synthetic", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", Timeout: "1s", MaxRPS: 10, P95LimitMS: 500, MaxErrorRate: .01,
		Steps: []config.Step{{Name: "read", Method: "GET", Path: "/items"}}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"migrate-scenario", "--config", input, "--out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("migration failed: code=%d stderr=%s", code, stderr.String())
	}
	migrated, err := config.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.ScenarioVersion != config.CurrentScenarioVersion || len(migrated.Workflows) != 1 || migrated.Workflows[0].Steps[0].Path != "/items" {
		t.Fatalf("command output did not use the versioned workflow format: %+v", migrated)
	}
	after, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatalf("migration changed its source: err=%v", err)
	}

	if code := Run([]string{"migrate-scenario", "--config", input, "--out", output}, &stdout, &stderr); code == 0 {
		t.Fatal("migration overwrote an existing destination")
	}
	if code := Run([]string{"migrate-scenario", "--config", input, "--out", input}, &stdout, &stderr); code == 0 {
		t.Fatal("migration allowed the source path as its output")
	}
}

func TestArrivalCampaignDryRunShowsOpenArrivalSchedule(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "arrival-rate-readonly.json")
	code := Run([]string{"arrival-campaign", "--config", path, "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--peak-journeys-per-second", "1", "--cooldown", "1s", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"base 1 scheduled starts/s", "worker limit", "Trial 1 warmup: 30s at 1 scheduled starts/s", "Trial 1 observation: 10s at 1 scheduled starts/s", "Cool-down between independent trials: 1s", "DRY RUN: no traffic generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("dry run missing %q: %s", expected, stdout.String())
		}
	}
}

func TestCampaignDryRunsIncludeConfiguredStabilization(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "closed", args: []string{"campaign", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--warmup", "0s", "--stabilization", "10s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run"}, want: "Trial 1 stabilization: 10s at 2 VUs"},
		{name: "arrival", args: []string{"arrival-campaign", "--config", filepath.Join("..", "..", "examples", "arrival-rate-readonly.json"), "--warmup", "0s", "--stabilization", "10s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run"}, want: "Trial 1 stabilization: 10s at 2 scheduled starts/s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stabilization dry-run mismatch: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunRequireCalibrationBlocksBeforeTrafficAndRequiresArtifact(t *testing.T) {
	path := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1", Mode: "arrival", Users: 10,
		ArrivalRate: 7, Duration: "5s", Timeout: "1s", MaxRPS: 50, P95LimitMS: 500, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "read", Method: "GET", Path: "/matrix/get-empty"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--config", configPath, "--authorized", "--require-calibration", "--calibration", path}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "no traffic was sent") || strings.Contains(stdout.String(), "Running bounded HTTP workload") {
		t.Fatalf("unmeasured rate did not block before traffic: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"run", "--config", configPath, "--dry-run", "--require-calibration"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires --calibration") {
		t.Fatalf("required calibration without artifact was not rejected: code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	closed := scenario
	closed.Mode = "closed"
	closed.ArrivalRate = 0
	closedData, err := json.Marshal(closed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, closedData, 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"run", "--config", configPath, "--dry-run", "--require-calibration", "--calibration", path}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "only exact open-arrival") {
		t.Fatalf("closed-loop gate was not rejected explicitly: code=%d stderr=%q", code, stderr.String())
	}
}

func TestArrivalCampaignCalibrationDryRunBlocksUnmeasuredRate(t *testing.T) {
	path := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1", Mode: "arrival", Users: 10,
		ArrivalRate: 7, Duration: "5s", Timeout: "1s", MaxRPS: 50, P95LimitMS: 500, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "read", Method: "GET", Path: "/matrix/get-empty"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"arrival-campaign", "--config", configPath, "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run", "--calibration", path}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "DRY RUN BLOCKED") || !strings.Contains(stdout.String(), "BLOCKED") {
		t.Fatalf("unmeasured campaign rate did not block: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestArrivalCampaignCalibrationGateBlocksBeforeSendingTraffic(t *testing.T) {
	path := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:1", Mode: "arrival", Users: 10,
		ArrivalRate: 7, Duration: "5s", Timeout: "1s", MaxRPS: 50, P95LimitMS: 500, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "read", Method: "GET", Path: "/matrix/get-empty"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"arrival-campaign", "--config", configPath, "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--out", filepath.Join(t.TempDir(), "campaign.json"), "--authorized", "--calibration", path}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "no traffic was sent") || strings.Contains(stdout.String(), "Report:") {
		t.Fatalf("unmeasured campaign rate did not block before traffic: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestClosedCampaignDryRunShowsCooldownBetweenTrials(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "advanced-readonly.json")
	code := Run([]string{"campaign", "--config", path, "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--cooldown", "1s", "--dry-run"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Cool-down between independent trials: 1s") {
		t.Fatalf("closed campaign omitted cooldown schedule: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestClosedCampaignCalibrationRequirementBlocksBeforeDryRunSchedule(t *testing.T) {
	calibrationPath := writePassingCalibration(t, nil)
	var stdout, stderr bytes.Buffer
	args := []string{"campaign", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run", "--calibration", calibrationPath, "--require-calibration"}
	code := Run(args, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "does not cover every planned closed-loop VU level") || strings.Contains(stdout.String(), "Trial 1 observation") {
		t.Fatalf("required closed-loop calibration did not block before schedule: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestClosedCampaignCalibrationExactVUProfilePassesDryRunGate(t *testing.T) {
	path := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic closed", BaseURL: "http://127.0.0.1", Mode: "closed", Users: 5, Duration: "2s", RampUp: "0s", ThinkTime: "0s", Timeout: "1s", MaxRPS: 100, ConnectionPolicy: "reuse", P95LimitMS: 500, MaxErrorRate: 0.1, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"campaign", "--config", configPath, "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run", "--calibration", path, "--require-calibration"}
	if code := Run(args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Closed-loop generator preflight: READY") {
		t.Fatalf("exact VU profile should pass the traffic-free calibration gate: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCampaignsRequireOutputArtifactOutsideDryRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "closed", args: []string{"campaign", "--config", "unused.json"}},
		{name: "arrival", args: []string{"arrival-campaign", "--config", "unused.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "campaigns require --out") {
				t.Fatalf("campaign without a retained artifact was not rejected before execution: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCampaignOutputCannotOverwriteScenarioFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		args []string
	}{
		{name: "closed", path: filepath.Join("..", "..", "examples", "advanced-readonly.json"), args: []string{"campaign", "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2"}},
		{name: "arrival", path: filepath.Join("..", "..", "examples", "arrival-rate-readonly.json"), args: []string{"arrival-campaign", "--observation", "10s", "--recovery", "10s", "--repeats", "2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{tc.args[0], "--config", tc.path}, tc.args[1:]...)
			args = append(args, "--out", tc.path)
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "must not refer to the scenario configuration") {
				t.Fatalf("campaign accepted its scenario as the output artifact: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			after, err := os.ReadFile(tc.path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("scenario file changed after output alias rejection: err=%v", err)
			}
		})
	}
}

func TestCampaignDryRunsShowLocalGeneratorGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "closed", args: []string{"campaign", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2"}},
		{name: "arrival", args: []string{"arrival-campaign", "--config", filepath.Join("..", "..", "examples", "arrival-rate-readonly.json"), "--observation", "10s", "--recovery", "10s", "--repeats", "2"}},
	} {
		args := append(append([]string(nil), tc.args...), "--max-generator-cpu-percent", "75", "--max-generator-rss-mb", "512", "--resource-guardrail-samples", "3", "--dry-run")
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s guardrail preview failed: code=%d stderr=%s", tc.name, code, stderr.String())
		}
		for _, expected := range []string{"Generator abort guardrails: CPU 75.0% | RSS 512.0 MiB | 3 consecutive samples", "local generator, not the target application"} {
			if !strings.Contains(stdout.String(), expected) {
				t.Fatalf("%s guardrail preview omitted %q: %s", tc.name, expected, stdout.String())
			}
		}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"campaign", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--max-generator-rss-mb", "18446744073709551615", "--dry-run"}
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "too large") {
		t.Fatalf("overflowing RSS MiB limit accepted: code=%d stderr=%s", code, stderr.String())
	}
}

func TestLongRunCampaignDryRunsRequireExplicitStagingAndBudget(t *testing.T) {
	closed := config.Scenario{
		Name: "staging-long-run", BaseURL: "https://app.example.com", Environment: "staging",
		Users: 2, Duration: "30s", Timeout: "2s", MaxRPS: 10, P95LimitMS: 1000, MaxErrorRate: .01,
		Steps: []config.Step{{Name: "read", Method: "GET", Path: "/"}},
	}
	arrival := closed
	arrival.Mode, arrival.ArrivalRate = "arrival", 1
	write := func(name string, scenario config.Scenario) string {
		t.Helper()
		data, err := json.Marshal(scenario)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name+".json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "closed", args: []string{"campaign", "--config", write("closed", closed), "--observation", "30m", "--recovery", "45s", "--repeats", "2", "--allow-long-run", "--staging-confirmed", "--budget", "70m", "--dry-run"}, want: "Trial 1 observation: 30m0s at 2 VUs"},
		{name: "arrival", args: []string{"arrival-campaign", "--config", write("arrival", arrival), "--observation", "30m", "--recovery", "45s", "--repeats", "2", "--allow-long-run", "--staging-confirmed", "--budget", "70m", "--dry-run"}, want: "Trial 1 observation: 30m0s at 1 scheduled starts/s"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(tc.args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), tc.want) || !strings.Contains(stdout.String(), "environment declared staging") || !strings.Contains(stdout.String(), "DRY RUN: no traffic") {
			t.Fatalf("%s long-run dry run failed: code=%d stdout=%s stderr=%s", tc.name, code, stdout.String(), stderr.String())
		}
		var rejectedOut, rejectedErr bytes.Buffer
		args := append([]string(nil), tc.args...)
		args = args[:len(args)-5] // remove approval, budget, and dry-run flags for fail-closed validation
		args = append(args, "--dry-run")
		if code := Run(args, &rejectedOut, &rejectedErr); code != 2 {
			t.Fatalf("%s long run without explicit policy was accepted: code=%d stdout=%s stderr=%s", tc.name, code, rejectedOut.String(), rejectedErr.String())
		}
		args = append([]string(nil), tc.args[:len(tc.args)-1]...)
		args = append(args, "--authorized")
		var outputRequired, outputError bytes.Buffer
		if code := Run(args, &outputRequired, &outputError); code != 2 || !strings.Contains(outputError.String(), "require --out") {
			t.Fatalf("%s long run without durable evidence output was accepted: code=%d stdout=%s stderr=%s", tc.name, code, outputRequired.String(), outputError.String())
		}
	}
}

func TestArrivalExploreDryRunLabelsJourneyStartRates(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "arrival-rate-readonly.json")
	code := Run([]string{"explore-arrival", "--config", path, "--min-journeys-per-second", "2", "--max-journeys-per-second", "10", "--authorized", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("arrival dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"scheduled journey starts 2-10/s", "worker limit 10", "DRY RUN: no traffic generated", "request/s and completed journeys/s are separate"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("arrival dry run omitted %q: %s", expected, stdout.String())
		}
	}
}

func TestArrivalDemandGoalDryRunCalculatesExactScheduledStartRate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "arrival-rate-readonly.json")
	code := Run([]string{"explore-arrival", "--config", path, "--min-journeys-per-second", "2", "--max-journeys-per-second", "10", "--peak-journeys-per-second", "5", "--growth-percent", "25", "--reserve-percent", "20", "--authorized", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("arrival demand dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"Business demand: peak 5 starts/s", "required exact target 8 starts/s", "scheduled journey starts 2-10/s", "DRY RUN: no traffic generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("arrival demand dry run omitted %q: %s", expected, stdout.String())
		}
	}
}

func TestArrivalExploreRejectsClosedLoopScenario(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "advanced-readonly.json")
	code := Run([]string{"explore-arrival", "--config", path, "--dry-run"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "requires mode=arrival") {
		t.Fatalf("arrival explorer accepted a closed-loop scenario: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestArrivalExploreCalibrationLimitsSearchToMeasuredRates(t *testing.T) {
	calibrationPath := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic-reference-shape", BaseURL: "http://127.0.0.1:18080", Mode: "arrival",
		Users: 10, ArrivalRate: 2, Duration: "20s", Timeout: "1s", MaxRPS: 50,
		P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"explore-arrival", "--config", configPath, "--min-journeys-per-second", "2", "--max-journeys-per-second", "20",
		"--target-journeys-per-second", "10", "--calibration", calibrationPath, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("calibrated dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"3 exact scheduled-start profiles are eligible", "Exact compatible rates: [2 5 10]/s",
		"Calibrated probe set: [2 5 10]/s", "effective search bounds 2-10/s (requested 2-20/s)",
		"Uncalibrated rates will not be probed", "DRY RUN: no traffic generated"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("calibrated dry run omitted %q: %s", expected, stdout.String())
		}
	}
}

func TestArrivalExploreCalibrationBlocksUncalibratedMinimumOrTarget(t *testing.T) {
	calibrationPath := writePassingCalibration(t, nil)
	scenario := config.Scenario{Name: "synthetic-reference-shape", BaseURL: "http://127.0.0.1:18080", Mode: "arrival",
		Users: 10, ArrivalRate: 2, Duration: "20s", Timeout: "1s", MaxRPS: 50,
		P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, _ := json.Marshal(scenario)
	configPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "uncalibrated minimum", args: []string{"--min-journeys-per-second", "3", "--max-journeys-per-second", "10"}, want: "requested minimum rate 3/s"},
		{name: "uncalibrated exact target", args: []string{"--min-journeys-per-second", "2", "--max-journeys-per-second", "10", "--target-journeys-per-second", "7"}, want: "exact demand target 7/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"explore-arrival", "--config", configPath}, tc.args...)
			args = append(args, "--calibration", calibrationPath, "--require-calibration", "--dry-run")
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), tc.want) || !strings.Contains(stderr.String(), "no traffic was sent") {
				t.Fatalf("incompatible calibration did not block before traffic: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"explore-arrival", "--config", configPath, "--require-calibration", "--dry-run"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires --calibration") {
		t.Fatalf("required calibration without artifact was accepted: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCalibratedArrivalExploreExecutesOnlyQualifiedProfileAndRetainsEvidence(t *testing.T) {
	calibrationPath := writePassingCalibration(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 4<<10))
	}))
	defer server.Close()
	scenario := config.Scenario{Name: "synthetic-reference-shape", BaseURL: server.URL, Mode: "arrival",
		Users: 10, ArrivalRate: 2, Duration: "10s", Timeout: "1s", MaxRPS: 50,
		P95LimitMS: 1000, MaxErrorRate: .1, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	configPath, outputPath := filepath.Join(dir, "scenario.json"), filepath.Join(dir, "exploration.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"explore-arrival", "--config", configPath, "--min-journeys-per-second", "2", "--max-journeys-per-second", "2",
		"--target-journeys-per-second", "2", "--stage-duration", "10s", "--calibration", calibrationPath,
		"--authorized", "--out", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("calibrated loopback exploration failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result arrivalexplorer.Result
	data, err = os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 2 || result.Outcome != "PASSED_TO_CONFIGURED_CEILING" || result.RequestedMaxJourneysPerSecond != 2 ||
		len(result.CalibratedProbeRates) != 1 || len(result.Probes) != 1 || result.Probes[0].JourneysPerSecond != 2 || len(result.Probes[0].Runs) != 2 {
		t.Fatalf("saved calibrated exploration omitted its qualified envelope: %+v", result)
	}
	for _, run := range result.Probes[0].Runs {
		if run.Calibration == nil || run.Calibration.Record.SchemaVersion != calibration.SchemaVersion || run.Config.ArrivalRate != 2 {
			t.Fatalf("native trial did not retain its exact calibration provenance: %+v", run.Calibration)
		}
	}
}

func TestWriteWorkflowDryRunClearlyMarksMutatingRoutes(t *testing.T) {
	t.Setenv("DAMOCLINE_PREVIEW_PREFIX", "preview")
	scenario := config.Scenario{
		Name: "synthetic-write-preview", BaseURL: "https://app.example.com", Users: 1, Duration: "10s",
		Timeout: "2s", MaxRPS: 5, P95LimitMS: 1000,
		SyntheticDataPrefixEnv: "DAMOCLINE_PREVIEW_PREFIX",
		Steps: []config.Step{
			{Name: "create", Method: "POST", Path: "/records/{{synthetic_id}}?mode=test"},
			{Name: "cleanup", Method: "DELETE", Path: "/records/{{synthetic_id}}"},
		},
	}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--config", path, "--dry-run", "--allow-writes"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mutating dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{
		"create: POST /records/{{synthetic_id}} [MUTATING; requires --allow-writes]",
		"cleanup: DELETE /records/{{synthetic_id}} [MUTATING; requires --allow-writes]",
		"DRY RUN: no requests were sent",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("mutating preview omitted %q: %s", expected, stdout.String())
		}
	}
}

func TestJourneyP95DescriptionDoesNotInventLatencyWhenNoneCompleted(t *testing.T) {
	if got := journeyP95Description(report.MetricSet{Total: 0, Cancelled: 2}); got != "unavailable (no completed journeys)" {
		t.Fatalf("empty journey percentile was presented as a measurement: %q", got)
	}
	if got := journeyP95Description(report.MetricSet{Total: 3, P95MS: 125.5}); got != "125.5 ms across completed journeys" {
		t.Fatalf("completed journey percentile was mislabeled: %q", got)
	}
}

func TestCampaignDryRunShowsBoundedWallTime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "examples", "seeded-weighted-readonly.json")
	code := Run([]string{"campaign", "--config", path, "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "wall-time cap 2m40s") || !strings.Contains(stdout.String(), "Trial 1 observation: 10s at 10 VUs") || !strings.Contains(stdout.String(), "Trial 2 recovery: 10s at 10 VUs") || !strings.Contains(stdout.String(), "DRY RUN: no traffic generated") {
		t.Fatalf("campaign preview did not show its effective cap: code=%d out=%s err=%s", code, stdout.String(), stderr.String())
	}
}

func TestCampaignWithoutRecoveryFailsBeforeReadingScenarioOrWritingReport(t *testing.T) {
	directory := t.TempDir()
	missingScenario := filepath.Join(directory, "not-read.json")
	outputPath := filepath.Join(directory, "campaign.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"campaign", "--config", missingScenario, "--observation", "10s", "--recovery", "0s", "--repeats", "2", "--authorized", "--out", outputPath}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "recovery") || strings.Contains(stderr.String(), "configuration error") {
		t.Fatalf("campaign without recovery was not rejected during preflight: code=%d out=%s err=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("invalid campaign created an output artifact: stat err=%v", err)
	}
}

func TestCampaignRepeatSummaryLabelsMedianMADAsDescriptive(t *testing.T) {
	variability := campaign.Variability{Runs: 5, P95MinMS: 100, P95MaxMS: 200, ErrorRateMin: 0, ErrorRateMax: .01,
		ThroughputMinRPS: 8, ThroughputMaxRPS: 10, SuccessfulJourneysMinPerSec: 7, SuccessfulJourneysMaxPerSec: 9,
		RequestP95MedianMS: 120, RequestP95MADMS: 10, RequestP95RobustLowerMS: 90, RequestP95RobustUpperMS: 150}
	var output bytes.Buffer
	writeCampaignObservationSummary(&output, &variability)
	for _, expected := range []string{"median 120ms", "MAD 10ms", "90-150ms", "not a confidence interval", "dependent or non-stationary"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("campaign summary omitted %q: %s", expected, output.String())
		}
	}
}

func TestRunDisplaysRequestAndSuccessfulJourneyThroughputSeparately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "scenario.json")
	scenario := config.Scenario{
		Name: "loopback-throughput", BaseURL: server.URL, Users: 1, Duration: "1s",
		Timeout: "2s", ThinkTime: "0s", MaxRPS: 1, P95LimitMS: 5000, MaxErrorRate: 0.9,
		Steps: []config.Step{{Name: "transaction", Method: "GET", Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--config", path, "--authorized"}, &stdout, &stderr); code != 0 {
		t.Fatalf("loopback run failed: code=%d stderr=%s output=%s", code, stderr.String(), stdout.String())
	}
	for _, expected := range []string{"request p95:", "Journey throughput:", "successful configured journeys/s", "completed journey p95:", "complete,", "cancelled/incomplete", "request p95=", "browser rendering or full page load", "near max_rps", "USER CAPACITY:  NOT DETERMINED"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("run output omitted %q: %s", expected, stdout.String())
		}
	}
}

func TestForcedInterruptionProcessHelper(t *testing.T) {
	if os.Getenv("DAMOCLINE_FORCED_INTERRUPTION_HELPER") != "1" {
		return
	}
	argsJSON, err := base64.RawURLEncoding.DecodeString(os.Getenv("DAMOCLINE_FORCED_INTERRUPTION_ARGS"))
	if err != nil {
		os.Exit(2)
	}
	var args []string
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		os.Exit(2)
	}
	os.Exit(Run(args, os.Stdout, os.Stderr))
}

func TestForcedProcessInterruptionLeavesNoRunReportAndReconcilesServerLedger(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: 2 * time.Second, MaxConcurrent: 1, MaxRequests: 2})
	if err != nil {
		t.Fatal(err)
	}
	requestStarted := make(chan struct{}, 1)
	requestFinished := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		defer func() { requestFinished <- struct{}{} }()
		fixture.ServeHTTP(w, r)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "scenario.json")
	reportPath := filepath.Join(t.TempDir(), "run.json")
	scenario := config.Scenario{
		Name: "forced interruption fixture", BaseURL: server.URL, Users: 1,
		Duration: "10s", RampUp: "0s", Timeout: "8s", MaxRPS: 100,
		P95LimitMS: 1000, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}},
	}
	configJSON, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	argsJSON, err := json.Marshal([]string{"run", "--config", configPath, "--authorized", "--out", reportPath})
	if err != nil {
		t.Fatal(err)
	}
	encodedArgs := base64.RawURLEncoding.EncodeToString(argsJSON)
	command := exec.Command(os.Args[0], "-test.run=^TestForcedInterruptionProcessHelper$")
	command.Env = append(os.Environ(),
		"DAMOCLINE_FORCED_INTERRUPTION_HELPER=1",
		"DAMOCLINE_FORCED_INTERRUPTION_ARGS="+encodedArgs,
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("child generator did not send a request: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if err := command.Process.Kill(); err != nil {
		_ = command.Wait()
		t.Fatalf("failed to force-stop child generator: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("forcibly interrupted generator exited successfully")
	}
	select {
	case <-requestFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("reference handler did not finish after the child process was killed")
	}
	ledger := fixture.Snapshot()
	if ledger.Received != 1 || ledger.Completed != 0 || ledger.Success != 0 || ledger.Failures != 0 ||
		ledger.Cancelled != 1 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("forced interruption did not reconcile with the independent reference ledger: %+v", ledger)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatalf("forced interruption must not leave a final run report, stat error=%v", err)
	}
}

func TestHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), "run --config") {
		t.Fatalf("wrong help: %s", out.String())
	}
}
func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"unrecognized"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
}
func TestRunNeedsConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"run", "--authorized"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestSlashCommandHelpAndHistoryRouting(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		if code := Run([]string{"help"}, &out, &errOut); code != 0 {
			t.Fatalf("help exit=%d", code)
		}
		if !strings.Contains(out.String(), "damocline /start") || !strings.Contains(out.String(), "damocline /history") {
			t.Fatalf("slash commands absent from help: %s", out.String())
		}
		out.Reset()
		if code := Run([]string{"/history"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "No saved scenarios") {
			t.Fatalf("empty history exit=%d output=%s stderr=%s", code, out.String(), errOut.String())
		}
		out.Reset()
		if code := Run([]string{"/history", "extra"}, &out, &errOut); code != 2 {
			t.Fatalf("history must reject unknown arguments, exit=%d", code)
		}
		if code := Run([]string{"/start", "extra"}, &out, &errOut); code != 2 {
			t.Fatalf("new scenario must reject unknown arguments, exit=%d", code)
		}
	})
}
