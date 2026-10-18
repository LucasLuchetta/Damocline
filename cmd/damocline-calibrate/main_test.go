package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestPersistedSoakQualityIsRecomputed(t *testing.T) {
	soak := report.Result{SchemaVersion: report.CurrentSchemaVersion}
	soak.Quality = report.AssessQuality(soak)
	path := filepath.Join(t.TempDir(), "calibration.json")
	if err := report.WriteAnyJSON(path, assessment{SoakRun: soak}); err != nil {
		t.Fatal(err)
	}
	if err := validatePersistedSoakQuality(path, soak.Quality.State); err != nil {
		t.Fatalf("matching persisted quality did not recompute: %v", err)
	}
}

func TestPersistedPositiveQualityWithoutEvidenceIsRejected(t *testing.T) {
	soak := report.Result{SchemaVersion: report.CurrentSchemaVersion, Quality: &report.Quality{State: report.QualityValid}}
	path := filepath.Join(t.TempDir(), "calibration.json")
	if err := report.WriteAnyJSON(path, assessment{SoakRun: soak}); err != nil {
		t.Fatal(err)
	}
	if err := validatePersistedSoakQuality(path, report.QualityValid); err == nil {
		t.Fatal("stale positive quality claim was accepted without recomputable evidence")
	}
}

func TestCheckReconcilesHealthyStagesWithSeparateSoakWork(t *testing.T) {
	metrics := passingStageFixture().Metrics
	run := report.Result{
		ArrivalsScheduled: 10, IterationsStarted: 10, IterationsCompleted: 10,
		Metrics: metrics, Journeys: metrics, Outcome: "PASS",
		Steps:   []report.StepResult{{Method: "GET", Path: "/work", Metrics: metrics}},
		Quality: &report.Quality{State: report.QualityLimited},
		Generator: &generatorhealth.Summary{
			Samples: 2, GoCPUValid: true, ProcessSamples: 2,
			ProcessCPUValid: true, PeakProcessRSSBytes: 1,
		},
	}
	server := reference.Snapshot{Received: 310, Completed: 310, Success: 310}
	if issues := check([]report.Result{run}, server, []int{2}); len(issues) != 0 {
		t.Fatalf("healthy stages plus the independent 30-second soak should reconcile: %v", issues)
	}
}

func TestCheckSoakRequiresCompleteBoundedSchedulerLagEvidence(t *testing.T) {
	const planned = 300
	base := report.Result{
		ArrivalsScheduled: planned, IterationsStarted: planned, IterationsCompleted: planned,
		Metrics:  report.MetricSet{Total: planned, Success: planned},
		Journeys: report.MetricSet{Total: planned, Success: planned}, Outcome: "PASS",
		Generator: &generatorhealth.Summary{Samples: 100, GoCPUValid: true, ProcessSamples: 100,
			ProcessCPUValid: true, PeakProcessRSSBytes: 1, GoCPUUtilizationRatio: 0.1,
			MeanProcessCPUUtilizationRatio: 0.1, PeakHeapAllocBytes: 1, PeakGoroutines: 1},
		SchedulerLagSamples: planned, SchedulerLagMethod: calibration.SchedulerDispatchLagMethod,
		SchedulerLagP95MS: 50, SchedulerLagMaxMS: 99.999,
	}
	if issues := checkSoak(base); len(issues) != 0 {
		t.Fatalf("lag at the documented p95 bound should pass: %v", issues)
	}
	cases := []struct {
		name   string
		mutate func(*report.Result)
	}{
		{name: "missing samples", mutate: func(r *report.Result) { r.SchedulerLagSamples-- }},
		{name: "missing method", mutate: func(r *report.Result) { r.SchedulerLagMethod = "" }},
		{name: "p95 above bound", mutate: func(r *report.Result) { r.SchedulerLagP95MS = 50.001 }},
		{name: "maximum at exclusive bound", mutate: func(r *report.Result) { r.SchedulerLagMaxMS = 100 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base
			tc.mutate(&got)
			if issues := checkSoak(got); len(issues) != 1 || !strings.Contains(issues[0], "soak scheduler lag failed") {
				t.Fatalf("invalid scheduler evidence was not rejected precisely: %v", issues)
			}
		})
	}
}

func TestCheckStageIdentifiesEachFailedEvidenceDimension(t *testing.T) {
	metrics := report.MetricSet{Total: 7, Success: 6, Failures: 1, P50MS: math.NaN(), P95MS: 900}
	run := report.Result{
		ArrivalsScheduled: 10, IterationsStarted: 8, IterationsCompleted: 7, ArrivalsDropped: 2,
		Metrics: metrics, Journeys: report.MetricSet{Total: 8, Success: 7, Failures: 1}, Outcome: "FAIL",
		Quality:           &report.Quality{State: report.QualityInvalid},
		SchedulerLagP95MS: 300, SchedulerLagMaxMS: 900,
		Generator: &generatorhealth.Summary{Samples: 1, ProcessSamples: 1, PeakProcessRSSBytes: 600 << 20,
			GoCPUValid: true, GoCPUUtilizationRatio: 1.1, ProcessCPUValid: true, PeakProcessCPUUtilizationRatio: 1.2,
			PeakHeapAllocBytes: 300 << 20},
	}
	issues := checkStage(10, 10, run)
	joined := strings.Join(issues, "\n")
	for _, expected := range []string{"stage 10 arrivals:", "stage 10 request outcomes:", "stage 10 response body:", "stage 10 journey outcomes:", "stage 10 outcome:", "stage 10 measurement quality:", "stage 10 latency envelope:", "stage 10 generator envelope:", "stage 10 scheduler lag:"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing diagnostic %q in %s", expected, joined)
		}
	}
	for _, expected := range []string{"scheduled/started/completed/dropped=10/10/10/0", "got 10/8/7/2", "NaN", "900.000ms", "scheduler lag"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing observed or expected value %q in %s", expected, joined)
		}
	}
}

