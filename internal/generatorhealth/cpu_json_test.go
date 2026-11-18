package generatorhealth

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHistoricalMeanCPUAbsenceSurvivesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, raw          string
		missing, available bool
	}{
		{"absent", `{"process_cpu_valid":true,"peak_process_cpu_utilization_ratio":0.8}`, true, false},
		{"null", `{"process_cpu_valid":true,"mean_process_cpu_utilization_ratio":null}`, true, false},
		{"measured zero", `{"process_cpu_valid":true,"mean_process_cpu_utilization_ratio":0}`, false, true},
		{"unavailable process", `{"process_cpu_valid":false}`, false, false},
		{"preserved unknown", `{"process_cpu_valid":true,"mean_process_cpu_utilization_ratio":0,"mean_process_cpu_unavailable":true}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got Summary
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatal(err)
			}
			if got.MeanProcessCPUUnavailable != tc.missing || got.MeanProcessCPUAvailable() != tc.available {
				t.Fatalf("missing=%t available=%t", got.MeanProcessCPUUnavailable, got.MeanProcessCPUAvailable())
			}
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var restored Summary
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, restored) {
				t.Fatal("CPU source availability changed during round-trip")
			}
		})
	}
}
