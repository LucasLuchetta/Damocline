package cli

import (
	"bytes"
	"strings"
	"testing"
)

// A higher ladder start shortens the plan when lower levels are known to pass,
// and an invalid start is refused before traffic.
func TestCapacityMinUsersStartsLadderHigher(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	if code := capacityCmd([]string{"--config", scenario, "--dry-run", "--min-users", "8", "--max-users", "32"}, &out, &errOut); code != 0 {
		t.Fatalf("dry run failed: %d %s %s", code, &out, &errOut)
	}
	if !strings.Contains(out.String(), "Explore levels [8 16 32]") {
		t.Fatalf("ladder did not start at --min-users:\n%s", &out)
	}
	out.Reset()
	errOut.Reset()
	if code := capacityCmd([]string{"--config", scenario, "--dry-run", "--min-users", "40", "--max-users", "32"}, &out, &errOut); code == 0 {
		t.Fatalf("min above max accepted: %s", &out)
	}
}
