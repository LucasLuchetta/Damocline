// Package generatorhealth samples Go runtime and supported OS process resource
// use of the local load generator. These observations cannot identify
// server-side bottlenecks or host-level resource saturation.
package generatorhealth

import (
	"math"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"
)

// Summary describes the load generator during one measurement window.
// GoCPUUtilizationRatio is based on Go runtime accounting. The process CPU
// mean process CPU ratio is whole-window process CPU time divided by elapsed
// time and GOMAXPROCS capacity; the peak is the highest adjacent sample ratio.
type Summary struct {
	Samples                        int     `json:"samples"`
	ObservedSeconds                float64 `json:"observed_seconds"`
	SamplingIntervalMS             float64 `json:"sampling_interval_ms,omitempty"`
	SampleCollectionTotalMS        float64 `json:"sample_collection_total_ms,omitempty"`
	SampleCollectionMaxMS          float64 `json:"sample_collection_max_ms,omitempty"`
	MaxSampleGapMS                 float64 `json:"max_sample_gap_ms,omitempty"`
	GOMAXPROCS                     int     `json:"gomaxprocs"`
	CPUCapacityCores               float64 `json:"cpu_capacity_cores,omitempty"`
	CPUCapacitySource              string  `json:"cpu_capacity_source,omitempty"`
	CPUCapacityKnown               bool    `json:"cpu_capacity_known"`
	PeakHeapAllocBytes             uint64  `json:"peak_heap_alloc_bytes"`
	PeakGoroutines                 int     `json:"peak_goroutines"`
	GCCycles                       uint32  `json:"gc_cycles"`
	GoCPUBusySeconds               float64 `json:"go_cpu_busy_seconds"`
	GoCPUCapacitySeconds           float64 `json:"go_cpu_capacity_seconds"`
	GoCPUUtilizationRatio          float64 `json:"go_cpu_utilization_ratio"`
	GoCPUValid                     bool    `json:"go_cpu_valid"`
	ProcessSamples                 int     `json:"process_samples"`
	InvalidProcessSamples          int     `json:"invalid_process_samples,omitempty"`
	ProcessCPUSamples              int     `json:"process_cpu_samples"`
	PeakProcessRSSBytes            uint64  `json:"peak_process_rss_bytes,omitempty"`
	PeakProcessCPUUtilizationRatio float64 `json:"peak_process_cpu_utilization_ratio"`
	MeanProcessCPUUtilizationRatio float64 `json:"mean_process_cpu_utilization_ratio"`
	ProcessCPUValid                bool    `json:"process_cpu_valid"`
	Note                           string  `json:"note"`
}

const note = "Generator-side Go runtime and supported OS process samples only; RSS is not total host memory. CPU utilization is normalized to detected CPU capacity, including a finite Linux cgroup quota when available. An unavailable CPU limit is not treated as unlimited. The peak is the highest sampled interval and may reflect a short burst. Sampling may miss brief peaks and does not describe the target application."

type reading struct {
	at                time.Time
	collectionTime    time.Duration
	heap              uint64
	goroutines        int
	gcCycles          uint32
	maxProcs          int
	cpuCapacity       float64
	cpuCapacitySource string
	cpuCapacityKnown  bool
	cpuTotal          float64
	cpuIdle           float64
	cpuAvailable      bool
	processCPUSeconds float64
	processRSSBytes   uint64
	processAvailable  bool
}

func sampleRuntime() reading {
	started := time.Now()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	cpu := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"},
	}
	metrics.Read(cpu)
	valid := cpu[0].Value.Kind() == metrics.KindFloat64 && cpu[1].Value.Kind() == metrics.KindFloat64
	s := reading{
		heap:         mem.HeapAlloc,
		goroutines:   runtime.NumGoroutine(),
		gcCycles:     mem.NumGC,
		maxProcs:     runtime.GOMAXPROCS(0),
		cpuAvailable: valid,
	}
	capacity := capacityAtSample()
	s.cpuCapacity = capacity.cores
	s.cpuCapacitySource = capacity.source
	s.cpuCapacityKnown = capacity.known
	if valid {
		s.cpuTotal = cpu[0].Value.Float64()
		s.cpuIdle = cpu[1].Value.Float64()
	}
	processCPU, processRSS, processOK := readProcessHealth()
	s.processCPUSeconds = processCPU
	s.processRSSBytes = processRSS
	s.processAvailable = processOK
	s.at = time.Now()
	s.collectionTime = s.at.Sub(started)
	return s
}

type accumulator struct {
	base, latest       reading
	summary            Summary
	previousProcess    reading
	processBase        reading
	hasProcessBase     bool
	hasPreviousProcess bool
	capacityChanged    bool
}

