package report

import (
	"encoding/json"
	"math"
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
		Generator: &generatorhealth.Summary{Samples: 10, GoCPUValid: true, ProcessSamples: 10, ProcessCPUValid: true},
		Outcome:   "PASS",
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

func TestQualityLimitsStaleGeneratorSamplingAndRejectsMalformedSampling(t *testing.T) {
	base := validSyntheticQualityRun()
	base.SchemaVersion = 10
	base.Generator.ObservedSeconds = 10
	base.Generator.GOMAXPROCS = 1
	base.Generator.SamplingIntervalMS = 250
	base.Generator.SampleCollectionTotalMS = 2
	base.Generator.SampleCollectionMaxMS = 0.5
	base.Generator.MaxSampleGapMS = 250
	base.Generator.ProcessSamples = base.Generator.Samples
	base.Generator.ProcessCPUSamples = base.Generator.ProcessSamples - 1
	base.Generator.PeakProcessRSSBytes = 4096
	base.Generator.GoCPUCapacitySeconds = 10
	base.Generator.PeakHeapAllocBytes = 1024
	base.Generator.PeakGoroutines = 2
	if q := AssessQuality(base); q.State != QualityLimited {
		t.Fatalf("valid bounded sampler should preserve the existing limited envelope: %+v", q)
	}
	stale := base
	staleGenerator := *base.Generator
	staleGenerator.MaxSampleGapMS = 501
	stale.Generator = &staleGenerator
	q := AssessQuality(stale)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "sampling has a gap") {
		t.Fatalf("stale sampling gap was not exposed: %+v", q)
	}
	zero := base
	zeroGenerator := *base.Generator
	zeroGenerator.PeakProcessRSSBytes = 0
	zero.Generator = &zeroGenerator
	if q := AssessQuality(zero); q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "no resident-memory value") {
		t.Fatalf("zero process RSS was not exposed as limited evidence: %+v", q)
	}
	for name, mutate := range map[string]func(*generatorhealth.Summary){
		"non-finite interval":               func(g *generatorhealth.Summary) { g.SamplingIntervalMS = math.NaN() },
		"sample counts exceed observations": func(g *generatorhealth.Summary) { g.ProcessSamples = g.Samples + 1 },
		"collection peak exceeds total":     func(g *generatorhealth.Summary) { g.SampleCollectionMaxMS = g.SampleCollectionTotalMS + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			copy := *base.Generator
			mutate(&copy)
			r.Generator = &copy
			if q := AssessQuality(r); q.State != QualityInvalid {
				t.Fatalf("malformed sampler evidence was not INVALID: %+v", q)
			}
		})
	}
}

func TestQualityLimitsExcludedInvalidProcessSamples(t *testing.T) {
	r := validSyntheticQualityRun()
	r.Generator.InvalidProcessSamples = 1
	q := AssessQuality(r)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "process samples were zero or implausible and were excluded") {
		t.Fatalf("excluded invalid generator samples must limit evidence: %+v", q)
	}
	r.Generator.InvalidProcessSamples = -1
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("negative invalid-sample count must invalidate evidence: %+v", q)
	}
}

func TestQualityLimitsHighProcessCPUWithoutCallingItMalformed(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 10
	g := *r.Generator
	g.GOMAXPROCS = 1
	g.ObservedSeconds = 10
	g.SamplingIntervalMS = 250
	g.SampleCollectionTotalMS = 2
	g.SampleCollectionMaxMS = 0.5
	g.MaxSampleGapMS = 250
	g.ProcessSamples = g.Samples
	g.ProcessCPUSamples = g.ProcessSamples - 1
	g.ProcessCPUValid = true
	g.PeakProcessCPUUtilizationRatio = 1.3
	g.MeanProcessCPUUtilizationRatio = 1.3
	g.PeakProcessRSSBytes = 4096
	g.GoCPUCapacitySeconds = 10
	g.PeakHeapAllocBytes = 1024
	g.PeakGoroutines = 2
	r.Generator = &g
	q := AssessQuality(r)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "Mean generator process CPU reached at least 90%") {
		t.Fatalf("high process CPU should limit generator-quality evidence without being called invalid: %+v", q)
	}
}

