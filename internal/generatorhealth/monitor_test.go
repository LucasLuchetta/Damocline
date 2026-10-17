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
	a.add(reading{at: start, heap: 100, goroutines: 3, gcCycles: 4, maxProcs: 2, cpuAvailable: true, cpuTotal: 10, cpuIdle: 7})
	a.add(reading{at: start.Add(time.Second), heap: 400, goroutines: 12, gcCycles: 5, maxProcs: 2, cpuAvailable: true, cpuTotal: 12, cpuIdle: 8})
	a.add(reading{at: start.Add(2 * time.Second), heap: 200, goroutines: 7, gcCycles: 6, maxProcs: 2, cpuAvailable: true, cpuTotal: 14, cpuIdle: 9})
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

func TestAccumulatorProcessHealthPeaks(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, maxProcs: 2, processCPUSeconds: 4, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(time.Second), maxProcs: 2, processCPUSeconds: 5, processRSSBytes: 200, processAvailable: true})
	a.add(reading{at: start.Add(2 * time.Second), maxProcs: 2, processCPUSeconds: 5.25, processRSSBytes: 150, processAvailable: true})
	s := a.finish()
	if s.ProcessSamples != 3 || s.ProcessCPUSamples != 2 || !s.ProcessCPUValid ||
		s.PeakProcessRSSBytes != 200 || s.PeakProcessCPUUtilizationRatio != 0.5 {
		t.Fatalf("wrong process metrics summary: %+v", s)
	}
}

func TestAccumulatorExcludesZeroAndImplausibleProcessReadings(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, maxProcs: 1, processCPUSeconds: 1, processRSSBytes: 0, processAvailable: true})
	a.add(reading{at: start.Add(time.Second), maxProcs: 1, processCPUSeconds: math.Inf(1), processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(2 * time.Second), maxProcs: 1, processCPUSeconds: 2, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: start.Add(3 * time.Second), maxProcs: 1, processCPUSeconds: 3, processRSSBytes: 200, processAvailable: true})
	summary := a.finish()
	if summary.InvalidProcessSamples != 2 || summary.ProcessSamples != 2 || summary.ProcessCPUSamples != 1 ||
		!summary.ProcessCPUValid || summary.PeakProcessRSSBytes != 200 {
		t.Fatalf("invalid process readings were silently accepted or replaced: %+v", summary)
	}
}

func TestAccumulatorDoesNotCreateProcessCPUSampleFromStaleTimestamps(t *testing.T) {
	at := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: at, maxProcs: 1, processCPUSeconds: 1, processRSSBytes: 100, processAvailable: true})
	a.add(reading{at: at, maxProcs: 1, processCPUSeconds: 1.5, processRSSBytes: 200, processAvailable: true})
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
	a.add(reading{at: start, maxProcs: 2, cpuAvailable: true, cpuTotal: 10, cpuIdle: 2})
	a.add(reading{at: start.Add(time.Second), maxProcs: 4, cpuAvailable: true, cpuTotal: 11, cpuIdle: 3})
	if a.finish().GoCPUValid {
		t.Fatal("should not synthesize a CPU utilization ratio across a changing CPU entitlement")
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
