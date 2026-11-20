package explorer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestDominantNetworkBaselinePreventsNoPassingLevel(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "FAIL", Config: report.RunConfig{P95LimitMS: 1000}, NetworkBaseline: &report.NetworkBaseline{
			Status: report.NetworkBaselineMeasured, Planned: 20, Completed: 20, P50MS: 40, P95MS: 2600, MaxMS: 2700, MaxToP50Ratio: 67.5}}
	})
	if err != nil || r.Outcome == "NO_PASSING_LEVEL" || r.Outcome != "INCONCLUSIVE" || r.FirstFailingUsers != nil ||
		!strings.Contains(r.Reason, "no application-capacity conclusion") {
		t.Fatalf("network-dominated failure was reported as an application limit: %+v err=%v", r, err)
	}
}
