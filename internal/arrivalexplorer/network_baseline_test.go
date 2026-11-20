package arrivalexplorer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestDominantNetworkBaselinePreventsNoPassingRate(t *testing.T) {
	base := arrivalScenario(t, 100, 10)
	result, err := WithRunner(context.Background(), base, Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 2, Repeats: 2, StageDuration: 10 * time.Second,
	}, func(_ context.Context, v config.Validated) report.Result {
		r := completeRun(v, "FAIL", v.Scenario.ArrivalRate)
		r.Config.P95LimitMS = 1000
		r.NetworkBaseline = &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Phase: "before_load", Source: "first_get_step",
			Method: "GET", Path: "/", Planned: 20, Completed: 20, IntervalMS: 100, TimeoutMS: 5000,
			MinMS: 300, P50MS: 900, P95MS: 2600, MaxMS: 3000, MaxToP50Ratio: 3.333}
		return r
	})
	if err != nil || result.Outcome == "NO_PASSING_RATE" || result.Outcome != "INSUFFICIENT_EVIDENCE" ||
		!strings.Contains(result.Reason, "network path may dominate") {
		t.Fatalf("network-dominated failure was reported as an application limit: %+v err=%v", result, err)
	}
}
