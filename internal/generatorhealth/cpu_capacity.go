package generatorhealth

import "runtime"

type cpuCapacity struct {
	cores  float64
	source string
	known  bool
}

// CPUCapacity describes the CPU entitlement used to normalize generator
// runtime and process CPU observations.
type CPUCapacity struct {
	Cores  float64
	Source string
	Known  bool
}

func capacityAtSample() cpuCapacity {
	return currentCPUCapacity(runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

// CurrentCPUCapacity returns a current CPU capacity estimate and its evidence
// source. On Linux, an observed finite cgroup quota or a readable v2 effective
// cpuset can lower the estimate. Absent or unreadable cgroup quota
// evidence is reported as unknown; malformed present cpuset evidence
// fails closed.
func CurrentCPUCapacity() CPUCapacity {
	capacity := capacityAtSample()
	return CPUCapacity{Cores: capacity.cores, Source: capacity.source, Known: capacity.known}
}
