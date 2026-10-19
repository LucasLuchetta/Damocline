package report

import (
	"math"
	"testing"
)

func TestStatisticalMethodMetadataIsVersionedAndFailClosed(t *testing.T) {
	current := Result{
		SchemaVersion: CurrentSchemaVersion, StatisticalMethods: CurrentStatisticalMethods(),
		Metrics: MetricSet{P95MS: 12, P95IID95LowerMS: 10, P95IID95UpperMS: 14, P95IID95Exact: true},
	}
	if !HasValidStatisticalMethods(current) {
		t.Fatal("current exact percentile method metadata was rejected")
	}
	current.StatisticalMethods.PercentileEstimator = "LINEAR_INTERPOLATION"
	if HasValidStatisticalMethods(current) {
		t.Fatal("altered current percentile method metadata was accepted")
	}

	legacy := Result{SchemaVersion: 29, Metrics: MetricSet{P95IID95Approximation: true, P95IID95LowerMS: 10, P95IID95UpperMS: 14}}
	if !HasValidStatisticalMethods(legacy) {
		t.Fatal("historical approximate interval lost its schema-specific interpretation")
	}
	legacy.Metrics.P95IID95Exact = true
	if HasValidStatisticalMethods(legacy) {
		t.Fatal("pre-v30 exact interval marker was accepted")
	}
}

func TestQualityRejectsOverflowAndNonFiniteDurations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Result)
	}{
		{"request overflow", func(r *Result) {
			r.Metrics.Total = math.MaxInt
			r.Metrics.Success = math.MaxInt
			r.Metrics.Failures = 1
		}},
		{"journey overflow", func(r *Result) {
			r.Journeys.Total = math.MaxInt
			r.Journeys.Success = math.MaxInt
			r.Journeys.Failures = 1
		}},
		{"arrival overflow", func(r *Result) {
			r.ArrivalsScheduled = math.MaxInt64
			r.IterationsStarted = math.MaxInt64
			r.ArrivalsDropped = 1
		}},
		{"dropped overflow", func(r *Result) {
			r.ArrivalsScheduled = math.MaxInt64
			r.IterationsStarted = 0
			r.ArrivalsDropped = math.MaxInt64
			r.ArrivalsDroppedLate = math.MaxInt64
			r.ArrivalsDroppedBusy = 1
		}},
		{"step overflow", func(r *Result) {
			r.Steps = []StepResult{{Metrics: MetricSet{Total: math.MaxInt, Success: math.MaxInt, Failures: 1}}}
		}},
		{"step negative", func(r *Result) { r.Steps = []StepResult{{Metrics: MetricSet{Total: 1, Success: -1, Failures: 2}}} }},
		{"elapsed NaN", func(r *Result) { r.ElapsedSeconds = math.NaN() }},
		{"elapsed infinite", func(r *Result) { r.ElapsedSeconds = math.Inf(1) }},
		{"observation NaN", func(r *Result) { r.ObservationSeconds = math.NaN() }},
		{"error rate infinite", func(r *Result) { r.ErrorsRate = math.Inf(1) }},
		{"scheduler lag negative", func(r *Result) { r.SchedulerLagP95MS = -1 }},
		{"scheduler lag inconsistent", func(r *Result) { r.SchedulerLagP95MS = 5; r.SchedulerLagMaxMS = 1 }},
		{"scheduler lag percentile order", func(r *Result) { r.SchedulerLagP50MS = 3; r.SchedulerLagP95MS = 2; r.SchedulerLagMaxMS = 4 }},
		{"scheduler lag sample coverage", func(r *Result) {
			r.SchedulerLagSamples = 99
			r.SchedulerLagP50MS = 1
			r.SchedulerLagP95MS = 2
			r.SchedulerLagMaxMS = 3
		}},
		{"SLO threshold NaN", func(r *Result) { r.Config.P95LimitMS = math.NaN() }},
		{"error budget invalid", func(r *Result) { r.Config.MaxErrorRate = 1.5 }},
		{"step cancelled negative", func(r *Result) { r.Steps = []StepResult{{Metrics: MetricSet{Total: 1, Success: 1, Cancelled: -1}}} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validSyntheticQualityRun()
			tc.mutate(&r)
			if q := AssessQuality(r); q.State != QualityInvalid {
				t.Fatalf("invalid counters accepted: %+v", q)
			}
		})
	}
}

func TestQualityCancellationAndSLOAreIndependent(t *testing.T) {
	r := validSyntheticQualityRun()
	r.Outcome = "FAIL"
	r.ArrivalsScheduled = 101
	r.ArrivalsDropped = 1
	r.ArrivalsDroppedBusy = 1
	r.SchedulerLagSamples = 101
	q := AssessQuality(r)
	if q.State != QualityLimited {
		t.Fatalf("legitimate dropped work should be limited, not corrupt: %+v", q)
	}
}

func TestQualityReconcilesNamedStepAndProfileLedgers(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Result)
		invalid bool
	}{
		{"exact step and profile ledgers", func(r *Result) {
			r.Steps = []StepResult{{Metrics: MetricSet{Total: 40, Success: 40}}, {Metrics: MetricSet{Total: 60, Success: 60}}}
			r.Profiles = []ProfileResult{{Journeys: MetricSet{Total: 40, Success: 40}}, {Journeys: MetricSet{Total: 60, Success: 60}}}
		}, false},
		{"missing step request", func(r *Result) {
			r.Steps = []StepResult{{Metrics: MetricSet{Total: 99, Success: 99}}}
		}, true},
		{"duplicated step request", func(r *Result) {
			r.Steps = []StepResult{{Metrics: MetricSet{Total: 100, Success: 100}}, {Metrics: MetricSet{Total: 1, Success: 1}}}
		}, true},
		{"missing profile journey", func(r *Result) {
			r.Profiles = []ProfileResult{{Journeys: MetricSet{Total: 99, Success: 99}}}
		}, true},
		{"duplicated profile journey", func(r *Result) {
			r.Profiles = []ProfileResult{{Journeys: MetricSet{Total: 100, Success: 100}}, {Journeys: MetricSet{Total: 1, Success: 1}}}
		}, true},
		{"step cancellation mismatch", func(r *Result) {
			r.Steps = []StepResult{{Metrics: MetricSet{Total: 100, Success: 100, Cancelled: 1}}}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validSyntheticQualityRun()
			tc.mutate(&r)
			q := AssessQuality(r)
			if (q.State == QualityInvalid) != tc.invalid {
				t.Fatalf("unexpected ledger state: %+v", q)
			}
		})
	}
}
