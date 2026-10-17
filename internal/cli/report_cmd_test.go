package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestReportCommandPrintsResponsibleConclusions(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.json")
	r := report.Result{SchemaVersion: 2, Name: "Synthetic smoke", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: 1, Duration: "30s", P95LimitMS: 1500, MaxRPS: 5},
		Metrics: report.MetricSet{Total: 10, Success: 10, P95MS: 250, P99MS: 310}, Outcome: "PASS"}
	if err := report.WriteJSON(file, r); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if exit := Run([]string{"report", "--file", file}, &out, &errOut); exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, errOut.String())
	}
	for _, want := range []string{"OBJECTIVES MET", "NOT DETERMINED", "NOT MEASURED", "Only one virtual user"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out.String())
		}
	}
}
func TestReportCommandRejectsMissingReport(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"report", "--file", "does-not-exist.json"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "cannot read run") {
		t.Fatalf("code %d, stderr %s", code, errOut.String())
	}
}
