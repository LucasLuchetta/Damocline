package generatorhealth

import (
	"strings"
	"testing"
	"time"
)

func TestAccumulatorPeakAndGoCPUOccupancy(t *testing.T) {
	start := time.Unix(0, 0)
	var a accumulator
	a.add(reading{at: start, heap: 100, goroutines: 3, gcCycles: 4, maxProcs: 2, cpuAvailable: true, cpuTotal: 10, cpuIdle: 7})
	a.add(reading{at: start.Add(time.Second), heap: 400, goroutines: 12, gcCycles: 5, maxProcs: 2, cpuAvailable: true, cpuTotal: 12, cpuIdle: 8})
	a.add(reading{at: start.Add(2*time.Second), heap: 200, goroutines: 7, gcCycles: 6, maxProcs: 2, cpuAvailable: true, cpuTotal: 14, cpuIdle: 9})
	s := a.finish()
	if s.Samples != 3 || s.PeakHeapAllocBytes != 400 || s.PeakGoroutines != 12 || s.GCCycles != 2 {
		t.Fatalf("bad runtime peaks and deltas: %+v", s)
	}
	// Busy seconds: (14-9) - (10-7) = 2; capacity = 2 s * 2 P = 4 s.
	if !s.GoCPUValid || s.GoCPUBusySeconds != 2 || s.GoCPUUtilizationRatio != 0.5 {
		t.Fatalf("wrong Go runtime CPU ratio: %+v", s)
	}
	if !strings.Contains(s.Note, "not process RSS") {
		t.Fatal("CPU and memory measurement limits must be explicit")
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
