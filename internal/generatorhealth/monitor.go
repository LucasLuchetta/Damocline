// Package generatorhealth samples Go runtime resource use of the local load
// generator. These observations cannot identify server-side bottlenecks and
// are not equivalent to OS process RSS or total process CPU utilization.
package generatorhealth

import (
	"runtime"
	"runtime/metrics"
	"sync"
	"time"
)

// Summary describes the load-generator process during one measurement window.
// GoCPUUtilizationRatio is the Go scheduler's active P time divided by the
// sampled GOMAXPROCS capacity. It is not the OS process CPU percentage.
type Summary struct {
	Samples               int     `json:"samples"`
	ObservedSeconds       float64 `json:"observed_seconds"`
	GOMAXPROCS            int     `json:"gomaxprocs"`
	PeakHeapAllocBytes    uint64  `json:"peak_heap_alloc_bytes"`
	PeakGoroutines        int     `json:"peak_goroutines"`
	GCCycles              uint32  `json:"gc_cycles"`
	GoCPUBusySeconds      float64 `json:"go_cpu_busy_seconds,omitempty"`
	GoCPUCapacitySeconds  float64 `json:"go_cpu_capacity_seconds,omitempty"`
	GoCPUUtilizationRatio float64 `json:"go_cpu_utilization_ratio,omitempty"`
	GoCPUValid            bool    `json:"go_cpu_valid"`
	Note                  string  `json:"note"`
}

const note = "Generator-side Go runtime measurements only. Heap is not process RSS; Go CPU utilization is not the host or full process CPU utilization. Samples may miss brief peaks."

type reading struct {
	at          time.Time
	heap        uint64
	goroutines  int
	gcCycles    uint32
	maxProcs    int
	cpuTotal    float64
	cpuIdle     float64
	cpuAvailable bool
}

func sampleRuntime() reading {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	cpu := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"},
	}
	metrics.Read(cpu)
	valid := cpu[0].Value.Kind() == metrics.KindFloat64 && cpu[1].Value.Kind() == metrics.KindFloat64
	s := reading{
		at: time.Now(), heap: mem.HeapAlloc,
		goroutines: runtime.NumGoroutine(),
		gcCycles: mem.NumGC,
		maxProcs: runtime.GOMAXPROCS(0),
		cpuAvailable: valid,
	}
	if valid {
		s.cpuTotal = cpu[0].Value.Float64()
		s.cpuIdle = cpu[1].Value.Float64()
	}
	return s
}

type accumulator struct {
	base, latest reading
	summary Summary
}

func (a *accumulator) add(r reading) {
	if a.summary.Samples == 0 {
		a.base = r
		a.summary.GOMAXPROCS = r.maxProcs
	}
	a.latest = r
	a.summary.Samples++
	if r.heap > a.summary.PeakHeapAllocBytes {
		a.summary.PeakHeapAllocBytes = r.heap
	}
	if r.goroutines > a.summary.PeakGoroutines {
		a.summary.PeakGoroutines = r.goroutines
	}
}

func (a *accumulator) finish() Summary {
	s := a.summary
	s.Note = note
	if s.Samples == 0 {
		return s
	}
	secs := a.latest.at.Sub(a.base.at).Seconds()
	if secs < 0 {
		secs = 0
	}
	s.ObservedSeconds = secs
	if a.latest.gcCycles >= a.base.gcCycles {
		s.GCCycles = a.latest.gcCycles - a.base.gcCycles
	}
	if secs > 0 && a.base.cpuAvailable && a.latest.cpuAvailable &&
		a.base.maxProcs == a.latest.maxProcs && a.base.maxProcs > 0 {
		busyBegin := a.base.cpuTotal - a.base.cpuIdle
		busyEnd := a.latest.cpuTotal - a.latest.cpuIdle
		busyDelta := busyEnd - busyBegin
		capacity := secs * float64(a.base.maxProcs)
		if busyDelta >= 0 && busyDelta <= capacity*1.05 {
			s.GoCPUBusySeconds = busyDelta
			s.GoCPUCapacitySeconds = capacity
			s.GoCPUUtilizationRatio = busyDelta / capacity
			s.GoCPUValid = true
		}
	}
	return s
}

// Monitor records a bounded set of in-process runtime samples; only aggregate
// statistics are exported. It never contacts the target or launches commands.
type Monitor struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	acc accumulator
}

// Start begins self-monitoring. Sampling runs separately from request workers.
// Call Stop after test workers finish; Stop is safe to call more than once.
func Start(interval time.Duration) *Monitor {
	if interval < 100*time.Millisecond {
		interval = 250*time.Millisecond
	}
	m := &Monitor{stop: make(chan struct{}), done: make(chan struct{})}
	m.acc.add(sampleRuntime())
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				m.acc.add(sampleRuntime())
				return
			case <-ticker.C:
				m.acc.add(sampleRuntime())
			}
		}
	}()
	return m
}

// Stop completes sampling and returns a consistent value snapshot.
func (m *Monitor) Stop() Summary {
	m.once.Do(func(){ close(m.stop) })
	<-m.done
	return m.acc.finish()
}
