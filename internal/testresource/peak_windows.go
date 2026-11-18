//go:build integration && windows

package testresource

import (
	"fmt"
	"syscall"
	"unsafe"
)

type reportResourceMemoryCounters struct {
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

// PeakRSSMethod identifies the platform kernel high-water measurement.
const PeakRSSMethod = "windows_peak_working_set"

func PeakRSS() (uint64, error) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, err
	}
	memory := reportResourceMemoryCounters{Size: uint32(unsafe.Sizeof(reportResourceMemoryCounters{}))}
	procedure := syscall.NewLazyDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
	result, _, callErr := procedure.Call(uintptr(handle), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size))
	if result == 0 || memory.PeakWorkingSetSize == 0 {
		return 0, fmt.Errorf("process peak working set unavailable: %v", callErr)
	}
	return uint64(memory.PeakWorkingSetSize), nil
}