func TestQualityUsesWindowMeanInsteadOfIsolatedProcessCPUPeak(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 10
	g := *r.Generator
	g.GOMAXPROCS = 1
	g.ObservedSeconds = 10
	g.SamplingIntervalMS = 250
	g.SampleCollectionTotalMS = 2
	g.SampleCollectionMaxMS = 0.5
	g.MaxSampleGapMS = 250
	g.ProcessSamples = g.Samples
	g.ProcessCPUSamples = g.ProcessSamples - 1
	g.ProcessCPUValid = true
	g.PeakProcessRSSBytes = 4096
	g.PeakProcessCPUUtilizationRatio = 1.3
	g.MeanProcessCPUUtilizationRatio = 0.08
	g.GoCPUCapacitySeconds = 10
	g.PeakHeapAllocBytes = 1024
	g.PeakGoroutines = 2
	r.Generator = &g
	q := AssessQuality(r)
	if strings.Contains(strings.Join(q.Findings, " "), "sustained generator headroom") {
		t.Fatalf("an isolated interval peak should not be called sustained process CPU saturation: %+v", q)
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

func TestQualityRejectsInconsistentHTTPTraceCountsAndTotals(t *testing.T) {
	base := Result{SchemaVersion: 7, Metrics: MetricSet{Total: 1, Success: 1}, Journeys: MetricSet{Total: 1, Success: 1}, Outcome: "PASS", ElapsedSeconds: 1, ObservationSeconds: 1, Config: RunConfig{Users: 1, MaxRPS: 10}, IterationsStarted: 1, IterationsCompleted: 1}
	base.Steps = []StepResult{{Metrics: MetricSet{Total: 1, Success: 1}, HTTPTrace: HTTPTrace{RequestAttemptCount: 1, RateWaitCount: 1, CompleteCount: 1, WriteCount: 1, FirstByteCount: 1, BodyCount: 1}}}
	for name, mutate := range map[string]func(*HTTPTrace){
		"negative phase count":          func(t *HTTPTrace) { t.DNSCount = -1 },
		"non-finite duration":           func(t *HTTPTrace) { t.CompleteMS = math.NaN() },
		"missing rate wait":             func(t *HTTPTrace) { t.RateWaitCount = 0 },
		"body count exceeds first byte": func(t *HTTPTrace) { t.BodyCount = 2 },
	} {
		r := base
		r.Steps = append([]StepResult(nil), base.Steps...)
		mutate(&r.Steps[0].HTTPTrace)
		if q := AssessQuality(r); q.State != QualityInvalid {
			t.Errorf("%s: expected INVALID, got %+v", name, q)
		}
	}
}

func TestQualityTreatsPreTraceSchemaAsUnknown(t *testing.T) {
	r := Result{SchemaVersion: 6, Metrics: MetricSet{Total: 1, Success: 1}, Journeys: MetricSet{Total: 1, Success: 1}, Outcome: "PASS", ElapsedSeconds: 1, ObservationSeconds: 1, Config: RunConfig{Users: 1, MaxRPS: 10}, IterationsStarted: 1, IterationsCompleted: 1, Steps: []StepResult{{Metrics: MetricSet{Total: 1, Success: 1}}}}
	if q := AssessQuality(r); q.State == QualityInvalid {
		t.Fatalf("historical report without HTTP phase fields must remain unknown, not invalid: %+v", q)
	}
}

func TestQualityRejectsInvalidObservedThinkTime(t *testing.T) {
	r := Result{SchemaVersion: 7, ElapsedSeconds: 1, ObservationSeconds: 1, Config: RunConfig{Users: 1, MaxRPS: 1}, ObservedThinkTimeMS: math.Inf(1), ThinkTimeObservations: 1}
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("non-finite think time must invalidate v7 evidence: %+v", q)
	}
}

func TestQualityRejectsInconsistentHTTPDiagnostics(t *testing.T) {
	base := Result{SchemaVersion: 8, ElapsedSeconds: 1, ObservationSeconds: 1, Config: RunConfig{Users: 1, MaxRPS: 10}, Metrics: MetricSet{Total: 1, Success: 1}, Journeys: MetricSet{Total: 1, Success: 1}}
	for name, diagnostics := range map[string]HTTPDiagnostics{
		"status family mismatch":  {StatusCodes: map[int]int{302: 1}, Status2xx: 1},
		"invalid status code":     {StatusCodes: map[int]int{99: 1}},
		"negative classification": {NetworkTimeouts: -1},
	} {
		r := base
		r.Metrics.HTTP = diagnostics
		if q := AssessQuality(r); q.State != QualityInvalid {
			t.Errorf("%s: expected INVALID, got %+v", name, q)
		}
	}
}
