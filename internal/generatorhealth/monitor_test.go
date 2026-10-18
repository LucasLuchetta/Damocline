package generatorhealth

import (
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var processCPUPressureSink atomic.Uint64

func TestAccumulatorPeakAndGoCPUOccupancy(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, heap: 100, goroutines: 3, gcCycles: 4, logicalCPUs: 2, maxProcs: 2, cpuAvailable: true, cpuTotal: 10, cpuIdle: 7})
	a.add(reading{at: start.Add(time.Second), heap: 400, goroutines: 12, gcCycles: 5, logicalCPUs: 2, maxProcs: 2, cpuAvailable: true, cpuTotal: 12, cpuIdle: 8})
	a.add(reading{at: start.Add(2 * time.Second), heap: 200, goroutines: 7, gcCycles: 6, logicalCPUs: 2, maxProcs: 2, cpuAvailable: true, cpuTotal: 14, cpuIdle: 9})
	s := a.finish()
	if s.Samples != 3 || s.PeakHeapAllocBytes != 400 || s.PeakGoroutines != 12 || s.GCCycles != 2 {
		t.Fatalf("bad runtime peaks and deltas: %+v", s)
	}
	// Busy seconds: (14-9) - (10-7) = 2; capacity = 2 s * 2 P = 4 s.
	if !s.GoCPUValid || s.GoCPUBusySeconds != 2 || s.GoCPUUtilizationRatio != 0.5 {
		t.Fatalf("wrong Go runtime CPU ratio: %+v", s)
	}
	if !strings.Contains(s.Note, "host memory") || !strings.Contains(s.Note, "target application") {
		t.Fatal("generator and process measurement limits must be explicit")
	}
}

func TestAccumulatorRecordsSampleCostAndMaximumGap(t *testing.T) {
	start := time.Unix(0, 0)
	a := accumulator{}
	a.summary.SamplingIntervalMS = 250
	for _, sample := range []struct {
		at   time.Duration
		cost time.Duration
	}{{0, time.Millisecond}, {250 * time.Millisecond, 2 * time.Millisecond}, {800 * time.Millisecond, 3 * time.Millisecond}} {
		a.add(reading{at: start.Add(sample.at), collectionTime: sample.cost, logicalCPUs: 1, maxProcs: 1})
	}
	s := a.finish()
	if s.Samples != 3 || s.SamplingIntervalMS != 250 || s.SampleCollectionTotalMS != 6 || s.SampleCollectionMaxMS != 3 || s.MaxSampleGapMS != 550 {
		t.Fatalf("sampling cost and gap were not retained: %+v", s)
	}
}

func TestAccumulatorProcessHealthPeaks(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, logicalCPUs: 2, maxProcs: 2, processCPUSeconds: 4, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(time.Second), logicalCPUs: 2, maxProcs: 2, processCPUSeconds: 5, processRSSBytes: 200, processAvailable: true})
	a.add(reading{at: start.Add(2 * time.Second), logicalCPUs: 2, maxProcs: 2, processCPUSeconds: 5.25, processRSSBytes: 150, processAvailable: true})
	s := a.finish()
	if s.ProcessSamples != 3 || s.ProcessCPUSamples != 2 || !s.ProcessCPUValid ||
		s.PeakProcessRSSBytes != 200 || s.PeakProcessCPUUtilizationRatio != 0.5 || s.MeanProcessCPUUtilizationRatio != 0.3125 {
		t.Fatalf("wrong process metrics summary: %+v", s)
	}
}

func TestAccumulatorNormalizesCPUByEffectiveQuota(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	for i := 0; i < 3; i++ {
		a.add(reading{
			at: start.Add(time.Duration(i) * time.Second), logicalCPUs: 12, maxProcs: 12,
			cpuCapacity: 4, cpuCapacitySource: "cgroup_v2_cpu_quota_hierarchy", cpuCapacityKnown: true,
			cpuAvailable: true, cpuTotal: float64(10 + i*4), cpuIdle: 8,
			processCPUSeconds: float64(i), processRSSBytes: 100, processAvailable: true,
		})
	}
	summary := a.finish()
	if !summary.CPUCapacityKnown || summary.CPUCapacityCores != 4 || summary.CPUCapacitySource != "cgroup_v2_cpu_quota_hierarchy" ||
		!summary.GoCPUValid || math.Abs(summary.GoCPUUtilizationRatio-1) > 1e-9 ||
		!summary.ProcessCPUValid || math.Abs(summary.MeanProcessCPUUtilizationRatio-0.25) > 1e-9 {
		t.Fatalf("CPU observations were not normalized to the cgroup quota: %+v", summary)
	}
}

func TestAccumulatorInvalidatesChangingCPUCapacity(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	for i, capacity := range []float64{4, 2} {
		a.add(reading{
			at: start.Add(time.Duration(i) * time.Second), logicalCPUs: 12, maxProcs: 12,
			cpuCapacity: capacity, cpuCapacitySource: "cgroup_v2_cpu_quota_hierarchy", cpuCapacityKnown: true,
			cpuAvailable: true, cpuTotal: float64(10 + i*2), cpuIdle: 8,
			processCPUSeconds: float64(i), processRSSBytes: 100, processAvailable: true,
		})
	}
	summary := a.finish()
	if summary.CPUCapacityKnown || summary.CPUCapacitySource != "changed_during_window" || summary.GoCPUValid || summary.ProcessCPUValid {
		t.Fatalf("changing CPU entitlement must not produce a stable capacity summary: %+v", summary)
	}
}

