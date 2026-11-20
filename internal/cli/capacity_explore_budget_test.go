package cli

import (
	"bytes"
	"strings"
	"testing"
)

// An explicit explore budget smaller than the planned stages must be refused
// before traffic, and the default must grow to fit instead of aborting the
// ladder midway.
func TestCapacityExploreBudgetFitsPlannedStages(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "8", "--explore-budget", "30s"}, &out, &errOut)
	if code == 0 || !strings.Contains(out.String()+errOut.String(), "too small") {
		t.Fatalf("undersized explicit budget accepted: code=%d out=%s err=%s", code, &out, &errOut)
	}
	out.Reset()
	errOut.Reset()
	if code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "8"}, &out, &errOut); code != 0 {
		t.Fatalf("default budget dry run failed: %d %s %s", code, &out, &errOut)
	}
}
