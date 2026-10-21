package report

import (
	"encoding/json"
	"testing"
)

func TestValidateMetricCountersInArtifactJSONRequiresExplicitCounters(t *testing.T) {
	base := `{"schema_version":1,"metrics":{"total":0,"success":0,"failures":0}}`
	if err := ValidateMetricCountersInArtifactJSON([]byte(base)); err != nil {
		t.Fatalf("explicit zero counters should be accepted: %v", err)
	}
	for _, missing := range []string{"total", "success", "failures"} {
		t.Run("missing_"+missing, func(t *testing.T) {
			candidate := `{"schema_version":1,"metrics":{"total":0,"success":0,"failures":0}}`
			candidate = removeJSONField(t, candidate, missing)
			if err := ValidateMetricCountersInArtifactJSON([]byte(candidate)); err == nil {
				t.Fatalf("missing %s counter was interpreted as measured zero", missing)
			}
		})
	}
}

func TestValidateMetricCountersInArtifactJSONTraversesPersistedReportFamilies(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "closed_loop_campaign", json: `{"trials":[{"phases":[{"run":{"metrics":{"total":0,"success":0,"failures":0}}}]}]}`},
		{name: "open_arrival_exploration", json: `{"probes":[{"runs":[{"metrics":{"total":0,"success":0,"failures":0}}]}]}`},
		{name: "window_metrics", json: `{"metrics":{"total":0,"success":0,"failures":0},"windows":[{"metrics":{"total":0,"success":0,"failures":0},"journeys":{"total":0,"success":0,"failures":0}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMetricCountersInArtifactJSON([]byte(tc.json)); err != nil {
				t.Fatalf("explicit zero counters should be accepted: %v", err)
			}
			candidate := removeJSONField(t, tc.json, "total")
			if err := ValidateMetricCountersInArtifactJSON([]byte(candidate)); err == nil {
				t.Fatal("nested missing counter was interpreted as measured zero")
			}
		})
	}
}

func TestValidateMetricCountersInArtifactJSONRejectsMissingNestedLedgers(t *testing.T) {
	base := `"total":0,"success":0,"failures":0`
	tests := []struct {
		name string
		json string
	}{
		{name: "step", json: `{"metrics":{` + base + `},"steps":[{"metrics":{"success":0,"failures":0}}]}`},
		{name: "profile", json: `{"metrics":{` + base + `},"profiles":[{"journeys":{"total":0,"failures":0}}]}`},
		{name: "window_aggregate", json: `{"schema_version":22,"metrics":{` + base + `},"windows":[{"metrics":{"success":0,"failures":0},"journeys":{` + base + `}}]}`},
		{name: "window_journey", json: `{"schema_version":22,"metrics":{` + base + `},"windows":[{"metrics":{` + base + `},"journeys":{"total":0,"success":0}}]}`},
		{name: "window_step", json: `{"schema_version":22,"metrics":{` + base + `},"windows":[{"metrics":{` + base + `},"journeys":{` + base + `},"steps":[{"metrics":{"total":0,"success":0}}]}]}`},
		{name: "window_profile", json: `{"schema_version":22,"metrics":{` + base + `},"windows":[{"metrics":{` + base + `},"journeys":{` + base + `},"profiles":[{"metrics":{"total":0,"failures":0}}]}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMetricCountersInArtifactJSON([]byte(tc.json)); err == nil {
				t.Fatal("nested missing counter was interpreted as measured zero")
			}
		})
	}
}

func removeJSONField(t *testing.T, input, key string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatal(err)
	}
	var remove func(any)
	remove = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			delete(typed, key)
			for _, child := range typed {
				remove(child)
			}
		case []any:
			for _, child := range typed {
				remove(child)
			}
		}
	}
	remove(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
