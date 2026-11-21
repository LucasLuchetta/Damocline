package cli

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
)

func TestCapacityCampaignBudgetLimitMatchesCampaignValidate(t *testing.T) {
	if got := capacityCampaignBudgetLimit(campaign.Options{}); got != campaign.DefaultWallTimeLimit {
		t.Fatalf("default limit %s", got)
	}
	if got := capacityCampaignBudgetLimit(campaign.Options{LongRun: true}); got != campaign.LongRunWallTimeLimit {
		t.Fatalf("long-run limit %s", got)
	}
}
