//go:build linux

package generatorhealth

import (
	"math"
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
	residentBytes, ok := residentBytesFromPages(pages, os.Getpagesize())
	if !ok {
		return 0, 0, false
	}
	cpuSeconds, ok = processCPUTime(usage.Utime.Sec, usage.Utime.Usec, usage.Stime.Sec, usage.Stime.Usec)
	if !ok || math.IsNaN(cpuSeconds) || math.IsInf(cpuSeconds, 0) {
		return 0, 0, false
	}
	return cpuSeconds, residentBytes, true
}

func residentBytesFromPages(pages uint64, pageSize int) (uint64, bool) {
	if pages == 0 || pageSize <= 0 || pages > ^uint64(0)/uint64(pageSize) {
		return 0, false
	}
	return pages * uint64(pageSize), true
}

func processCPUTime(userSeconds, userMicroseconds, systemSeconds, systemMicroseconds int64) (float64, bool) {
	maxInt64 := int64(^uint64(0) >> 1)
	if userSeconds < 0 || systemSeconds < 0 || userMicroseconds < 0 || userMicroseconds >= 1_000_000 ||
		systemMicroseconds < 0 || systemMicroseconds >= 1_000_000 || userSeconds > maxInt64-systemSeconds {
		return 0, false
	}
	seconds := userSeconds + systemSeconds
	value := float64(seconds) + float64(userMicroseconds+systemMicroseconds)/1e6
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}
