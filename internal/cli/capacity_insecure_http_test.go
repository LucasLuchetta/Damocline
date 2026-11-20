package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

// Plaintext targets on a private network are common. The capacity protocol
// must require the same explicit opt-in as run, explore and campaign, and
// accept it when given, instead of offering no way to proceed.
func TestCapacityRequiresAndForwardsExplicitPlaintextOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.json")
	data, err := json.Marshal(config.Scenario{
		Name: "fixture", BaseURL: "http://app.example.com", Users: 2, Duration: "20s",
		P95LimitMS: 750, MaxErrorRate: .01, MaxRPS: 100,
		BaselinePath: "/baseline", Steps: []config.Step{{Name: "fast", Path: "/fast"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := capacityCmd([]string{"--config", path, "--dry-run", "--max-users", "4"}, &out, &errOut); code == 0 || !strings.Contains(out.String()+errOut.String(), "--allow-insecure-http") {
		t.Fatalf("plaintext non-loopback target must require the opt-in: code=%d out=%s err=%s", code, &out, &errOut)
	}
	out.Reset()
	errOut.Reset()
	if code := capacityCmd([]string{"--config", path, "--dry-run", "--max-users", "4", "--allow-insecure-http"}, &out, &errOut); code != 0 {
		t.Fatalf("dry run with explicit opt-in failed: %d %s %s", code, &out, &errOut)
	}
	if got := capacityTransportArgs(true); len(got) != 1 || got[0] != "--allow-insecure-http" {
		t.Fatalf("opt-in is not forwarded to traffic stages: %v", got)
	}
	if got := capacityTransportArgs(false); len(got) != 0 {
		t.Fatalf("no opt-in must forward nothing: %v", got)
	}
}
