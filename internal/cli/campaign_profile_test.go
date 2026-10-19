package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/campaignprofile"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestCampaignProfileRunDryRunAndLoopbackReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer server.Close()
	profile, err := campaignprofile.New("loopback", "", config.Scenario{Name: "loopback", Environment: "development", BaseURL: server.URL, Users: 2, Duration: "10s", Timeout: "1s", MaxRPS: 10, P95LimitMS: 1000, MaxErrorRate: 1, Steps: []config.Step{{Name: "health", Method: "GET", Path: "/"}}}, campaign.Options{Observation: 10 * time.Second, Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	store := campaignprofile.Store{Dir: filepath.Join(t.TempDir(), "profiles")}
	if err := store.Save(profile, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := campaignProfileRun([]string{"--name", "loopback", "--dry-run"}, store, &stdout, &stderr); code != 0 {
		t.Fatalf("dry run code=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("DRY RUN")) {
		t.Fatalf("dry run summary missing: %s", stdout.String())
	}
	out := filepath.Join(t.TempDir(), "report.json")
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileRun([]string{"--name", "loopback", "--authorized", "--out", out}, store, &stdout, &stderr); code == 2 {
		t.Fatalf("loopback campaign rejected: %s", stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("campaign report not written: %v; stderr=%s", err, stderr.String())
	}
}
