package insights

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestReadRunGatesNetworkBaselineBySchema(t *testing.T) {
	got, err := ReadRun(writeRoot(t, readGoldenRoot(t, "37")))
	if err != nil || got.NetworkBaseline == nil || got.NetworkBaseline.Planned != 20 {
		t.Fatalf("schema-v37 golden was rejected or lost its baseline: %+v err=%v", got.NetworkBaseline, err)
	}
	root := readGoldenRoot(t, "36")
	root["network_baseline"] = map[string]any{"status": "SKIPPED", "skip_reason": "x"}
	if _, err := ReadRun(writeRoot(t, root)); err == nil {
		t.Fatal("schema-v36 report carrying network_baseline was accepted")
	}
	root = readGoldenRoot(t, "37")
	root["network_baseline"].(map[string]any)["method"] = "POST"
	if _, err := ReadRun(writeRoot(t, root)); err == nil {
		t.Fatal("malformed network baseline was accepted")
	}
}

func TestEvaluateWarnsWhenNetworkPathDominatesLatency(t *testing.T) {
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Outcome: "FAIL", Config: report.RunConfig{Users: 2, P95LimitMS: 1000},
		NetworkBaseline: &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Method: "GET", Planned: 20, Completed: 20,
			P50MS: 40, P95MS: 2600, MaxMS: 2700, MaxToP50Ratio: 67.5}}
	a := Evaluate(r, nil)
	found := false
	for _, f := range a.Findings {
		if f.Level == "LIMIT" && strings.Contains(f.Text, "network path may dominate") && strings.Contains(f.Text, "Do not conclude that the application cannot handle this load") {
			found = true
		}
	}
	if !found || !strings.Contains(a.Performance, "NETWORK PATH") || a.Capacity != "NOT DETERMINED" {
		t.Fatalf("missing network caveat: %+v", a)
	}
}
