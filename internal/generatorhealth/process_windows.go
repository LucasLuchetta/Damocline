//go:build windows

package generatorhealth

import (
	"syscall"
	"unsafe"
)

type processMemoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var getProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func readProcessHealth() (cpuSeconds float64, residentBytes uint64, ok bool) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0, false
	}
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, 0, false
	}
	memory := processMemoryCounters{Size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, _ := getProcessMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size))
	if result == 0 {
		return 0, 0, false
	}
	cpuTicks := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	userTicks := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	if userTicks > ^uint64(0)-cpuTicks || memory.WorkingSetSize == 0 {
		return 0, 0, false
	}
	cpuTicks += userTicks
	return float64(cpuTicks) / 1e7, uint64(memory.WorkingSetSize), true
}
