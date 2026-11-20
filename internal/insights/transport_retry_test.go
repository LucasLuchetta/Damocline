package insights

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestReadRunGatesTransportRetryCountBySchema(t *testing.T) {
	if _, err := ReadRun(writeRoot(t, readGoldenRoot(t, "36"))); err != nil {
		t.Fatalf("schema-v36 golden was rejected: %v", err)
	}
	root := readGoldenRoot(t, "35")
	step := root["steps"].([]any)[0].(map[string]any)
	step["http_trace"] = map[string]any{"transport_retry_count": 1}
	if _, err := ReadRun(writeRoot(t, root)); err == nil {
		t.Fatal("schema-v35 report carrying transport_retry_count was accepted")
	}
}

func TestEvaluateReportsTransportRetries(t *testing.T) {
	r := report.Result{SchemaVersion: report.CurrentSchemaVersion, Config: report.RunConfig{Users: 1},
		Steps: []report.StepResult{{Name: "page", HTTPTrace: report.HTTPTrace{RequestAttemptCount: 49, TransportRetryCount: 1}}}}
	found := false
	for _, f := range Evaluate(r, nil).Findings {
		if f.Level == "CAUTION" && strings.Contains(f.Text, "Transparent transport retries: 1 of 49") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing transparent transport retry caution")
	}
}
