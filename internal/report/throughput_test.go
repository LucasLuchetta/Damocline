package report

import "testing"

func TestJourneyThroughputUsesObservationAndSeparatesFailures(t *testing.T) {
	r := Result{
		ElapsedSeconds: 20, ObservationSeconds: 10,
		Journeys: MetricSet{Total: 70, Success: 60, Failures: 10, Cancelled: 5},
	}
	if got := SuccessfulJourneysPerSecond(r); got != 6 {
		t.Fatalf("successful journey rate = %v, want 6/s", got)
	}
	if got := CompletedJourneysPerSecond(r); got != 7 {
		t.Fatalf("completed journey rate = %v, want 7/s", got)
	}
	r.ObservationSeconds = 0
	if got := SuccessfulJourneysPerSecond(r); got != 0 {
		t.Fatalf("missing observation duration must stay unknown, got %v", got)
	}
}
