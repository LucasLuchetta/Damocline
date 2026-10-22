package hostmon

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ReadWorkload reads bounded process identity and cgroup-v2 counters for pid.
// Cgroup limits are returned as unknown when files or controllers are absent.
func ReadWorkload(pid int) (*Workload, error) {
	if pid < 1 {
		return nil, fmt.Errorf("pid must be positive")
	}
	root := filepath.Join("/proc", strconv.Itoa(pid))
	if _, err := os.Stat(filepath.Join(root, "stat")); err != nil {
		return nil, fmt.Errorf("process %d is unavailable: %w", pid, err)
	}
	result := &Workload{PID: pid}
	if stat, err := readText(filepath.Join(root, "stat")); err == nil {
		if parsed, parseErr := parseProcessStat(stat); parseErr == nil {
			result.StartTimeTicks = parsed.StartTimeTicks
			if uint64(parsed.RSSPages) <= ^uint64(0)/uint64(syscall.Getpagesize()) {
				rss := uint64(parsed.RSSPages) * uint64(syscall.Getpagesize())
				result.ProcessRSSBytes = &rss
			}
			if parsed.CPUUserTicks <= ^uint64(0)-parsed.CPUSystemTicks {
				ticks := parsed.CPUUserTicks + parsed.CPUSystemTicks
				result.ProcessCPUTimeTicks = &ticks
				if clockTicks, clockErr := readClockTicks(); clockErr == nil {
					result.ProcessCPUClockTicksPerSecond = clockTicks
				}
			}
		}
	}
	group, err := processCgroup(root)
	if err != nil {
		return result, nil
	}
	group, err = namespaceCgroup(group)
	if err != nil {
		return result, nil
	}
	cgroupPath := filepath.Join("/sys/fs/cgroup", filepath.FromSlash(group))
	cgroupRoot := filepath.Clean("/sys/fs/cgroup")
	if cgroupPath != cgroupRoot && !strings.HasPrefix(cgroupPath, cgroupRoot+string(os.PathSeparator)) {
		return result, nil
	}
	if value, err := readText(filepath.Join(cgroupPath, "cpu.max")); err == nil {
		fields := strings.Fields(value)
		if len(fields) == 2 {
			period, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr == nil && period > 0 {
				result.CPUPeriodMicros = &period
				if fields[0] != "max" {
					quota, parseErr := strconv.ParseInt(fields[0], 10, 64)
					if parseErr == nil && quota >= 0 {
						result.CPUQuotaMicros = &quota
					}
				}
			}
		}
	}
	if value, err := readText(filepath.Join(cgroupPath, "memory.max")); err == nil && value != "max" {
		if n, parseErr := strconv.ParseUint(value, 10, 64); parseErr == nil {
			result.MemoryLimitBytes = &n
		}
	}
	if value, err := readText(filepath.Join(cgroupPath, "memory.current")); err == nil {
		if n, parseErr := strconv.ParseUint(value, 10, 64); parseErr == nil {
			result.MemoryCurrentBytes = &n
		}
	}
	if value, err := readText(filepath.Join(cgroupPath, "memory.events")); err == nil {
		result.MemoryEvents = parseCounters(value)
	}
	return result, nil
}

// parseProcessStat handles a process comm field that may contain spaces and
// closing parentheses. It returns the kernel start-time ticks and RSS pages.
type processStat struct {
	StartTimeTicks uint64
	RSSPages       int64
	CPUUserTicks   uint64
	CPUSystemTicks uint64
}

func parseProcessStat(value string) (processStat, error) {
	closeComm := strings.LastIndex(value, ")")
	if closeComm < 0 || closeComm+1 >= len(value) {
		return processStat{}, fmt.Errorf("invalid process stat comm")
	}
	fields := strings.Fields(value[closeComm+1:])
	// Fields begin at proc stat field 3 (state); starttime is field 22 and rss
	// is field 24.
	if len(fields) <= 21 {
		return processStat{}, fmt.Errorf("process stat is truncated")
	}
	userTicks, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return processStat{}, fmt.Errorf("invalid process user CPU ticks")
	}
	systemTicks, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return processStat{}, fmt.Errorf("invalid process system CPU ticks")
	}
	start, parseErr := strconv.ParseUint(fields[19], 10, 64)
	if parseErr != nil {
		return processStat{}, fmt.Errorf("invalid process start time")
	}
	rss, parseErr := strconv.ParseInt(fields[21], 10, 64)
	if parseErr != nil || rss < 0 {
		return processStat{}, fmt.Errorf("invalid process resident pages")
	}
	return processStat{StartTimeTicks: start, RSSPages: rss, CPUUserTicks: userTicks, CPUSystemTicks: systemTicks}, nil
}

