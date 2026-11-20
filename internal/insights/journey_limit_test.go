package insights

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestReadRunGatesJourneyP95LimitBySchema(t *testing.T) {
	t.Run("v35 accepted", func(t *testing.T) {
		root := readGoldenRoot(t, "35")
		if _, err := ReadRun(writeRoot(t, root)); err != nil {
			t.Fatalf("schema-v35 journey limit was rejected: %v", err)
		}
	})
	t.Run("v34 carrying v35 field rejected", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		root["config"].(map[string]any)["journey_p95_limit_ms"] = 8000
		if _, err := ReadRun(writeRoot(t, root)); err == nil {
			t.Fatal("schema-v34 report carrying journey_p95_limit_ms was accepted")
		}
	})
	for _, limit := range []float64{-1, 120001} {
		root := readGoldenRoot(t, "35")
		root["config"].(map[string]any)["journey_p95_limit_ms"] = limit
		if _, err := ReadRun(writeRoot(t, root)); err == nil {
			t.Fatalf("out-of-range journey limit %v was accepted", limit)
		}
	}
}

func TestEvaluateWarnsWhenJourneyObjectiveIncludesThinkTime(t *testing.T) {
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Config: report.RunConfig{Users: 1, P95LimitMS: 100, ThinkTime: "2s"}}
	if !hasFinding(Evaluate(r, nil), report.JourneyLimitIncludesThinkTimeWarning) {
		t.Fatal("missing think-time journey objective caution")
	}
	r.Config.JourneyP95LimitMS = 10000
	if hasFinding(Evaluate(r, nil), report.JourneyLimitIncludesThinkTimeWarning) {
		t.Fatal("explicit journey limit still produced the think-time caution")
	}
	r.Config.JourneyP95LimitMS, r.Config.ThinkTime = 0, ""
	if hasFinding(Evaluate(r, nil), report.JourneyLimitIncludesThinkTimeWarning) {
		t.Fatal("scenario without think time produced the think-time caution")
	}
}

func hasFinding(a Assessment, text string) bool {
	for _, finding := range a.Findings {
		if strings.Contains(finding.Text, text) {
			return true
		}
	}
	return false
}
