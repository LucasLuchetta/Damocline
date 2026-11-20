package capacityplan

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// lightLeadingRun returns a fixture phase whose first window carries fewer
// requests and a lower p95 than the steady windows that follow it.
func lightLeadingRun(v config.Validated) report.Result {
	run := auditFixture().Trials[0].Phases[0].Run
	run.Target = v.Base.Scheme + "://" + v.Base.Host
	run.Config.Users = v.Scenario.Users
	run.Config.Duration = v.Duration.String()
	run.Config.WindowDuration = v.Window.String()
	run.Config.RampUp = v.Scenario.RampUp
	for i := range run.Windows {
		total, p95 := 30, 85.0
		if i == 0 {
			total, p95 = 21, 66
		}
		for _, metrics := range []*report.MetricSet{&run.Windows[i].Metrics, &run.Windows[i].Journeys, &run.Windows[i].Steps[0].Metrics} {
			metrics.Total, metrics.Success, metrics.P95MS = total, total, p95
		}
	}
	return run
}

func TestOfflineCampaignValidationReplaysRampAndDriftBaseline(t *testing.T) {
	base, err := config.Parse(config.Scenario{
		Name: "synthetic", BaseURL: "http://127.0.0.1:18080", Users: 2, Duration: "10s", Timeout: "1s", RampUp: "2s",
		MaxRPS: 40, P95LimitMS: 1000, MaxErrorRate: 0.1, WindowDuration: "2.5s",
		Steps: []config.Step{{Name: "request", Method: "GET", Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := campaign.WithRunner(context.Background(), base, campaign.Options{
		Warmup: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
	}, func(_ context.Context, v config.Validated) report.Result { return lightLeadingRun(v) })
	if err != nil || evidence.Outcome != "PASS" {
		t.Fatalf("campaign with a light leading window did not pass: %+v %v", evidence, err)
	}
	observation := evidence.Trials[0].Phases[1]
	if observation.Name != "observation" || observation.StabilityBaselineWindow != 1 || observation.RampUp != "0s" || evidence.Trials[0].Phases[0].RampUp != "2s" {
		t.Fatalf("phase ramp or drift reference was not recorded: %+v", evidence.Trials[0].Phases)
	}
	path := filepath.Join(t.TempDir(), "campaign.json")
	if err := report.WriteAnyJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	reloaded, err := ReadCampaign(path)
	if err != nil {
		t.Fatalf("offline recompute disagreed with the campaign decision: %v", err)
	}
	if err := ValidateSavedCampaign(reloaded); err != nil {
		t.Fatalf("offline recompute disagreed with the campaign decision: %v", err)
	}

	for name, mutate := range map[string]func(*campaign.Result){
		"baseline window": func(r *campaign.Result) { r.Trials[0].Phases[1].StabilityBaselineWindow = 0 },
		"phase ramp":      func(r *campaign.Result) { r.Trials[0].Phases[1].RampUp = "2s" },
		"native ramp":     func(r *campaign.Result) { r.Trials[0].Phases[1].Run.Config.RampUp = "2s" },
		"missing ramp":    func(r *campaign.Result) { r.Trials[1].Phases[2].RampUp = "" },
		"baseline rule":   func(r *campaign.Result) { r.DriftBaseline = campaign.DriftBaselineFirstWindow },
	} {
		forged, err := ReadCampaign(path)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&forged)
		if err := ValidateSavedCampaign(forged); err == nil {
			t.Fatalf("altered %s was accepted", name)
		}
	}

	historical := reloaded
	historical.SchemaVersion = 16
	historical.DriftBaseline = ""
	for i := range historical.Trials {
		for j := range historical.Trials[i].Phases {
			historical.Trials[i].Phases[j].RampUp = ""
			historical.Trials[i].Phases[j].StabilityBaselineWindow = 0
		}
	}
	if got := campaignStability(historical, historical.Trials[0].Phases[1].Run); got != "NON_STATIONARY" {
		t.Fatalf("historical schema must replay its first-window reference: %s", got)
	}
}
