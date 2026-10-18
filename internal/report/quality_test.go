package report

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
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

func TestQualityKeepsAttachedCalibrationLimitedWhileTargetEvidenceIsUnknown(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 10
	local := calibration.CurrentRuntime()
	r.Generator.GOMAXPROCS = local.GOMAXPROCS
	r.Generator.ObservedSeconds = 10
	r.Generator.SamplingIntervalMS = 250
	r.Generator.SampleCollectionTotalMS = 1
	r.Generator.SampleCollectionMaxMS = 0.5
	r.Generator.MaxSampleGapMS = 250
	r.Generator.ProcessCPUSamples = r.Generator.ProcessSamples
	r.Generator.PeakProcessRSSBytes = 4096
	r.Generator.GoCPUCapacitySeconds = 10
	r.Generator.PeakHeapAllocBytes = 1024
	r.Generator.PeakGoroutines = 2
	rec, err := calibration.NewRecord(time.Now().Add(-time.Minute), time.Now(),
		calibration.Generator{OS: local.OS, Arch: local.Arch, LogicalCPUs: local.LogicalCPUs, GOMAXPROCS: local.GOMAXPROCS, GoVersion: local.GoVersion, RunnerVersion: local.RunnerVersion},
		calibration.Fixture{NetworkMode: "loopback_same_host_separate_process", WorkloadModel: "open_loop_journey_arrivals", Method: "GET", HealthyResponseBytes: 4096, HealthyDelay: "20ms", StageDuration: "5s", SoakDuration: "30s", StageUsers: 10, MaximumRPS: 100, Repeats: 1, HealthyRates: []int{2, 5, 10}},
		[]calibration.Stage{{ArrivalRate: 2, Scheduled: 10, Started: 10, Completed: 10, Outcome: "PASS"}, {ArrivalRate: 5, Scheduled: 25, Started: 25, Completed: 25, Outcome: "PASS"}, {ArrivalRate: 10, Scheduled: 50, Started: 50, Completed: 50, Outcome: "PASS"}}, "REFERENCE_SMOKE_PASSED")
	if err != nil {
		t.Fatal(err)
	}
	referenceWorkloadFingerprint, err := calibration.ReferenceWorkloadFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	r.Config.WorkloadFingerprint = referenceWorkloadFingerprint
	r.Calibration = &CalibrationEvidence{Record: rec, Coverage: calibration.Coverage{Status: "INCONCLUSIVE", Dimensions: []calibration.Dimension{
		{Name: "response_shape", Status: "UNVERIFIED", Reason: "unknown"}, {Name: "fixture_identity", Status: "UNVERIFIED", Reason: "unknown"},
	}}}
	q := AssessQuality(r)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "response shape and controlled fixture identity") {
		t.Fatalf("attached fixed-fixture calibration cannot upgrade unknown target evidence: %+v", q)
	}
	r.SchemaVersion = 12
	observedSize := AssessObservedResponseSize(r, int64(rec.Fixture.HealthyResponseBytes))
	observedLatency := AssessObservedResponseLatency(r)
	workloadShape := AssessDeclaredWorkloadShape(r, rec)
	r.Calibration.Coverage.Dimensions = append(r.Calibration.Coverage.Dimensions, observedSize, observedLatency, workloadShape)
	q = AssessQuality(r)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "complete per-step evidence is insufficient") {
		t.Fatalf("missing observed body sizes must remain limited: %+v", q)
	}
	observedIndex := -1
	for i := range r.Calibration.Coverage.Dimensions {
		if r.Calibration.Coverage.Dimensions[i].Name == "observed_response_size" {
			observedIndex = i
			break
		}
	}
	if observedIndex < 0 {
		t.Fatal("observed response-size dimension missing")
	}
	r.Calibration.Coverage.Dimensions[observedIndex].Status = "MATCHED"
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("persisted response-size MATCHED status must be recomputed: %+v", q)
	}
	body := ResponseBodyDiagnostics{CompleteObservations: 100, MinBytes: 4096, MaxBytes: 4096, TotalBytes: 409600}
	http := HTTPDiagnostics{StatusCodes: map[int]int{200: 100}, Status2xx: 100}
	step := StepResult{
		Metrics:   MetricSet{Total: 100, Success: 100, P50MS: 20, P95MS: 30, HTTP: http, ResponseBody: body},
		HTTPTrace: HTTPTrace{RequestAttemptCount: 100, WriteCount: 100, RateWaitCount: 100, FirstByteCount: 100, BodyCount: 100, CompleteCount: 100},
	}
	r.Metrics.HTTP = http
	r.Metrics.ResponseBody = body
	r.Steps = []StepResult{step}
	r.Calibration.Coverage.Dimensions[observedIndex] = AssessObservedResponseSize(r, int64(rec.Fixture.HealthyResponseBytes))
	for i := range r.Calibration.Coverage.Dimensions {
		if r.Calibration.Coverage.Dimensions[i].Name == "observed_response_latency" {
			r.Calibration.Coverage.Dimensions[i] = AssessObservedResponseLatency(r)
		}
	}
	q = AssessQuality(r)
	if q.State != QualityLimited || !strings.Contains(strings.Join(q.Findings, " "), "response timing distribution and endpoint identity remain unverified") {
		t.Fatalf("matching fixed body size cannot certify the broader response envelope: %+v", q)
	}
	for i := range r.Calibration.Coverage.Dimensions {
		if r.Calibration.Coverage.Dimensions[i].Name == "observed_response_latency" {
			r.Calibration.Coverage.Dimensions[i].Status = "OUTSIDE_REFERENCE"
		}
	}
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("persisted response-latency status must be recomputed: %+v", q)
	}
	for i := range r.Calibration.Coverage.Dimensions {
		if r.Calibration.Coverage.Dimensions[i].Name == "observed_response_latency" {
			r.Calibration.Coverage.Dimensions[i] = AssessObservedResponseLatency(r)
		}
	}
	r.Calibration.Coverage.Dimensions[observedIndex].Status = "OUTSIDE_REFERENCE"
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("persisted response-size mismatch must be recomputed: %+v", q)
	}
	r.Calibration.Coverage.Dimensions = []calibration.Dimension{{Name: "arrival_rate", Status: "MATCHED", Reason: "matched"}}
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("missing mandatory unknowns should invalidate calibration coverage: %+v", q)
	}
}

