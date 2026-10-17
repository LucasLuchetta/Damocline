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

func TestTransportChecksRequireTrustedHTTPSAndRejectUntrustedHTTPS(t *testing.T) {
	checks := []transportCheck{
		{Name: "http", Outcome: "PASS", Requests: 10, Success: 10, ServerReceived: 10},
		{Name: "https_trusted_fixture", Outcome: "PASS", Requests: 10, Success: 10, ServerReceived: 10},
		{Name: "https_untrusted_fixture", Outcome: "FAIL", Requests: 10, Failures: 10, Reasons: map[string]int{"transport": 10}},
	}
	if !transportChecksPass(checks) {
		t.Fatal("expected successful HTTP, trusted HTTPS, and rejected untrusted HTTPS checks")
	}
	checks[1].ServerReceived = 9
	if transportChecksPass(checks) {
		t.Fatal("accepted a TLS stage whose client/server request ledgers differ")
	}
}
