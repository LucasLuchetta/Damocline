package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCapacityLoopbackFindsBoundedQueueKnee(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the native bounded protocol")
	}
	slots := make(chan struct{}, 2)
	body := bytes.Repeat([]byte{'x'}, 128)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(body)
	}))
	defer target.Close()

	root := t.TempDir()
	scenario := filepath.Join(root, "scenario.json")
	out := filepath.Join(root, "protocol")
	if err := report.WriteAnyJSON(scenario, config.Scenario{
		Name: "bounded queue fixture", BaseURL: target.URL, Users: 2, Mode: "closed",
		Duration: "20s", Timeout: "2s", MaxRPS: 100, P95LimitMS: 10000, MaxErrorRate: .05,
		BaselinePath: "/ready", Steps: []config.Step{{Name: "work", Path: "/work"}},
		WorkloadRevision: "fixture-v1", ApplicationRevision: "fixture-v1", Environment: "lab",
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := capacityCmd([]string{
		"--config", scenario, "--output-dir", out, "--authorized", "--max-users", "3",
		"--response-bytes", "128", "--server-delay", "50ms", "--calibration-level-duration", "5s",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capacity returned %d\n%s\n%s", code, &stdout, &stderr)
	}
	exploration, err := readCapacityExplore(filepath.Join(out, "explore.json"))
	if err != nil || exploration.HighestPassingUsers == nil || *exploration.HighestPassingUsers != 2 ||
		exploration.FirstFailingUsers == nil || *exploration.FirstFailingUsers != 3 {
		t.Fatalf("expected a 2–3 VU bracket: %v (%v)\n%s", exploration.Outcome, err, &stdout)
	}
	evidence, err := capacityplan.ReadCampaign(filepath.Join(out, "campaign.json"))
	if err != nil || evidence.BaseUsers != 2 || evidence.Outcome != "PASS" {
		t.Fatalf("expected passing campaign at two VUs: %s (%v)\n%s", evidence.Outcome, err, &stdout)
	}
	data, err := os.ReadFile(filepath.Join(out, "audit.json"))
	var audit capacityplan.Assessment
	if err != nil || json.Unmarshal(data, &audit) != nil || audit.Audit.State != "OBSERVED_PASS_ONLY" || audit.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected audit verdict: %s / %s (%v)\n%s", audit.State, audit.Audit.State, err, &stdout)
	}
}