func TestSchemaThirteenRequiresReconciledNamedWindowLedgers(t *testing.T) {
	http := HTTPDiagnostics{StatusCodes: map[int]int{200: 1}, Status2xx: 1}
	body := ResponseBodyDiagnostics{CompleteObservations: 1, MinBytes: 1, MaxBytes: 1, TotalBytes: 1}
	metrics := MetricSet{Total: 1, Success: 1, P50MS: 10, P95MS: 10, P99MS: 10, HTTP: http, ResponseBody: body}
	r := validSyntheticQualityRun()
	r.SchemaVersion = 13
	r.Config.Mode = "closed"
	r.Generator = nil
	r.Metrics = metrics
	trace := HTTPTrace{RateWaitCount: 1, RequestAttemptCount: 1, CompleteCount: 1, FirstByteCount: 1, BodyCount: 1}
	r.Steps = []StepResult{{Name: "read", Metrics: metrics, HTTPTrace: trace}}
	r.Windows = []WindowResult{{StartSeconds: 0, EndSeconds: 1, Metrics: metrics,
		Steps: []WindowMetric{{Name: "read", Metrics: metrics}}}}
	if q := AssessQuality(r); q.State == QualityInvalid {
		t.Fatalf("consistent named window ledgers were rejected: %+v", q)
	}
	r.Windows[0].Steps = nil
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("missing per-step window ledger was trusted: %+v", q)
	}
	r.Windows[0].Steps = []WindowMetric{{Name: "read", Metrics: metrics}}
	r.Windows[0].Steps[0].Metrics.P95MS = math.NaN()
	if q := AssessQuality(r); q.State != QualityInvalid {
		t.Fatalf("non-finite window latency was trusted: %+v", q)
	}
}

func TestSchemaThirteenAllowsOnlyFinalWindowToEndAtObservationHorizon(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 13
	r.ObservationSeconds = 1.3
	r.ElapsedSeconds = 1.3
	r.Config.WindowDuration = "1s"
	r.Windows = []WindowResult{{StartSeconds: 0, EndSeconds: 1}, {StartSeconds: 1, EndSeconds: 1.3}}
	if !validObservationWindowBounds(r) {
		t.Fatal("final observation window should end at the exact observation horizon")
	}
	r.Windows[1].EndSeconds = 2
	if validObservationWindowBounds(r) {
		t.Fatal("final observation window extending beyond the observation horizon was accepted")
	}
}