func TestAccumulatorSeparatesProcessCPUPeakFromWindowMean(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 0, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(250 * time.Millisecond), logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 0.3, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(5 * time.Second), logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 0.4, processRSSBytes: 100, processAvailable: true})
	s := a.finish()
	if math.Abs(s.PeakProcessCPUUtilizationRatio-1.2) > 1e-9 || math.Abs(s.MeanProcessCPUUtilizationRatio-0.08) > 1e-9 {
		t.Fatalf("short process CPU burst was not separated from sustained window use: %+v", s)
	}
}

func TestAccumulatorExcludesZeroAndImplausibleProcessReadings(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 1, processRSSBytes: 0, processAvailable: true})
	a.add(reading{at: start.Add(time.Second), logicalCPUs: 1, maxProcs: 1, processCPUSeconds: math.Inf(1), processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(2 * time.Second), logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 2, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(3 * time.Second), logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 3, processRSSBytes: 200, processAvailable: true})
	summary := a.finish()
	if summary.InvalidProcessSamples != 2 || summary.ProcessSamples != 2 || summary.ProcessCPUSamples != 1 ||
		!summary.ProcessCPUValid || summary.PeakProcessRSSBytes != 200 {
		t.Fatalf("invalid process readings were silently accepted or replaced: %+v", summary)
	}
}

func TestAccumulatorDoesNotCreateProcessCPUSampleFromStaleTimestamps(t *testing.T) {
	at := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: at, logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 1, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: at, logicalCPUs: 1, maxProcs: 1, processCPUSeconds: 1.5, processRSSBytes: 200, processAvailable: true})
	summary := a.finish()
	if summary.ProcessSamples != 2 || summary.ProcessCPUSamples != 0 || summary.ProcessCPUValid {
		t.Fatalf("stale timestamp yielded process CPU evidence: %+v", summary)
	}
}

func TestUnavailableProcessMetricsRemainUnknown(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, maxProcs: 1})
	a.add(reading{at: start.Add(time.Second), maxProcs: 1})
	summary := a.finish()
	if summary.ProcessSamples != 0 || summary.InvalidProcessSamples != 0 || summary.ProcessCPUValid || summary.PeakProcessRSSBytes != 0 {
		t.Fatalf("unavailable process measurements must remain unknown: %+v", summary)
	}
}

func TestCPUIsUnknownWhenGOMAXPROCSChanges(t *testing.T) {
	var a accumulator
	start := time.Unix(0, 0)
	a.add(reading{at: start, logicalCPUs: 2, maxProcs: 2, cpuAvailable: true, cpuTotal: 10, cpuIdle: 2})
	a.add(reading{at: start.Add(time.Second), logicalCPUs: 4, maxProcs: 4, cpuAvailable: true, cpuTotal: 11, cpuIdle: 3})
	if a.finish().GoCPUValid {
		t.Fatal("should not synthesize a CPU utilization ratio across a changing CPU entitlement")
	}
}

func TestCPUIsUnknownWhenLogicalCPUCountChanges(t *testing.T) {
	var a accumulator
	start := time.Unix(0, 0)
	a.add(reading{at: start, logicalCPUs: 8, maxProcs: 8, cpuAvailable: true, cpuTotal: 10, cpuIdle: 2})
	a.add(reading{at: start.Add(time.Second), logicalCPUs: 4, maxProcs: 4, cpuAvailable: true, cpuTotal: 11, cpuIdle: 3})
	if summary := a.finish(); summary.GoCPUValid || summary.CPUCapacityKnown {
		t.Fatalf("should not synthesize CPU evidence across changing logical CPU capacity: %+v", summary)
	}
}

func TestStopReturnsConsistentSnapshotAndIsIdempotent(t *testing.T) {
	m := Start(200 * time.Millisecond)
	first := m.Stop()
	second := m.Stop()
	if first.Samples < 2 || first.PeakGoroutines < 1 || first.PeakHeapAllocBytes == 0 || first != second {
		t.Fatalf("unexpected monitor snapshot: first=%+v second=%+v", first, second)
	}
}

func TestMonitorSamplesSupportedProcessMetrics(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OS process metrics are currently implemented for Windows and Linux")
	}
	m := Start(100 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	s := m.Stop()
	if s.ProcessSamples < 2 || s.ProcessCPUSamples < 1 || !s.ProcessCPUValid || s.PeakProcessRSSBytes == 0 {
		t.Fatalf("supported OS process metrics missing: %+v", s)
	}
	if s.SamplingIntervalMS != 100 || s.MaxSampleGapMS <= 0 || s.SampleCollectionMaxMS < 0 || s.SampleCollectionTotalMS < s.SampleCollectionMaxMS {
		t.Fatalf("sampling cadence or collection cost missing: %+v", s)
	}
}

func TestProcessCPUCounterDetectsControlledGeneratorWork(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OS process CPU metrics are currently implemented for Windows and Linux")
	}

	before, _, ok := readProcessHealth()
	if !ok {
		t.Fatal("could not read process health before controlled CPU work")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var work uint64
	for time.Now().Before(deadline) {
		work = work*1664525 + 1013904223
	}
	processCPUPressureSink.Store(work)
	after, _, ok := readProcessHealth()
	if !ok {
		t.Fatal("could not read process health after controlled CPU work")
	}
	if delta := after - before; delta < 0.01 {
		t.Fatalf("process CPU counter did not reflect controlled work: delta=%.6fs", delta)
	}
}
