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
	Samples                        int               `json:"samples"`
	ObservedSeconds                float64           `json:"observed_seconds"`
	SamplingIntervalMS             float64           `json:"sampling_interval_ms,omitempty"`
	SampleCollectionTotalMS        float64           `json:"sample_collection_total_ms,omitempty"`
	SampleCollectionMaxMS          float64           `json:"sample_collection_max_ms,omitempty"`
	MaxSampleGapMS                 float64           `json:"max_sample_gap_ms,omitempty"`
	LogicalCPUs                    int               `json:"logical_cpus"`
	GOMAXPROCS                     int               `json:"gomaxprocs"`
	CPUCapacityCores               float64           `json:"cpu_capacity_cores,omitempty"`
	CPUCapacitySource              string            `json:"cpu_capacity_source,omitempty"`
	CPUCapacityKnown               bool              `json:"cpu_capacity_known"`
	PeakHeapAllocBytes             uint64            `json:"peak_heap_alloc_bytes"`
	PeakGoroutines                 int               `json:"peak_goroutines"`
	GCCycles                       uint32            `json:"gc_cycles"`
	GoCPUBusySeconds               float64           `json:"go_cpu_busy_seconds"`
	GoCPUCapacitySeconds           float64           `json:"go_cpu_capacity_seconds"`
	GoCPUUtilizationRatio          float64           `json:"go_cpu_utilization_ratio"`
	GoCPUValid                     bool              `json:"go_cpu_valid"`
	ProcessSamples                 int               `json:"process_samples"`
	InvalidProcessSamples          int               `json:"invalid_process_samples,omitempty"`
	ProcessCPUSamples              int               `json:"process_cpu_samples"`
	PeakProcessRSSBytes            uint64            `json:"peak_process_rss_bytes,omitempty"`
	PeakProcessCPUUtilizationRatio float64           `json:"peak_process_cpu_utilization_ratio"`
	MeanProcessCPUUtilizationRatio float64           `json:"mean_process_cpu_utilization_ratio"`
	ProcessCPUValid                bool              `json:"process_cpu_valid"`
	Guardrails                     *Guardrails       `json:"guardrails,omitempty"`
	GuardrailTrigger               *GuardrailTrigger `json:"guardrail_trigger,omitempty"`
	Note                           string            `json:"note"`
}

// Guardrails optionally stop a run when sampled generator process resources
// exceed operator-selected limits. CPU is normalized to detected CPU capacity.
type Guardrails struct {
	MaxProcessCPUPercent float64 `json:"max_process_cpu_percent,omitempty"`
	MaxProcessRSSBytes   uint64  `json:"max_process_rss_bytes,omitempty"`
	ConsecutiveSamples   int     `json:"consecutive_samples,omitempty"`
}

func (g Guardrails) Enabled() bool {
	return g.MaxProcessCPUPercent > 0 || g.MaxProcessRSSBytes > 0
}

func (g Guardrails) Validate() bool {
	if math.IsNaN(g.MaxProcessCPUPercent) || math.IsInf(g.MaxProcessCPUPercent, 0) || g.MaxProcessCPUPercent < 0 || g.MaxProcessCPUPercent > 100 {
		return false
	}
	if !g.Enabled() {
		return g.ConsecutiveSamples == 0
	}
	return g.ConsecutiveSamples >= 2 && g.ConsecutiveSamples <= 20
}

func (g Guardrails) Equal(other Guardrails) bool { return g == other }

// GuardrailTrigger contains the first consecutive sample sequence that
// crossed one configured generator-resource limit.
type GuardrailTrigger struct {
	Resource           string  `json:"resource"`
	ObservedCPUPercent float64 `json:"observed_cpu_percent,omitempty"`
	LimitCPUPercent    float64 `json:"limit_cpu_percent,omitempty"`
	ObservedRSSBytes   uint64  `json:"observed_rss_bytes,omitempty"`
	LimitRSSBytes      uint64  `json:"limit_rss_bytes,omitempty"`
	ConsecutiveSamples int     `json:"consecutive_samples"`
	SampleNumber       int     `json:"sample_number"`
	ElapsedMS          float64 `json:"elapsed_ms"`
}

const note = "Generator-side Go runtime and supported OS process samples only; RSS is not total host memory. CPU utilization is normalized to detected capacity bounded by logical CPUs, GOMAXPROCS, and finite Linux cgroup CPU quotas across the process hierarchy and readable cgroup v2 effective cpusets. An unavailable quota is not treated as unlimited. The peak is the highest sampled interval and may reflect a short burst. Sampling may miss brief peaks and does not describe the target application."

