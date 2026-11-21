package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCapacityPlanningUsesSlowestRepeatedStep(t *testing.T) {
	v := capacityScenario(t, "http://127.0.0.1:18089")
	users := 7
	run := report.Result{
		Target: v.Scenario.BaseURL, Outcome: "PASS", ElapsedSeconds: 20,
		Config: report.RunConfig{
			Users: users, P95LimitMS: 750, MaxErrorRate: .01,
			WorkloadFingerprint: v.WorkloadFingerprint,
		},
		Journeys: report.MetricSet{Total: 200},
		Steps: []report.StepResult{
			{Name: "fast", Metrics: report.MetricSet{Total: 200}},
			{Name: "slow", Metrics: report.MetricSet{Total: 150}},
		},
	}
	second := run
	second.Steps = append([]report.StepResult(nil), run.Steps...)
	second.Steps[1].Metrics.Total = 120
	result := explorer.Result{
		Outcome: "BRACKETED", HighestPassingUsers: &users, Repeats: 2,
		Probes: []explorer.Probe{{Users: users, Status: "PASS", Runs: []report.Result{run, second}}},
	}
	var output bytes.Buffer
	gotUsers, window, err := capacityObservation(&output, result, v, 20)
	if gotUsers != users || window != 50*time.Second || err != nil || !strings.Contains(output.String(), "299") {
		t.Fatalf("unexpected plan: users=%d window=%s err=%v output=%s", gotUsers, window, err, &output)
	}
	second.Steps[1].Metrics.Total = 60
	if _, _, err := capacityObservation(io.Discard, result, v, 20); err == nil || !strings.Contains(err.Error(), "60s") {
		t.Fatalf("expected the window ceiling error, got %v", err)
	}
}

func TestCapacityNetworkPreflightSendsOnlyBaseline(t *testing.T) {
	var baseline, workload atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/baseline" {
			baseline.Add(1)
		} else {
			workload.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	v := capacityScenario(t, server.URL)
	v.NetworkBaseline.Requests = 2
	v.NetworkBaseline.Interval = time.Millisecond
	if err := capacityPreflight(io.Discard, v, ""); err != nil {
		t.Fatal(err)
	}
	if baseline.Load() != 2 || workload.Load() != 0 {
		t.Fatalf("baseline=%d workload=%d", baseline.Load(), workload.Load())
	}
}

func capacityScenario(t *testing.T, target string) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{
		Name: "fixture", BaseURL: target, Users: 1, Duration: "20s",
		P95LimitMS: 750, MaxErrorRate: .01, MaxRPS: 80,
		BaselinePath: "/baseline", Steps: []config.Step{{Name: "fast", Path: "/fast"}, {Name: "slow", Path: "/slow"}},
	}, config.Options{Authorized: true, NetworkBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCapacityResumeRequiresSameScenarioDigest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	control := capacityManifest{ScenarioSHA: "fixture-one", MaxUsers: 10, Budget: time.Minute}
	if err := prepareCapacityDirectory(dir, false, control); err != nil {
		t.Fatal(err)
	}
	if err := prepareCapacityDirectory(dir, true, control); err != nil {
		t.Fatal(err)
	}
	bigger := control
	bigger.Budget = 2 * time.Minute
	if err := prepareCapacityDirectory(dir, true, bigger); err != nil {
		t.Fatalf("explicitly increased budget was rejected: %v", err)
	}
	control.ScenarioSHA = "fixture-two"
	if err := prepareCapacityDirectory(dir, true, control); err == nil {
		t.Fatal("changed scenario was accepted on resume")
	}
}

func TestCapacitySessionHook(t *testing.T) {
	if mode := os.Getenv("CAPACITY_TEST_HOOK_MODE"); mode != "" {
		if os.Getenv("DAMOCLINE_SESSION_COUNT") != "2" {
			os.Exit(1)
		}
		if mode == "duplicate" {
			fmt.Print(`["session=fixture-a; kind=test","kind=test; session=fixture-a"]`)
		} else {
			fmt.Print(`["session=fixture-a","session=fixture-b"]`)
		}
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPACITY_TEST_HOOK_MODE", "valid")
	t.Setenv("TEST_CAPACITY_COOKIE_1", "session=original")
	args := []string{"-test.run=^TestCapacitySessionHook$"}
	restore, err := provisionCapacityCookies(exe, args, "TEST_CAPACITY_COOKIE_", 2)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEST_CAPACITY_COOKIE_2") != "session=fixture-b" {
		t.Error("hook cookie was not assigned")
	}
	restore()
	if os.Getenv("TEST_CAPACITY_COOKIE_1") != "session=original" {
		t.Error("original cookie was not restored")
	}
	t.Setenv("CAPACITY_TEST_HOOK_MODE", "duplicate")
	if _, err := provisionCapacityCookies(exe, args, "TEST_CAPACITY_COOKIE_", 2); err == nil {
		t.Fatal("duplicate cookies were accepted")
	}
}

func TestCapacityUsageHypothesisUsesLocalLog(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "actions.jsonl")
	contents := `{"user":"fixture-a","timestamp":"2026-01-01T00:00:00Z"}
{"user":"fixture-b","timestamp":"2026-01-01T00:00:30Z"}
{"user":"fixture-a","timestamp":"2026-01-01T00:01:00Z"}
`
	if err := os.WriteFile(log, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	intensity, err := capacityLogIntensity(log)
	if err != nil || intensity != 1.5 {
		t.Fatalf("intensity=%g, err=%v", intensity, err)
	}
	path := filepath.Join(dir, "report.md")
	if err := os.WriteFile(path, []byte("# Fixture audit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	evidence := campaign.Result{BaseUsers: 7, Trials: []campaign.Trial{{Phases: []campaign.Phase{
		{Name: "observation", Run: report.Result{ElapsedSeconds: 20, Metrics: report.MetricSet{Success: 30}}},
	}}}}
	destination := filepath.Join(dir, "augmented.md")
	if err := appendIntensityHypothesis(path, destination, evidence, intensity); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || !bytes.Contains(data, []byte("approximately 60.0 people")) || bytes.Contains(data, []byte("fixture-a")) {
		t.Fatalf("unexpected hypothesis: %s (%v)", data, err)
	}
}