// readClockTicks obtains AT_CLKTCK from the Linux auxiliary vector (the
// runtime's _SC_CLK_TCK equivalent), avoiding an assumed fixed tick rate.
func readClockTicks() (uint64, error) {
	data, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return 0, err
	}
	return parseAuxvClockTicks(data)
}

func parseAuxvClockTicks(data []byte) (uint64, error) {
	if len(data) > 4096 {
		return 0, fmt.Errorf("process auxiliary vector exceeds limit")
	}
	word := strconv.IntSize / 8
	if word != 4 && word != 8 || len(data)%(2*word) != 0 {
		return 0, fmt.Errorf("invalid process auxiliary vector")
	}
	for offset := 0; offset+2*word <= len(data); offset += 2 * word {
		var key, value uint64
		if word == 8 {
			key = binary.NativeEndian.Uint64(data[offset : offset+word])
			value = binary.NativeEndian.Uint64(data[offset+word : offset+2*word])
		} else {
			key = uint64(binary.NativeEndian.Uint32(data[offset : offset+word]))
			value = uint64(binary.NativeEndian.Uint32(data[offset+word : offset+2*word]))
		}
		if key == 0 {
			break
		}
		if key == 17 && value > 0 {
			return value, nil
		}
	}
	return 0, fmt.Errorf("AT_CLKTCK is unavailable")
}

func applyProcessCPUDelta(previous, current *Workload, elapsed time.Duration) {
	if current == nil {
		return
	}
	current.ProcessCPUCoverage = "INCOMPLETE"
	if previous == nil || elapsed <= 0 || previous.PID != current.PID || previous.StartTimeTicks == 0 || previous.StartTimeTicks != current.StartTimeTicks ||
		previous.ProcessCPUTimeTicks == nil || current.ProcessCPUTimeTicks == nil || current.ProcessCPUClockTicksPerSecond == 0 ||
		previous.ProcessCPUClockTicksPerSecond != current.ProcessCPUClockTicksPerSecond || *current.ProcessCPUTimeTicks < *previous.ProcessCPUTimeTicks {
		return
	}
	delta := *current.ProcessCPUTimeTicks - *previous.ProcessCPUTimeTicks
	percent := float64(delta) / float64(current.ProcessCPUClockTicksPerSecond) / elapsed.Seconds() * 100
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		return
	}
	current.ProcessCPUDeltaTicks = &delta
	current.ProcessCPUPercent = &percent
	intervalMS := float64(elapsed) / float64(time.Millisecond)
	current.ProcessCPUIntervalMS = &intervalMS
	current.ProcessCPUCoverage = "COMPLETE"
}

// namespaceCgroup refuses paths outside the visible cgroup mount. PID sharing
// does not imply cgroup namespace sharing, so ../container-id cannot safely be
// mapped to this process's visible root.
func namespaceCgroup(group string) (string, error) {
	clean := strings.TrimPrefix(filepath.ToSlash(group), "./")
	if clean == ".." || strings.HasPrefix(clean, "../") || hasTraversal(clean) {
		return "", fmt.Errorf("unsafe process cgroup path")
	}
	return clean, nil
}

func processCgroup(procRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, "cgroup"))
	if err != nil {
		return "", fmt.Errorf("read process cgroup: %w", err)
	}
	return parseProcessCgroup(string(data))
}

func parseProcessCgroup(data string) (string, error) {
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		line = strings.TrimSpace(line)
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" {
			if fields[2] == "/" {
				return ".", nil
			}
			path := strings.TrimPrefix(fields[2], "/")
			if strings.HasPrefix(fields[2], "/") && !hasTraversal(fields[2]) {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("process is not in a visible cgroup-v2 hierarchy")
}

func hasTraversal(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func parseCounters(text string) map[string]uint64 {
	result := make(map[string]uint64)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) > 64 {
			continue
		}
		if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			result[fields[0]] = n
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