type reading struct {
	at                time.Time
	collectionTime    time.Duration
	heap              uint64
	goroutines        int
	gcCycles          uint32
	logicalCPUs       int
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
		logicalCPUs:  runtime.NumCPU(),
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
		a.summary.LogicalCPUs = r.logicalCPUs
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
	return left.logicalCPUs == right.logicalCPUs && left.logicalCPUs > 0 &&
		left.maxProcs == right.maxProcs && left.maxProcs > 0 &&
		capacityCores(left) == capacityCores(right) && left.cpuCapacitySource == right.cpuCapacitySource &&
		left.cpuCapacityKnown == right.cpuCapacityKnown
}

// Monitor records a bounded set of in-process runtime samples; only aggregate
// statistics are exported. It never contacts the target or launches commands.
type Monitor struct {
	stop        chan struct{}
	done        chan struct{}
	once        sync.Once
	acc         accumulator
	guardrails  Guardrails
	startedAt   time.Time
	previous    reading
	hasPrevious bool
	cpuBreaches int
	rssBreaches int
	trigger     *GuardrailTrigger
	onTrigger   func(GuardrailTrigger)
}

// Start begins self-monitoring. Sampling runs separately from request workers.
// Call Stop after test workers finish; Stop is safe to call more than once.
func Start(interval time.Duration) *Monitor {
	return StartWithGuardrails(interval, Guardrails{}, nil)
}

// StartWithGuardrails monitors resources and invokes onTrigger once, from the
// sampler goroutine, when the configured consecutive-sample policy is met.
func StartWithGuardrails(interval time.Duration, guardrails Guardrails, onTrigger func(GuardrailTrigger)) *Monitor {
	if interval < 100*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	if guardrails.Enabled() && guardrails.ConsecutiveSamples == 0 {
		guardrails.ConsecutiveSamples = 2
	}
	m := &Monitor{stop: make(chan struct{}), done: make(chan struct{}), guardrails: guardrails, startedAt: time.Now(), onTrigger: onTrigger}
	m.acc.summary.SamplingIntervalMS = float64(interval.Microseconds()) / 1000
	first := sampleRuntime()
	m.acc.add(first)
	m.observeGuardrails(first)
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				r := sampleRuntime()
				m.acc.add(r)
				m.observeGuardrails(r)
				return
			case <-ticker.C:
				r := sampleRuntime()
				m.acc.add(r)
				m.observeGuardrails(r)
			}
		}
	}()
	return m
}

func (m *Monitor) observeGuardrails(r reading) {
	if !m.guardrails.Enabled() || m.trigger != nil {
		return
	}
	cpuPercent := 0.0
	cpuValid := m.hasPrevious && r.processAvailable && m.previous.processAvailable &&
		r.at.After(m.previous.at) && sameCPUCapacity(r, m.previous) && r.processCPUSeconds >= m.previous.processCPUSeconds
	if cpuValid {
		capacity := r.at.Sub(m.previous.at).Seconds() * capacityCores(r)
		cpuValid = capacity > 0
		if cpuValid {
			cpuPercent = (r.processCPUSeconds - m.previous.processCPUSeconds) / capacity * 100
		}
	}
	if m.guardrails.MaxProcessCPUPercent > 0 {
		if cpuValid && cpuPercent > m.guardrails.MaxProcessCPUPercent {
			m.cpuBreaches++
		} else {
			m.cpuBreaches = 0
		}
	}
	if m.guardrails.MaxProcessRSSBytes > 0 {
		if r.processAvailable && r.processRSSBytes > m.guardrails.MaxProcessRSSBytes {
			m.rssBreaches++
		} else {
			m.rssBreaches = 0
		}
	}
	m.previous, m.hasPrevious = r, true
	trigger := GuardrailTrigger{}
	switch {
	case m.guardrails.MaxProcessCPUPercent > 0 && m.cpuBreaches >= m.guardrails.ConsecutiveSamples:
		trigger = GuardrailTrigger{Resource: "generator_process_cpu", ObservedCPUPercent: cpuPercent,
			LimitCPUPercent: m.guardrails.MaxProcessCPUPercent, ConsecutiveSamples: m.cpuBreaches}
	case m.guardrails.MaxProcessRSSBytes > 0 && m.rssBreaches >= m.guardrails.ConsecutiveSamples:
		trigger = GuardrailTrigger{Resource: "generator_process_rss", ObservedRSSBytes: r.processRSSBytes,
			LimitRSSBytes: m.guardrails.MaxProcessRSSBytes, ConsecutiveSamples: m.rssBreaches}
	default:
		return
	}
	trigger.SampleNumber = m.acc.summary.Samples
	trigger.ElapsedMS = float64(r.at.Sub(m.startedAt).Microseconds()) / 1000
	m.trigger = &trigger
	if m.onTrigger != nil {
		m.onTrigger(trigger)
	}
}

// Stop completes sampling and returns a consistent value snapshot.
func (m *Monitor) Stop() Summary {
	m.once.Do(func() { close(m.stop) })
	<-m.done
	summary := m.acc.finish()
	if m.guardrails.Enabled() {
		policy := m.guardrails
		summary.Guardrails = &policy
	}
	summary.GuardrailTrigger = m.trigger
	return summary
}
