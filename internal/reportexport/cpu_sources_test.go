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
