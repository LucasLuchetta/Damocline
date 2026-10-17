package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createGoalDryRunScenario(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.json")
	data := []byte(`{
		"name": "Synthetic local scenario",
		"base_url": "http://127.0.0.1:18080",
		"users": 2,
		"duration": "30s",
		"max_rps": 30,
		"p95_limit_ms": 1500,
		"mode": "closed",
		"steps": [{"name": "home", "method": "GET", "path": "/"}]
	}`)
	if err := os.WriteFile(path, data, 0600); err != nil { t.Fatal(err) }
	return path
}

func TestExploreGoalDryRunTestsExactTargetWithoutTraffic(t *testing.T) {
	path := createGoalDryRunScenario(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"explore", "--config", path, "--dry-run",
		"--min-users", "2", "--max-users", "20", "--peak-users", "12",
		"--growth-percent", "25", "--reserve-percent", "0"}, &stdout, &stderr)
	if code != 0 { t.Fatalf("dry-run returned %d: %s",code,stderr.String()) }
	for _, part := range []string{"Business workload target: 15", "exact target as a separate stage", "DRY RUN: no traffic generated"} {
		if !strings.Contains(stdout.String(),part) {t.Fatalf("missing %q: %s",part,stdout.String())}
	}
}

func TestExploreGoalNeverIncreasesConfiguredSafetyCeiling(t *testing.T) {
	path := createGoalDryRunScenario(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"explore", "--config", path, "--dry-run",
		"--min-users", "2", "--max-users", "20", "--peak-users", "200"}, &stdout, &stderr)
	if code != 0 {t.Fatalf("out-of-range goal should be informational, exit %d stderr %s",code,stderr.String())}
	for _, part := range []string{"target: 200", "outside configured search bounds", "NOT lift safe test limits"} {
		if !strings.Contains(stdout.String(),part) {t.Fatalf("missing %q: %s",part,stdout.String())}
	}
}

func TestExploreGoalRejectsGrowthWithoutPeak(t *testing.T) {
	path := createGoalDryRunScenario(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"explore", "--config", path, "--dry-run",
		"--growth-percent", "20"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "invalid business capacity goal") {
		t.Fatalf("invalid demand accepted: exit=%d stderr=%s",code,stderr.String())
	}
}
