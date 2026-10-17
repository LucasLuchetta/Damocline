package report

import (
 "math"
 "testing"
)

func TestQualityRejectsOverflowAndNonFiniteDurations(t *testing.T) {
 tests:=[]struct{name string; mutate func(*Result)}{
  {"request overflow",func(r *Result){r.Metrics.Total=math.MaxInt;r.Metrics.Success=math.MaxInt;r.Metrics.Failures=1}},
  {"journey overflow",func(r *Result){r.Journeys.Total=math.MaxInt;r.Journeys.Success=math.MaxInt;r.Journeys.Failures=1}},
  {"arrival overflow",func(r *Result){r.ArrivalsScheduled=math.MaxInt64;r.IterationsStarted=math.MaxInt64;r.ArrivalsDropped=1}},
  {"dropped overflow",func(r *Result){r.ArrivalsScheduled=math.MaxInt64;r.IterationsStarted=0;r.ArrivalsDropped=math.MaxInt64;r.ArrivalsDroppedLate=math.MaxInt64;r.ArrivalsDroppedBusy=1}},
  {"step overflow",func(r *Result){r.Steps=[]StepResult{{Metrics:MetricSet{Total:math.MaxInt,Success:math.MaxInt,Failures:1}}}}},
  {"step negative",func(r *Result){r.Steps=[]StepResult{{Metrics:MetricSet{Total:1,Success:-1,Failures:2}}}}},
  {"elapsed NaN",func(r *Result){r.ElapsedSeconds=math.NaN()}},
  {"elapsed infinite",func(r *Result){r.ElapsedSeconds=math.Inf(1)}},
  {"observation NaN",func(r *Result){r.ObservationSeconds=math.NaN()}},
 }
 for _,tc:=range tests {
  t.Run(tc.name,func(t *testing.T){
   r:=validSyntheticQualityRun()
   tc.mutate(&r)
   if q:=AssessQuality(r);q.State!=QualityInvalid {t.Fatalf("invalid counters accepted: %+v",q)}
  })
 }
}

func TestQualityCancellationAndSLOAreIndependent(t *testing.T) {
 r:=validSyntheticQualityRun()
 r.Outcome="FAIL"
 r.ArrivalsScheduled=101
 r.ArrivalsDropped=1
 r.ArrivalsDroppedBusy=1
 q:=AssessQuality(r)
 if q.State!=QualityLimited {t.Fatalf("legitimate dropped work should be limited, not corrupt: %+v",q)}
}