func TestCheckStageRejectsNonFiniteLatency(t *testing.T) {
	run := passingStageFixture()
	run.Metrics.P50MS = math.NaN()
	run.Metrics.P95MS = math.Inf(1)
	issues := checkStage(2, 10, run)
	if len(issues) != 1 || !strings.Contains(issues[0], "latency envelope") {
		t.Fatalf("non-finite latency must fail the fixture gate with a precise diagnostic: %v", issues)
	}
}

func TestCheckStageRequiresKnownMeasurementQuality(t *testing.T) {
	for _, tc := range []struct {
		name    string
		quality *report.Quality
		want    string
	}{
		{name: "missing"},
		{name: "invalid", quality: &report.Quality{State: report.QualityInvalid}, want: "INVALID"},
		{name: "unknown", quality: &report.Quality{State: "FUTURE_STATE"}, want: "FUTURE_STATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := passingStageFixture()
			run.Quality = tc.quality
			issues := strings.Join(checkStage(2, 10, run), "\n")
			if !strings.Contains(issues, "stage 2 measurement quality:") || (tc.want != "" && !strings.Contains(issues, tc.want)) {
				t.Fatalf("missing, invalid, or unknown quality must fail calibration with a diagnostic, got %q", issues)
			}
		})
	}
}

func TestCheckStageUsesSustainedProcessCPUAndReportsIntervalPeak(t *testing.T) {
	base := passingStageFixture()
	base.Generator.PeakProcessCPUUtilizationRatio = 1.3
	base.Generator.MeanProcessCPUUtilizationRatio = 0.08
	if issues := checkStage(2, 10, base); len(issues) != 0 {
		t.Fatalf("an isolated interval peak must remain diagnostic when sustained CPU stays within the envelope: %v", issues)
	}
	base.Generator.MeanProcessCPUUtilizationRatio = 0.9
	issues := checkStage(2, 10, base)
	joined := strings.Join(issues, "\n")
	if !strings.Contains(joined, "stage 2 generator envelope") || !strings.Contains(joined, "process_cpu_mean=0.900") || !strings.Contains(joined, "process_cpu_peak=1.300") {
		t.Fatalf("sustained process CPU at the threshold must fail with both mean and peak values: %s", joined)
	}
}

func TestCheckStageRejectsResponseBodyOutsideObservedFixtureShape(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*report.Result)
	}{
		{name: "zero-byte body", mutate: func(r *report.Result) {
			r.Metrics.ResponseBody = report.ResponseBodyDiagnostics{CompleteObservations: 10}
			r.Steps[0].Metrics.ResponseBody = report.ResponseBodyDiagnostics{CompleteObservations: 10}
		}},
		{name: "larger body", mutate: func(r *report.Result) {
			body := report.ResponseBodyDiagnostics{CompleteObservations: 10, MinBytes: 64 << 10,
				MaxBytes: 64 << 10, TotalBytes: 10 * (64 << 10)}
			r.Metrics.ResponseBody, r.Steps[0].Metrics.ResponseBody = body, body
		}},
		{name: "incomplete read", mutate: func(r *report.Result) {
			r.Metrics.ResponseBody.IncompleteObservations++
			r.Steps[0].Metrics.ResponseBody.IncompleteObservations++
		}},
		{name: "step ledger mismatch", mutate: func(r *report.Result) {
			r.Steps[0].Metrics.ResponseBody.TotalBytes--
		}},
		{name: "aggregate ledger mismatch", mutate: func(r *report.Result) {
			r.Metrics.ResponseBody.TotalBytes--
		}},
		{name: "step outcome mismatch", mutate: func(r *report.Result) {
			r.Steps[0].Metrics.Success--
		}},
		{name: "missing step evidence", mutate: func(r *report.Result) {
			r.Steps = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := passingStageFixture()
			tc.mutate(&run)
			issues := checkStage(2, 10, run)
			if len(issues) != 1 || !strings.Contains(issues[0], "stage 2 response body") {
				t.Fatalf("response shape discrepancy was not rejected precisely: %v", issues)
			}
		})
	}
}

func passingStageFixture() report.Result {
	body := report.ResponseBodyDiagnostics{CompleteObservations: 10, MinBytes: 4 << 10,
		MaxBytes: 4 << 10, TotalBytes: 10 * (4 << 10)}
	metrics := report.MetricSet{Total: 10, Success: 10, P50MS: 20, P95MS: 25, ResponseBody: body}
	return report.Result{ArrivalsScheduled: 10, IterationsStarted: 10, IterationsCompleted: 10,
		Metrics: metrics, Journeys: metrics, Outcome: "PASS", Quality: &report.Quality{State: report.QualityLimited},
		Steps: []report.StepResult{{Method: "GET", Path: "/work", Metrics: metrics}},
		Generator: &generatorhealth.Summary{Samples: 2, GoCPUValid: true, ProcessSamples: 2,
			ProcessCPUValid: true, PeakProcessRSSBytes: 1}}
}
