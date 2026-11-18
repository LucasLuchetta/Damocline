package generatorhealth

import (
	"testing"
	"time"
)

func TestStationaryRuntimeCPUCountersDoNotEstablishIdleWindow(t *testing.T) {
	start := time.Unix(0, 0)
	var samples accumulator
	samples.add(reading{at: start, logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: 10, cpuIdle: 8})
	samples.add(reading{at: start.Add(time.Second), logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: 10, cpuIdle: 8})
	if observed := samples.finish(); observed.GoCPUValid || observed.GoCPUCapacitySeconds != 0 || observed.GoCPUBusySeconds != 0 {
		t.Fatalf("stationary GC-snapshot counters were interpreted as a current idle window: %+v", observed)
	}
}

func TestAdvancingRuntimeCPUCountersCanEstablishIdleWindow(t *testing.T) {
	start := time.Unix(0, 0)
	var samples accumulator
	samples.add(reading{at: start, logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: 10, cpuIdle: 8})
	samples.add(reading{at: start.Add(time.Second), logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: 11, cpuIdle: 9})
	if observed := samples.finish(); !observed.GoCPUValid || observed.GoCPUCapacitySeconds != 1 || observed.GoCPUUtilizationRatio != 0 {
		t.Fatalf("advancing source observations for an idle interval were lost: %+v", observed)
	}
}

func TestInvalidRuntimeCPUDeltasRemainUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		total, idle float64
	}{
		{"total moved backwards", 9, 8},
		{"idle moved backwards", 11, 7},
		{"idle advanced beyond total", 11, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(0, 0)
			var samples accumulator
			samples.add(reading{at: start, logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: 10, cpuIdle: 8})
			samples.add(reading{at: start.Add(time.Second), logicalCPUs: 1, maxProcs: 1, cpuAvailable: true, cpuTotal: tc.total, cpuIdle: tc.idle})
			if got := samples.finish(); got.GoCPUValid || got.GoCPUBusySeconds != 0 || got.GoCPUCapacitySeconds != 0 || got.GoCPUUtilizationRatio != 0 {
				t.Fatalf("invalid source deltas produced measured CPU: %+v", got)
			}
		})
	}
}
