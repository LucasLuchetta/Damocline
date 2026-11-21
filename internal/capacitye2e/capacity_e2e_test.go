// Package capacitye2e runs the full capacity protocol end to end against a
// loopback fixture. It is a separate package so its few minutes run in
// parallel with the other package tests.
package capacitye2e

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
	"github.com/LucasLuchetta/Damocline/internal/cli"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

// knownCapacityTarget serves /work in 150ms while at most workers requests
// are in flight and in 2s once that pool is exceeded. A zero-think closed
// loop therefore meets a 1s p95 SLO at `workers` VUs or fewer and fails it
// above. The SLO leaves 850ms for host scheduling inflation of the 150ms
// service time: with a 400ms SLO, CPU load from parallel package tests
// inflated passing levels past the SLO (even 4 VUs and the 91 VU campaign
// failed), while the 2s over-capacity delay still fails it twice over.
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
			delay = 2 * time.Second
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

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

// TestCapacityExtendedProtocolFindsKnownCapacityEndToEnd runs the full
// capacity chain (baseline, self-calibration, extended explore above 50 VUs,
// calibrated campaign, audit, executive report) against a loopback server
// whose capacity is known in advance.
func TestCapacityExtendedProtocolFindsKnownCapacityEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end capacity protocol runs for a few minutes")
	}
	t.Cleanup(cli.SetCapacityStageFloorsForTest(10*time.Second, 2*time.Second, 3*time.Second))
	const maxUsers = 100
	const levelDuration = 5 * time.Second
	calibrated := cli.CapacityCalibrationLevels(2, maxUsers, levelDuration)
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
		MaxRPS: 1000, P95LimitMS: 1000, MaxErrorRate: .01, Environment: "lab",
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
	code := cli.Run([]string{"capacity",
		"--config", scenarioPath, "--authorized", "--output-dir", out, "--max-users", "100",
		"--response-bytes", "2", "--server-delay", "150ms", "--calibration-level-duration", levelDuration.String(),
		"--extended-envelope", "--extended-load-authorized", "--target-class", "lab",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capacity exit %d\nstdout:\n%s\nstderr:\n%s", code, &stdout, &stderr)
	}
	log := stdout.String()

	var selfCalibration calibration.ScenarioRecord
	readJSON(t, filepath.Join(out, "selfcal-explore.json"), &selfCalibration)
	if selfCalibration.Status != calibration.ScenarioCalibrationPassed {
		t.Fatalf("self-calibration did not pass: %s\n%s", selfCalibration.Status, log)
	}

	// The planned explore budget must cover the explore stage's real wall
	// time, which starts after calibration and ends when explore.json is
	// written.
	var manifest struct{ ExploreBudget time.Duration }
	readJSON(t, filepath.Join(out, "protocol.json"), &manifest)
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

	var exploration explorer.Result
	readJSON(t, filepath.Join(out, "explore.json"), &exploration)
	got := 0
	for _, probe := range exploration.Probes {
		if probe.Status == "PASS" {
			got = max(got, probe.Users)
		}
	}
	if got != known {
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

	var assessment capacityplan.Assessment
	readJSON(t, filepath.Join(out, "audit.json"), &assessment)
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
