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
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

// knownCapacityTarget serves /work in 150ms while at most workers requests
// are in flight and in 600ms once that pool is exceeded. A zero-think closed
// loop therefore meets a 400ms p95 SLO at `workers` VUs or fewer and fails
// it above.
func knownCapacityTarget(t *testing.T, workers int64) string {
	t.Helper()
	var inflight atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/work" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		delay := 150 * time.Millisecond
		if inflight.Add(1) > workers {
			delay = 600 * time.Millisecond
		}
		time.Sleep(delay)
		// Leave the pool before the response is written so a VU's next
		// closed-loop request never overlaps its own previous one.
		inflight.Add(-1)
		w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func shortenCapacityTiming(t *testing.T) {
	t.Helper()
	previous := capacityTiming
	capacityTiming.ExploreStage = 10 * time.Second
	capacityTiming.Warmup = 2 * time.Second
	capacityTiming.Recovery = 3 * time.Second
	t.Cleanup(func() { capacityTiming = previous })
}

// TestCapacityExtendedProtocolFindsKnownCapacityEndToEnd runs the full
// capacity chain (baseline, self-calibration, extended explore above 50 VUs,
// calibrated campaign, audit, executive report) against a loopback server
// whose capacity is known in advance.
func TestCapacityExtendedProtocolFindsKnownCapacityEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end capacity protocol runs for a few minutes")
	}
	shortenCapacityTiming(t)
	const maxUsers = 100
	const levelDuration = 5 * time.Second
	opt := explorer.Options{MinUsers: 2, MaxUsers: maxUsers}
	calibrated := capacityCalibrationLevels(opt, levelDuration)
	// The known capacity is a calibrated level above the standard ceiling
	// with a higher calibrated level that must fail.
	known := calibrated[len(calibrated)-2]
	if known <= config.StandardEnvelopeLimits.MaxUsers {
		t.Fatalf("fixture needs a calibrated level above 50 VUs: %v", calibrated)
	}
	base := knownCapacityTarget(t, int64(known))
	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "scenario.json")
	data, err := json.Marshal(config.Scenario{
		Name: "known capacity", BaseURL: base, Mode: "closed", Users: 2, Duration: "10s",
		MaxRPS: 1000, P95LimitMS: 400, MaxErrorRate: .01, Environment: "lab",
		BaselinePath: "/baseline", Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenarioPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "run")
	var stdout, stderr bytes.Buffer
	code := capacityCmd([]string{
		"--config", scenarioPath, "--authorized", "--output-dir", out, "--max-users", "100",
		"--response-bytes", "2", "--server-delay", "150ms", "--calibration-level-duration", levelDuration.String(),
		"--extended-envelope", "--extended-load-authorized", "--target-class", "lab",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capacity exit %d\nstdout:\n%s\nstderr:\n%s", code, &stdout, &stderr)
	}
	log := stdout.String()

	loaded, err := loadAnyCalibration(filepath.Join(out, "selfcal-explore.json"))
	if err != nil || loaded.scenario == nil || loaded.scenario.Status != calibration.ScenarioCalibrationPassed {
		t.Fatalf("self-calibration did not pass: %v\n%s", err, log)
	}

	// The planned explore budget must cover the explore stage's real wall
	// time, which starts after calibration and ends when explore.json is
	// written.
	manifestData, err := os.ReadFile(filepath.Join(out, "protocol.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest capacityManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	calibratedAt, errA := os.Stat(filepath.Join(out, "selfcal-explore.json"))
	exploredAt, errB := os.Stat(filepath.Join(out, "explore.json"))
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	elapsed := exploredAt.ModTime().Sub(calibratedAt.ModTime())
	if elapsed <= 0 || elapsed > manifest.ExploreBudget {
		t.Fatalf("explore took %s, planned budget %s", elapsed, manifest.ExploreBudget)
	}
	t.Logf("explore took %s within the planned %s budget", elapsed, manifest.ExploreBudget)

	exploration, err := readCapacityExplore(filepath.Join(out, "explore.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := capacityPassingLevel(exploration, true); got != known {
		t.Fatalf("highest passing level %d VUs, want the known capacity %d (%s: %s)\n%s", got, known, exploration.Outcome, exploration.Reason, log)
	}
	failedAbove := false
	for _, probe := range exploration.Probes {
		if probe.Users > known && probe.Status == "FAIL" {
			failedAbove = true
		}
		if probe.Users <= known && probe.Status != "PASS" {
			t.Fatalf("level %d at or below the known capacity did not pass: %s", probe.Users, probe.Status)
		}
	}
	if !failedAbove || exploration.LoadEnvelope == nil {
		t.Fatalf("exploration did not bracket the knee under the extended envelope\n%s", log)
	}

	evidence, err := capacityplan.ReadCampaign(filepath.Join(out, "campaign.json"))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.BaseUsers != known || evidence.Outcome != "PASS" || len(evidence.Trials) != 2 {
		t.Fatalf("campaign users=%d outcome=%s trials=%d\n%s", evidence.BaseUsers, evidence.Outcome, len(evidence.Trials), log)
	}
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Run.Quality == nil || phase.Run.Quality.State != "VALID_WITHIN_ENVELOPE" {
				t.Fatalf("campaign phase %s quality %+v", phase.Name, phase.Run.Quality)
			}
		}
	}

	auditData, err := os.ReadFile(filepath.Join(out, "audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var assessment capacityplan.Assessment
	if err := json.Unmarshal(auditData, &assessment); err != nil {
		t.Fatal(err)
	}
	if assessment.State == "" || assessment.State == "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("audit state %q blockers %+v", assessment.State, assessment.DecisionBlockers)
	}
	t.Logf("known capacity %d VUs; explore %s; audit %s", known, exploration.Outcome, assessment.State)
	if report, err := os.ReadFile(filepath.Join(out, "capacity.md")); err != nil || len(report) == 0 {
		t.Fatalf("executive report missing: %v", err)
	}
	if !strings.Contains(log, "Explore sizing:") {
		t.Fatalf("plan did not print the explore sizing:\n%s", log)
	}
}

func TestCapacityWorstCaseProbesCountRefinement(t *testing.T) {
	// Standard: failing at 16 leaves 7 levels inside 8-16, three probes.
	if got := capacityWorstCaseProbes([]int{2, 4, 8, 16, 20}, nil, false); got != 7 {
		t.Fatalf("standard worst case %d", got)
	}
	// Extended: only calibrated levels inside a bracket can be probed.
	if got := capacityWorstCaseProbes([]int{2, 4, 8}, []int{2, 4, 8}, true); got != 3 {
		t.Fatalf("extended without fill-ins %d", got)
	}
	if got := capacityWorstCaseProbes([]int{2, 4, 8}, []int{2, 3, 4, 5, 6, 7, 8}, true); got != 5 {
		t.Fatalf("extended with fill-ins %d", got)
	}
}

func TestCapacityStageOverheadUsesEngineBounds(t *testing.T) {
	s := config.Scenario{Name: "fixture", BaseURL: "http://127.0.0.1:18089", Users: 2, Duration: "20s", Timeout: "4s",
		P95LimitMS: 750, MaxErrorRate: .01, MaxRPS: 50, BaselinePath: "/baseline",
		Steps: []config.Step{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}}}
	overhead, err := capacityStageOverhead(s, 2, nil, false)
	want := config.NetworkBaselineWallBudget + 8*time.Second + capacityStageSetupAllowance
	if err != nil || overhead != want {
		t.Fatalf("overhead %s, want %s (%v)", overhead, want, err)
	}
	s.BaselinePath, s.NetworkBaselineSkipReason = "", "synthetic-loopback-test"
	if overhead, err := capacityStageOverhead(s, 2, nil, false); err != nil || overhead != want-config.NetworkBaselineWallBudget {
		t.Fatalf("skipped baseline overhead %s (%v)", overhead, err)
	}
}

func TestCapacityRefusesExploreAboveCapWithProposal(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "128", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "above the 15m explore cap") || !strings.Contains(errOut.String(), "lower --max-users") {
		t.Fatalf("oversized explore plan: %d %s %s", code, &out, &errOut)
	}
}