func TestSchemaThirteenRejectsProfileWindowTotalsAboveRunLedger(t *testing.T) {
	r := Result{
		Config:   RunConfig{Profiles: []ProfileConfig{{Name: "browse"}}},
		Profiles: []ProfileResult{{Name: "browse", Journeys: MetricSet{Total: 2}}},
		Windows:  []WindowResult{{Profiles: []WindowMetric{{Name: "browse", Metrics: MetricSet{Total: 3}}}}},
	}
	if validWindowProfileTotals(r) {
		t.Fatal("profile journey windows cannot exceed the saved run-level profile ledger")
	}
	r.Windows[0].Profiles[0].Metrics.Total = 2
	if !validWindowProfileTotals(r) {
		t.Fatal("reconciled profile journey windows were rejected")
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

func TestQualityReconcilesResponseBodyShapeWithoutCertifyingIt(t *testing.T) {
	r := validSyntheticQualityRun()
	r.SchemaVersion = 12
	r.Generator = &generatorhealth.Summary{
		Samples: 10, ObservedSeconds: 10, SamplingIntervalMS: 250,
		SampleCollectionTotalMS: 1, SampleCollectionMaxMS: 0.5, MaxSampleGapMS: 250,
		GOMAXPROCS: 1, PeakHeapAllocBytes: 1024, PeakGoroutines: 2,
		GoCPUCapacitySeconds: 10, GoCPUValid: true,
		ProcessSamples: 10, ProcessCPUSamples: 10, PeakProcessRSSBytes: 4096, ProcessCPUValid: true,
	}
	r.SchedulerLagSamples = r.ArrivalsScheduled
	r.SchedulerLagMethod = "synthetic deterministic fixture"
	r.Metrics.HTTP = HTTPDiagnostics{
		StatusCodes: map[int]int{200: 3},
		Status2xx:   3,
	}
	r.Metrics.ResponseBody = ResponseBodyDiagnostics{
		CompleteObservations: 3,
		MinBytes:             1,
		MaxBytes:             4,
		TotalBytes:           6,
	}
	if q := AssessQuality(r); q.State != QualityLimited {
		t.Fatalf("well-formed size observations alone cannot certify the envelope: %+v", q)
	}
	for name, mutate := range map[string]func(*Result){
		"response count mismatch": func(result *Result) {
			result.Metrics.ResponseBody.CompleteObservations = 2
		},
		"impossible byte total": func(result *Result) {
			result.Metrics.ResponseBody.TotalBytes = 2
		},
		"oversized count mismatch": func(result *Result) {
			result.Metrics.ResponseBody.OversizedObservations = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := r
			mutate(&invalid)
			if q := AssessQuality(invalid); q.State != QualityInvalid {
				t.Fatalf("inconsistent response-size evidence must be INVALID: %+v", q)
			}
		})
	}
}

func TestObservedResponseSizeCoverageUsesEveryStepAndConservativeStates(t *testing.T) {
	makeStep := func(complete, incomplete int64, minimum, maximum int64) StepResult {
		return StepResult{Metrics: MetricSet{
			HTTP: HTTPDiagnostics{StatusCodes: map[int]int{200: int(complete + incomplete)}, Status2xx: int(complete + incomplete)},
			ResponseBody: ResponseBodyDiagnostics{
				CompleteObservations:   complete,
				IncompleteObservations: incomplete,
				MinBytes:               minimum,
				MaxBytes:               maximum,
				TotalBytes:             complete * minimum,
			},
		}}
	}
	r := Result{SchemaVersion: 12, Steps: []StepResult{makeStep(3, 0, 4096, 4096)}}
	if got := AssessObservedResponseSize(r, 4096); got.Status != "MATCHED" {
		t.Fatalf("complete exact-size observations should match only the fixed size dimension: %+v", got)
	}
	r.Steps = append(r.Steps, makeStep(0, 0, 0, 0))
	if got := AssessObservedResponseSize(r, 4096); got.Status != "UNVERIFIED" {
		t.Fatalf("every named step needs complete evidence: %+v", got)
	}
	r.Steps[1] = makeStep(2, 0, 1024, 4096)
	if got := AssessObservedResponseSize(r, 4096); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("a variable size outside the exact fixture response should not match: %+v", got)
	}
	r.Steps = []StepResult{makeStep(1, 1, 4096, 4096)}
	if got := AssessObservedResponseSize(r, 4096); got.Status != "UNVERIFIED" {
		t.Fatalf("interrupted response reads cannot establish a complete size envelope: %+v", got)
	}
}

func TestLegacyMissingResponseBodyIsUnknownRatherThanZeroBytes(t *testing.T) {
	r := Result{SchemaVersion: 11, Steps: []StepResult{{Name: "read", Metrics: MetricSet{Total: 1, Success: 1}}}}
	if got := AssessObservedResponseSize(r, 0); got.Status != "UNVERIFIED" {
		t.Fatalf("a legacy report without body measurements must not be treated as a measured empty body: %+v", got)
	}
	r.SchemaVersion = 12
	r.Steps[0].Metrics.HTTP = HTTPDiagnostics{StatusCodes: map[int]int{200: 1}, Status2xx: 1}
	r.Steps[0].Metrics.ResponseBody = ResponseBodyDiagnostics{CompleteObservations: 1}
	if got := AssessObservedResponseSize(r, 0); got.Status != "MATCHED" {
		t.Fatalf("a recorded complete zero-byte body must remain distinct from a missing legacy value: %+v", got)
	}
}

func TestAssessObservedResponseLatencyRequiresSamplesAndChecksEveryStep(t *testing.T) {
	r := Result{SchemaVersion: 12, Steps: []StepResult{{Metrics: MetricSet{Total: 99, Success: 99, P50MS: 20, P95MS: 30}}}}
	if got := AssessObservedResponseLatency(r); got.Status != "UNVERIFIED" {
		t.Fatalf("sub-floor latency sample must be unverified: %+v", got)
	}
	r.Steps[0].Metrics = MetricSet{Total: 100, Success: 100, P50MS: 20, P95MS: 30}
	if got := AssessObservedResponseLatency(r); got.Status != "MATCHED" {
		t.Fatalf("well-sampled descriptive percentiles inside the fixture guardrail should match: %+v", got)
	}
	r.Steps = append(r.Steps, StepResult{Metrics: MetricSet{Total: 100, Success: 100, P50MS: 20, P95MS: 151}})
	if got := AssessObservedResponseLatency(r); got.Status != "OUTSIDE_REFERENCE" {
		t.Fatalf("an out-of-guardrail step must not be hidden by another step: %+v", got)
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
