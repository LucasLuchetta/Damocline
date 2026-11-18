package reportexport

import (
	"encoding/json"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestExportKeepsUnavailableRuntimeCPUDistinctFromMeasuredProcessCPU(t *testing.T) {
	out := FromRun(report.Result{SchemaVersion: report.CurrentSchemaVersion, Generator: &generatorhealth.Summary{
		Samples: 4, GoCPUValid: false, ProcessCPUValid: true, MeanProcessCPUUtilizationRatio: .25,
	}})
	if out.Run == nil || out.Run.Generator == nil || out.Run.Generator.GoCPUValid ||
		!out.Run.Generator.ProcessCPUValid || out.Run.Generator.MeanProcessCPU != .25 {
		t.Fatalf("redaction lost CPU source availability: %+v", out.Run)
	}
	data, err := Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Run struct {
			Generator struct {
				GoCPUValid      *bool `json:"go_cpu_valid"`
				ProcessCPUValid *bool `json:"process_cpu_valid"`
			} `json:"generator"`
		} `json:"run"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	g := decoded.Run.Generator
	if g.GoCPUValid == nil || *g.GoCPUValid || g.ProcessCPUValid == nil || !*g.ProcessCPUValid {
		t.Fatal("serialized export omitted the explicit false/true availability flags")
	}
}

func TestExportMarksHistoricalMeanCPUUnknownWithoutLosingPeak(t *testing.T) {
	var health generatorhealth.Summary
	if err := json.Unmarshal([]byte(`{"process_cpu_valid":true,"peak_process_cpu_utilization_ratio":0.75}`), &health); err != nil {
		t.Fatal(err)
	}
	out := FromRun(report.Result{SchemaVersion: 11, Generator: &health})
	if !out.Run.Generator.ProcessCPUValid || out.Run.Generator.MeanProcessCPUValid || out.Run.Generator.PeakProcessCPU != .75 {
		t.Fatal("export lost independent peak/mean availability")
	}
	data, err := Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("invalid export")
	}
	var root map[string]json.RawMessage
	_ = json.Unmarshal(data, &root)
	var run map[string]json.RawMessage
	_ = json.Unmarshal(root["run"], &run)
	var generator map[string]json.RawMessage
	_ = json.Unmarshal(run["generator"], &generator)
	if string(generator["mean_process_cpu_valid"]) != "false" {
		t.Fatal("export omitted the explicit unavailable-mean flag")
	}
}
