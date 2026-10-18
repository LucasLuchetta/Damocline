package main

import (
	"math"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCheckReconcilesHealthyStagesWithSeparateSoakWork(t *testing.T) {
	metrics := report.MetricSet{Total: 10, Success: 10, P50MS: 20, P95MS: 25}
	run := report.Result{
		ArrivalsScheduled: 10, IterationsStarted: 10, IterationsCompleted: 10,
		Metrics: metrics, Journeys: metrics, Outcome: "PASS",
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
	for _, expected := range []string{"stage 10 arrivals:", "stage 10 request outcomes:", "stage 10 journey outcomes:", "stage 10 outcome:", "stage 10 measurement quality:", "stage 10 latency envelope:", "stage 10 generator envelope:", "stage 10 scheduler lag:"} {
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
	metrics := report.MetricSet{Total: 10, Success: 10, P50MS: math.NaN(), P95MS: math.Inf(1)}
	run := report.Result{ArrivalsScheduled: 10, IterationsStarted: 10, IterationsCompleted: 10,
		Metrics: metrics, Journeys: metrics, Outcome: "PASS", Quality: &report.Quality{State: report.QualityLimited}, SchedulerLagP95MS: 1, SchedulerLagMaxMS: 1,
		Generator: &generatorhealth.Summary{Samples: 2, GoCPUValid: true, ProcessSamples: 2, ProcessCPUValid: true, PeakProcessRSSBytes: 1}}
	issues := checkStage(2, 10, run)
	if len(issues) != 1 || !strings.Contains(issues[0], "latency envelope") {
		t.Fatalf("non-finite latency must fail the fixture gate with a precise diagnostic: %v", issues)
	}
}