func (a *accumulator) add(r reading) {
	if a.summary.Samples > 0 {
		if !sameCPUCapacity(a.latest, r) {
			a.capacityChanged = true
		}
		gap := r.at.Sub(a.latest.at)
		gapMS := float64(gap.Microseconds()) / 1000
		if gapMS > a.summary.MaxSampleGapMS {
			a.summary.MaxSampleGapMS = gapMS
		}
	}
	if a.summary.Samples == 0 {
		a.base = r
		a.summary.GOMAXPROCS = r.maxProcs
		a.summary.CPUCapacityCores = r.cpuCapacity
		a.summary.CPUCapacitySource = r.cpuCapacitySource
		a.summary.CPUCapacityKnown = r.cpuCapacityKnown
	}
	a.latest = r
	a.summary.Samples++
	if r.processAvailable && (math.IsNaN(r.processCPUSeconds) || math.IsInf(r.processCPUSeconds, 0) || r.processCPUSeconds < 0 || r.processRSSBytes == 0) {
		a.summary.InvalidProcessSamples++
		r.processAvailable = false
	}
	collectionMS := float64(r.collectionTime.Microseconds()) / 1000
	a.summary.SampleCollectionTotalMS += collectionMS
	if collectionMS > a.summary.SampleCollectionMaxMS {
		a.summary.SampleCollectionMaxMS = collectionMS
	}
	if r.heap > a.summary.PeakHeapAllocBytes {
		a.summary.PeakHeapAllocBytes = r.heap
	}
	if r.goroutines > a.summary.PeakGoroutines {
		a.summary.PeakGoroutines = r.goroutines
	}
	if r.processAvailable {
		a.summary.ProcessSamples++
		if r.processRSSBytes > a.summary.PeakProcessRSSBytes {
			a.summary.PeakProcessRSSBytes = r.processRSSBytes
		}
		if !a.hasProcessBase {
			a.processBase = r
			a.hasProcessBase = true
		}
		if a.hasPreviousProcess && r.at.After(a.previousProcess.at) && sameCPUCapacity(r, a.previousProcess) && r.processCPUSeconds >= a.previousProcess.processCPUSeconds {
			deltaSeconds := r.at.Sub(a.previousProcess.at).Seconds()
			cpuCapacity := deltaSeconds * capacityCores(r)
			if cpuCapacity > 0 {
				ratio := (r.processCPUSeconds - a.previousProcess.processCPUSeconds) / cpuCapacity
				if ratio >= 0 {
					a.summary.ProcessCPUSamples++
					a.summary.ProcessCPUValid = true
					if ratio > a.summary.PeakProcessCPUUtilizationRatio {
						a.summary.PeakProcessCPUUtilizationRatio = ratio
					}
				}
			}
		}
		a.previousProcess = r
		a.hasPreviousProcess = true
	} else {
		a.hasPreviousProcess = false
	}
}

func (a *accumulator) finish() Summary {
	s := a.summary
	s.Note = note
	if a.capacityChanged {
		s.CPUCapacityKnown = false
		s.CPUCapacitySource = "changed_during_window"
	}
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
	if a.hasProcessBase && a.hasPreviousProcess && a.previousProcess.at.After(a.processBase.at) &&
		sameCPUCapacity(a.processBase, a.previousProcess) &&
		a.previousProcess.processCPUSeconds >= a.processBase.processCPUSeconds {
		capacity := a.previousProcess.at.Sub(a.processBase.at).Seconds() * capacityCores(a.processBase)
		if capacity > 0 {
			s.MeanProcessCPUUtilizationRatio = (a.previousProcess.processCPUSeconds - a.processBase.processCPUSeconds) / capacity
		}
	}
	if secs > 0 && a.base.cpuAvailable && a.latest.cpuAvailable && sameCPUCapacity(a.base, a.latest) {
		busyBegin := a.base.cpuTotal - a.base.cpuIdle
		busyEnd := a.latest.cpuTotal - a.latest.cpuIdle
		busyDelta := busyEnd - busyBegin
		capacity := secs * capacityCores(a.base)
		if busyDelta >= 0 && busyDelta <= capacity*1.05 {
			s.GoCPUBusySeconds = busyDelta
			s.GoCPUCapacitySeconds = capacity
			s.GoCPUUtilizationRatio = busyDelta / capacity
			s.GoCPUValid = true
		}
	}
	return s
}

func capacityCores(r reading) float64 {
	if r.cpuCapacity > 0 {
		return r.cpuCapacity
	}
	return float64(r.maxProcs)
}

func sameCPUCapacity(left, right reading) bool {
	return left.maxProcs == right.maxProcs && left.maxProcs > 0 &&
		capacityCores(left) == capacityCores(right) && left.cpuCapacitySource == right.cpuCapacitySource &&
		left.cpuCapacityKnown == right.cpuCapacityKnown
}

// Monitor records a bounded set of in-process runtime samples; only aggregate
// statistics are exported. It never contacts the target or launches commands.
type Monitor struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	acc  accumulator
}

// Start begins self-monitoring. Sampling runs separately from request workers.
// Call Stop after test workers finish; Stop is safe to call more than once.
func Start(interval time.Duration) *Monitor {
	if interval < 100*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	m := &Monitor{stop: make(chan struct{}), done: make(chan struct{})}
	m.acc.summary.SamplingIntervalMS = float64(interval.Microseconds()) / 1000
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
	m.once.Do(func() { close(m.stop) })
	<-m.done
	return m.acc.finish()
}
