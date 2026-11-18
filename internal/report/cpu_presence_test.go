package report

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMissingProcessMeanMustNotBecomeIdleHeadroom(t *testing.T) {
	run := validSyntheticQualityRun()
	run.SchemaVersion = CurrentSchemaVersion
	identity := validGeneratorIdentity()
	run.GeneratorIdentity = identity
	run.Generator.PeakProcessRSSBytes = 4096
	run.Generator.MeanProcessCPUUtilizationRatio = .95
	if GeneratorCapacityIssue(run) == "" {
		t.Fatal("CPU pressure did not block the headroom screen")
	}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var generator map[string]json.RawMessage
	if err := json.Unmarshal(raw["generator"], &generator); err != nil {
		t.Fatal(err)
	}
	delete(generator, "mean_process_cpu_utilization_ratio")
	raw["generator"], _ = json.Marshal(generator)
	data, _ = json.Marshal(raw)
	var restored Result
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Generator.MeanProcessCPUUtilizationRatio != 0 || restored.Generator.MeanProcessCPUAvailable() || GeneratorCapacityIssue(restored) == "" {
		t.Fatal("decoded absent mean was treated as measured idle headroom")
	}
	if err := ValidateMetricCountersInArtifactJSON(data); err == nil || !strings.Contains(err.Error(), "mean_process_cpu_utilization_ratio") {
		t.Fatalf("raw reader accepted unknown CPU as measured idle: %v", err)
	}
}

func TestClaimedCPUSourcePresenceAcrossArtifactFamilies(t *testing.T) {
	const validRun = `{"schema_version":33,"metrics":{"total":0,"success":0,"failures":0},"generator":{"process_cpu_valid":true,"mean_process_cpu_utilization_ratio":0,"peak_process_cpu_utilization_ratio":0,"go_cpu_valid":true,"go_cpu_busy_seconds":0,"go_cpu_capacity_seconds":1,"go_cpu_utilization_ratio":0}}`
	wrappers := []struct {
		name string
		wrap func(string) string
	}{
		{"run", func(s string) string { return s }},
		{"closed campaign", func(s string) string { return `{"schema_version":15,"trials":[{"phases":[{"run":` + s + `}]}]}` }},
		{"arrival campaign", func(s string) string {
			return `{"arrival_campaign_schema_version":8,"trials":[{"phases":[{"run":` + s + `}]}]}`
		}},
		{"pending trial", func(s string) string { return `{"trials":[],"pending_trial":{"phases":[{"run":` + s + `}]}}` }},
		{"exploration", func(s string) string { return `{"probes":[{"runs":[` + s + `]}]}` }},
	}
	fields := []string{"mean_process_cpu_utilization_ratio", "peak_process_cpu_utilization_ratio", "go_cpu_busy_seconds", "go_cpu_capacity_seconds", "go_cpu_utilization_ratio"}
	for _, wrapper := range wrappers {
		t.Run(wrapper.name, func(t *testing.T) {
			if err := ValidateMetricCountersInArtifactJSON([]byte(wrapper.wrap(validRun))); err != nil {
				t.Fatalf("explicit measured zeros rejected: %v", err)
			}
			for _, field := range fields {
				for _, null := range []bool{false, true} {
					var raw map[string]json.RawMessage
					_ = json.Unmarshal([]byte(validRun), &raw)
					var generator map[string]json.RawMessage
					_ = json.Unmarshal(raw["generator"], &generator)
					if null {
						generator[field] = json.RawMessage(`null`)
					} else {
						delete(generator, field)
					}
					raw["generator"], _ = json.Marshal(generator)
					data, _ := json.Marshal(raw)
					if err := ValidateMetricCountersInArtifactJSON([]byte(wrapper.wrap(string(data)))); err == nil || !strings.Contains(err.Error(), field) {
						t.Fatalf("missing/null %s accepted: %v", field, err)
					}
				}
			}
		})
	}
}

func TestCPUPresencePreservesUnknownSourcesAndHistoricalSchemas(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":11,"metrics":{"total":0,"success":0,"failures":0},"generator":{"process_cpu_valid":true,"go_cpu_valid":true}}`,
		`{"schema_version":33,"metrics":{"total":0,"success":0,"failures":0},"generator":{"process_cpu_valid":false,"go_cpu_valid":false}}`,
		`{"schema_version":33,"metrics":{"total":0,"success":0,"failures":0}}`,
	} {
		if err := ValidateMetricCountersInArtifactJSON([]byte(raw)); err != nil {
			t.Fatalf("unknown/historical evidence rejected: %v", err)
		}
	}
	const boundary = `{"schema_version":12,"metrics":{"total":0,"success":0,"failures":0},"generator":{"process_cpu_valid":true,"peak_process_cpu_utilization_ratio":0}}`
	if err := ValidateMetricCountersInArtifactJSON([]byte(boundary)); err == nil {
		t.Fatal("schema-v12 missing mean was accepted")
	}
}

func TestUnknownHistoricalMeanCannotQualifyCPUHeadroom(t *testing.T) {
	var run Result
	data := `{"schema_version":11,"metrics":{"total":1,"success":1,"failures":0},"generator":{"samples":2,"process_samples":2,"process_cpu_valid":true,"peak_process_cpu_utilization_ratio":0.95}}`
	if err := ValidateMetricCountersInArtifactJSON([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(data), &run); err != nil {
		t.Fatal(err)
	}
	if HasRequiredCPUSamples(run) {
		t.Fatal("unrecorded window mean qualified CPU headroom")
	}
	q := AssessQuality(run)
	if !strings.Contains(strings.Join(q.Findings, " "), "window-mean process CPU was not recorded") {
		t.Fatalf("missing-mean limitation not retained: %+v", q)
	}
	pressure := false
	for _, signal := range DetectSignals(run) {
		if signal.Code == "GENERATOR_CPU_PRESSURE" {
			pressure = true
		}
	}
	if !pressure {
		t.Fatal("unknown mean hid independently recorded high peak CPU")
	}
}
