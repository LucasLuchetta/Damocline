package cli

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

func TestCapacityContinuesFromInconclusiveExploreWithPassingLevel(t *testing.T) {
	passing := 150
	reason := "Network-path baseline was incomplete: 19 of 20 requests completed (1 failed, 0 not attempted within the bounded budget)."
	result := explorer.Result{Outcome: "INCONCLUSIVE", Reason: reason, HighestPassingUsers: &passing}
	if got := capacityPassingLevel(result, false); got != 150 {
		t.Fatalf("capacity level = %d, want 150", got)
	}
	note := capacityUnresolvedNote(result)
	if !strings.Contains(note, "INCONCLUSIVE") || !strings.Contains(note, "150 VUs") || !strings.Contains(note, "unresolved") || !strings.Contains(note, reason) {
		t.Fatalf("unresolved note missing facts: %q", note)
	}
	if capacityUnresolvedNote(explorer.Result{Outcome: "BRACKETED", HighestPassingUsers: &passing}) != "" {
		t.Fatal("resolved exploration reported unresolved levels")
	}
	if capacityPassingLevel(explorer.Result{Outcome: "INCONCLUSIVE", Reason: reason}, false) != 0 {
		t.Fatal("inconclusive exploration without a passing level produced a capacity level")
	}
}
