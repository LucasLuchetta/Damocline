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
