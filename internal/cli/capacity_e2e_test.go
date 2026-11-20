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

	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

// knownCapacityWorkers is the loopback fixture's exact concurrency capacity.
const knownCapacityWorkers = 5

// knownCapacityTarget serves /work in 20ms while at most knownCapacityWorkers
// requests are in flight and in 150ms once that pool is exceeded. A
// zero-think closed loop therefore meets a 100ms p95 SLO at 5 VUs or fewer
// and fails it at 6 VUs or more.
func knownCapacityTarget(t *testing.T) string {
	t.Helper()
	var inflight atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/work" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		delay := 20 * time.Millisecond
		if inflight.Add(1) > knownCapacityWorkers {
			delay = 150 * time.Millisecond
		}
		time.Sleep(delay)
		// Leave the pool before the response is written so the client's
		// next closed-loop request never overlaps its own previous one.
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
// capacity chain (baseline, self-calibration, extended explore, calibrated
// campaign, audit, executive report) against a loopback server whose
// capacity is known in advance.
func TestCapacityExtendedProtocolFindsKnownCapacityEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end capacity protocol runs for a few minutes")
	}
	shortenCapacityTiming(t)
	base := knownCapacityTarget(t)
	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "scenario.json")
	data, err := json.Marshal(config.Scenario{
		Name: "known capacity", BaseURL: base, Mode: "closed", Users: 2, Duration: "10s",
		MaxRPS: 1000, P95LimitMS: 100, MaxErrorRate: .01, Environment: "lab",
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
		"--config", scenarioPath, "--authorized", "--output-dir", out, "--max-users", "8",
		"--response-bytes", "2", "--server-delay", "20ms", "--calibration-level-duration", "5s",
		"--extended-envelope", "--extended-load-authorized", "--target-class", "lab",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capacity exit %d\nstdout:\n%s\nstderr:\n%s", code, &stdout, &stderr)
	}

	for _, name := range []string{"selfcal-explore.json", "selfcal-campaign.json"} {
		loaded, err := loadAnyCalibration(filepath.Join(out, name))
		if err != nil || loaded.scenario == nil || loaded.scenario.Status != "PASSED" {
			t.Fatalf("%s did not pass: %v", name, err)
		}
	}
	// The planned explore budget must cover the explore stage's real wall
	// time: it starts after the explore calibration artifact is written and
	// ends when explore.json is written.
	manifestData, err := os.ReadFile(filepath.Join(out, "protocol.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest capacityManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	calibrated, errA := os.Stat(filepath.Join(out, "selfcal-explore.json"))
	explored, errB := os.Stat(filepath.Join(out, "explore.json"))
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if elapsed := explored.ModTime().Sub(calibrated.ModTime()); elapsed <= 0 || elapsed > manifest.ExploreBudget {
		t.Fatalf("explore took %s, planned budget %s", elapsed, manifest.ExploreBudget)
	} else {
		t.Logf("explore took %s within planned budget %s", elapsed, manifest.ExploreBudget)
	}
	exploration, err := readCapacityExplore(filepath.Join(out, "explore.json"))
	if err != nil {
		t.Fatal(err)
	}
	if exploration.Outcome != "BRACKETED" || exploration.HighestPassingUsers == nil || exploration.FirstFailingUsers == nil {
		t.Fatalf("exploration did not bracket the knee: %s %s\n%s", exploration.Outcome, exploration.Reason, &stdout)
	}
	if got := *exploration.HighestPassingUsers; got != knownCapacityWorkers {
		t.Fatalf("highest passing %d VUs, want the known capacity %d\n%s", got, knownCapacityWorkers, &stdout)
	}
	if *exploration.FirstFailingUsers != knownCapacityWorkers+1 {
		t.Fatalf("first failing %d VUs, want %d", *exploration.FirstFailingUsers, knownCapacityWorkers+1)
	}
	if exploration.LoadEnvelope == nil {
		t.Fatal("extended exploration did not record its load envelope")
	}

	evidence, err := capacityplan.ReadCampaign(filepath.Join(out, "campaign.json"))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.BaseUsers != knownCapacityWorkers || evidence.Outcome != "PASS" || len(evidence.Trials) != 2 {
		t.Fatalf("campaign users=%d outcome=%s trials=%d\n%s", evidence.BaseUsers, evidence.Outcome, len(evidence.Trials), &stdout)
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
		t.Fatalf("audit state %q blockers %+v audit %+v", assessment.State, assessment.DecisionBlockers, assessment.Audit)
	}
	reportBytes, err := os.ReadFile(filepath.Join(out, "capacity.md"))
	if err != nil || len(reportBytes) == 0 {
		t.Fatalf("executive report missing: %v", err)
	}
	if !strings.Contains(stdout.String(), "Extended envelope (target class lab)") {
		t.Fatalf("plan did not state the extended envelope:\n%s", &stdout)
	}
}

func TestCapacityExtendedCalibrationLevelsFillBrackets(t *testing.T) {
	levels, probes := capacityExtendedCalibrationLevels([]int{2, 4, 8}, 5*time.Second)
	if !slices.Equal(levels, []int{2, 3, 4, 5, 6, 7, 8}) || probes != 2 {
		t.Fatalf("small ladder: %v %d", levels, probes)
	}
	ladder := explorer.PlannedLevels(explorer.Options{MinUsers: 2, MaxUsers: 500})
	levels, probes = capacityExtendedCalibrationLevels(ladder, 20*time.Second)
	if len(levels) != 12 || !slices.IsSorted(levels) || probes < 1 {
		t.Fatalf("large ladder: %v %d", levels, probes)
	}
	for _, level := range ladder {
		if !slices.Contains(levels, level) {
			t.Fatalf("ladder level %d missing from %v", level, levels)
		}
	}
	// A long level duration leaves no room for fill-ins within the wall cap.
	levels, _ = capacityExtendedCalibrationLevels(ladder, 65*time.Second)
	if len(levels) != len(ladder) {
		t.Fatalf("wall-capped fill-ins: %v", levels)
	}
}

func TestCapacityContinuesFromUncalibratedRefinementStop(t *testing.T) {
	result := explorer.Result{Outcome: "INCONCLUSIVE", Reason: explorer.UncalibratedRefinementReason + " Bracket 64-128 VUs.",
		Probes: []explorer.Probe{{Users: 32, Status: "PASS"}, {Users: 64, Status: "PASS"}, {Users: 128, Status: "FAIL"}}}
	if users, ok := capacityCoarseBracket(result); !ok || users != 64 {
		t.Fatalf("coarse bracket %d %v", users, ok)
	}
	result.Reason = "The configured request-rate ceiling may be limiting the workload."
	if _, ok := capacityCoarseBracket(result); ok {
		t.Fatal("other INCONCLUSIVE outcomes must still stop the protocol")
	}
}

func TestCapacityExtendedEnvelopeForwardsToTrafficStages(t *testing.T) {
	if args := capacityEnvelopeArgs(false, ""); args != nil {
		t.Fatalf("standard envelope forwarded %v", args)
	}
	got := strings.Join(capacityEnvelopeArgs(true, "staging"), " ")
	if got != "--extended-envelope --extended-load-authorized --target-class staging" {
		t.Fatalf("forwarded %q", got)
	}
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "200", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "3. Explore levels [2 4 8 16 32 64 128 200]") || !strings.Contains(out.String(), "bracket refinement probes only calibrated levels") {
		t.Fatalf("extended dry run above 50 VUs: %d %s %s", code, &out, &errOut)
	}
	if code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "200"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("standard envelope accepted 200 VUs: %d", code)
	}
}
