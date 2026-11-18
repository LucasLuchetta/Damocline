package report

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

func TestRequiredCPUSourcesPreserveLegacyContract(t *testing.T) {
	for _, test := range []struct {
		name                  string
		version               int
		goValid, processValid bool
		processSamples        int
		want                  bool
	}{
		{name: "legacy unknown runtime estimate", version: 32, processValid: true, processSamples: 2},
		{name: "legacy complete sources", version: 32, goValid: true, processValid: true, processSamples: 2, want: true},
		{name: "current measured process", version: 33, processValid: true, processSamples: 2, want: true},
		{name: "runtime cannot replace process CPU", version: 33, goValid: true, processSamples: 2},
		{name: "insufficient process samples", version: 33, processValid: true, processSamples: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := Result{SchemaVersion: test.version, Generator: &generatorhealth.Summary{
				Samples: 2, GoCPUValid: test.goValid, ProcessCPUValid: test.processValid, ProcessSamples: test.processSamples,
			}}
			if got := HasRequiredCPUSamples(run); got != test.want {
				t.Fatalf("CPU source availability=%t; want %t", got, test.want)
			}
		})
	}
	if HasRequiredCPUSamples(Result{SchemaVersion: 33}) {
		t.Fatal("absent generator telemetry qualified as measured CPU")
	}
}

func TestCPUUnavailableValuesRemainVersioned(t *testing.T) {
	run := Result{SchemaVersion: 33, Generator: &generatorhealth.Summary{
		GoCPUValid: false, GoCPUBusySeconds: 2, GoCPUCapacitySeconds: 10, GoCPUUtilizationRatio: .2,
	}}
	const finding = "Unavailable Go runtime CPU estimate retains numeric observations."
	if q := AssessQuality(run); q.State != QualityInvalid || !strings.Contains(strings.Join(q.Findings, " "), finding) {
		t.Fatalf("contradictory source availability was accepted: %+v", q)
	}
	run.SchemaVersion = 32
	if q := AssessQuality(run); strings.Contains(strings.Join(q.Findings, " "), finding) {
		t.Fatalf("historical CPU fields were reinterpreted: %+v", q)
	}
}

func TestIndependentCPUSourcesDoNotHideMeasuredPressure(t *testing.T) {
	for _, test := range []struct {
		name     string
		health   generatorhealth.Summary
		pressure bool
	}{
		{name: "process mean with unavailable runtime estimate", health: generatorhealth.Summary{Samples: 2, ProcessCPUValid: true, MeanProcessCPUUtilizationRatio: .95}, pressure: true},
		{name: "runtime with unavailable process CPU", health: generatorhealth.Summary{Samples: 2, GoCPUValid: true, GoCPUUtilizationRatio: .95}, pressure: true},
		{name: "unavailable values do not establish pressure", health: generatorhealth.Summary{Samples: 2, MeanProcessCPUUtilizationRatio: .95, GoCPUUtilizationRatio: .95}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pressure, unavailable := false, false
			for _, signal := range DetectSignals(Result{Generator: &test.health}) {
				if signal.Code == "GENERATOR_CPU_PRESSURE" {
					pressure = true
					if signal.Origin != "client" {
						t.Fatalf("CPU pressure was attributed to a target: %+v", signal)
					}
				}
				unavailable = unavailable || signal.Code == "GENERATOR_TELEMETRY_UNAVAILABLE"
			}
			if pressure != test.pressure || !unavailable {
				t.Fatalf("pressure=%t unavailable=%t; want pressure=%t and explicit missing source", pressure, unavailable, test.pressure)
			}
		})
	}
}
