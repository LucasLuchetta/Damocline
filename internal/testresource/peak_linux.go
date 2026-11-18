//go:build integration && linux

package testresource

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// PeakRSSMethod identifies the current executable image's kernel high-water mark.
const PeakRSSMethod = "linux_proc_self_status_vmhwm"

func PeakRSS() (uint64, error) {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, fmt.Errorf("read process peak RSS: %w", err)
	}
	defer file.Close()
	// getrusage preserves pre-exec memory peaks. VmHWM describes the current
	// address space, so an earlier large test parent cannot inflate this image.
	return peakRSSFromStatus(file)
}

func peakRSSFromStatus(reader io.Reader) (uint64, error) {
	const maximumStatusBytes = 64 << 10
	data, err := io.ReadAll(io.LimitReader(reader, maximumStatusBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read process memory status: %w", err)
	}
	if len(data) > maximumStatusBytes {
		return 0, errors.New("process memory status exceeds size limit")
	}
	var peak uint64
	seen := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "VmHWM:" {
			continue
		}
		if seen || len(fields) != 3 || fields[2] != "kB" {
			return 0, errors.New("invalid process peak RSS field")
		}
		seen = true
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kib == 0 || kib > ^uint64(0)/1024 {
			return 0, errors.New("invalid process peak RSS value")
		}
		peak = kib * 1024
	}
	if !seen {
		return 0, errors.New("process peak RSS is unavailable")
	}
	return peak, nil
}
