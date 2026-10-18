//go:build !linux

package generatorhealth

func currentCPUCapacity(maxProcs int) cpuCapacity {
	if maxProcs < 1 {
		return cpuCapacity{}
	}
	return cpuCapacity{cores: float64(maxProcs), source: "runtime_gomaxprocs", known: true}
}
