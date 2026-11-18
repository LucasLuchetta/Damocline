package insights

import (
	"encoding/json"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"strings"
	"testing"
)

func TestHistoricalProcessMeanIsDescribedAsUnknown(t *testing.T) {
	var health generatorhealth.Summary
	if err := json.Unmarshal([]byte(`{"process_cpu_valid":true,"process_samples":2,"peak_process_cpu_utilization_ratio":0.75}`), &health); err != nil {
		t.Fatal(err)
	}
	run := synthetic()
	run.SchemaVersion = 11
	run.Generator = &health
	found := false
	for _, finding := range Evaluate(run, nil).Findings {
		found = found || strings.Contains(finding.Text, "Window-mean generator process CPU was not recorded")
	}
	if !found {
		t.Fatal("historical mean absence was not disclosed")
	}
}
