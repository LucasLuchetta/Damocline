//go:build linux

package generatorhealth

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func readProcessHealth() (cpuSeconds float64, residentBytes uint64, ok bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0, false
	}
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0, 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	cpuSeconds = float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
	return cpuSeconds, pages * uint64(os.Getpagesize()), true
}
