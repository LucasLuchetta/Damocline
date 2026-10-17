package main

import (
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
