//go:build !windows && !linux

package generatorhealth

func readProcessHealth() (cpuSeconds float64, residentBytes uint64, ok bool) {
	return 0, 0, false
}
