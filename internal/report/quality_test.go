package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

func validSyntheticQualityRun() Result {
	return Result{
		ElapsedSeconds: 10, ObservationSeconds: 10,
		Config:  RunConfig{Users: 10, MaxRPS: 100, Mode: "arrival", ArrivalRate: 10},
		Metrics: MetricSet{Total: 100, Success: 100}, Journeys: MetricSet{Total: 100, Success: 100},
		IterationsStarted: 100, IterationsCompleted: 100, ArrivalsScheduled: 100,
		SchedulerLagSamples: 100, SchedulerLagP50MS: 1, SchedulerLagP95MS: 2, SchedulerLagMaxMS: 3,
		SchedulerLagMethod: "monotonic due-slot dispatch lag",
		Generator:          &generatorhealth.Summary{Samples: 10, GoCPUValid: true, ProcessSamples: 10, ProcessCPUValid: true},
		Outcome:            "PASS",
	}
}

func TestQualityNoLocalCapacityCertificateEvenForPerfectDelivery(t *testing.T) {
	r := validSyntheticQualityRun()
	q := AssessQuality(r)
	if q.State != QualityLimited || len(q.Findings) == 0 {
		t.Fatalf("apparently healthy but uncalibrated observation must be LIMITED: %+v", q)
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "VALID_WITHIN_ENVELOPE") {
		t.Fatal("cannot claim validated capacity")
	}
}

func TestQualityRejectsCorruptedAccounting(t *testing.T) {
	for _, mutate := range []func(*Result){
		func(r *Result) { r.Metrics.Success++ },
		func(r *Result) { r.Journeys.Failures++ },
		func(r *Result) { r.IterationsCompleted = 101 },
		func(r *Result) { r.ArrivalsDropped = 5 },
		func(r *Result) { r.ArrivalsDroppedLate = 1 },
		func(r *Result) { r.ObservationSeconds = -1 },
		func(r *Result) { r.Config.Users = 0 },
		func(r *Result) {
			r.Steps = []StepResult{{Name: "secret-route", Metrics: MetricSet{Total: 4, Success: 5}}}
		},
	} {
		r := validSyntheticQualityRun()
		mutate(&r)
		q := AssessQuality(r)
		if q.State != QualityInvalid {
			t.Fatalf("corrupted evidence not invalid: %+v %#v", q, r)
		}
		serial, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(serial), "secret-route") {
			t.Fatal("data quality output leaked a step name")
		}
	}
}

func TestQualitySeparatesMissingGeneratorAndSLOFailure(t *testing.T) {
	r := validSyntheticQualityRun()
	r.Outcome = "FAIL"
	r.Generator = nil
	r.Journeys.Cancelled = 2
	q := AssessQuality(r)
	if q.State != QualityLimited {
		t.Fatalf("failure need not imply corrupt measurement: %+v", q)
	}
	got := strings.Join(q.Findings, " ")
	if !strings.Contains(got, "Generator") || !strings.Contains(got, "cancelled") {
		t.Fatalf("missing limited evidence warning: %+v", q)
	}
}
