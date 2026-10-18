//go:build !linux

package generatorhealth

func currentCPUCapacity(logicalCPUs, maxProcs int) cpuCapacity {
	if logicalCPUs < 1 || maxProcs < 1 {
		return cpuCapacity{}
	}
	capacity := logicalCPUs
	if maxProcs < capacity {
		capacity = maxProcs
	}
	return cpuCapacity{cores: float64(capacity), source: "runtime_logical_cpus_and_gomaxprocs", known: true}
}
