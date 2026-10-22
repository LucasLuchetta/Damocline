// Package hostmon samples Linux host-level CPU and memory from procfs.
// It does not infer a bottleneck or expose a listening port.
package hostmon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Counters struct{ Total, Idle uint64 }
type IOKind struct {
	ReadBytes, WriteBytes uint64
	Devices               int
	DeviceSet             string
	Available             bool
}
type NetworkCounters struct {
	ReceiveBytes, TransmitBytes uint64
	Interfaces                  int
	InterfaceSet                string
	Available                   bool
}
type Sample struct {
	At               string    `json:"at"`
	CPUPercent       float64   `json:"cpu_percent"`
	MemoryUsedBytes  uint64    `json:"memory_used_bytes"`
	MemoryTotalBytes uint64    `json:"memory_total_bytes"`
	SwapUsedBytes    *uint64   `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes   *uint64   `json:"swap_total_bytes,omitempty"`
	DiskReadBytes    *uint64   `json:"disk_read_bytes,omitempty"`
	DiskWriteBytes   *uint64   `json:"disk_write_bytes,omitempty"`
	NetworkRxBytes   *uint64   `json:"network_receive_bytes,omitempty"`
	NetworkTxBytes   *uint64   `json:"network_transmit_bytes,omitempty"`
	DiskCoverage     string    `json:"disk_io_coverage,omitempty"`
	NetworkCoverage  string    `json:"network_io_coverage,omitempty"`
	Workload         *Workload `json:"workload,omitempty"`
}
type Result struct {
	SchemaVersion int      `json:"schema_version"`
	Source        string   `json:"source"`
	StartedAt     string   `json:"started_at"`
	Interval      string   `json:"interval"`
	Samples       []Sample `json:"samples"`
	Note          string   `json:"note"`
}

// Workload contains read-only process and cgroup-v2 evidence for one selected
// process. Missing cgroup values remain unknown (nil), never zero.
type Workload struct {
	PID                           int               `json:"pid"`
	StartTimeTicks                uint64            `json:"process_start_time_ticks,omitempty"`
	ProcessRSSBytes               *uint64           `json:"process_rss_bytes,omitempty"`
	ProcessCPUTimeTicks           *uint64           `json:"process_cpu_time_ticks,omitempty"`
	ProcessCPUDeltaTicks          *uint64           `json:"process_cpu_delta_ticks,omitempty"`
	ProcessCPUPercent             *float64          `json:"process_cpu_percent,omitempty"`
	ProcessCPUIntervalMS          *float64          `json:"process_cpu_interval_ms,omitempty"`
	ProcessCPUClockTicksPerSecond uint64            `json:"process_cpu_clock_ticks_per_second,omitempty"`
	ProcessCPUCoverage            string            `json:"process_cpu_coverage,omitempty"`
	CPUQuotaMicros                *int64            `json:"cpu_quota_micros,omitempty"`
	CPUPeriodMicros               *uint64           `json:"cpu_period_micros,omitempty"`
	MemoryLimitBytes              *uint64           `json:"memory_limit_bytes,omitempty"`
	MemoryCurrentBytes            *uint64           `json:"memory_current_bytes,omitempty"`
	MemoryEvents                  map[string]uint64 `json:"memory_events,omitempty"`
	RestartCount                  *uint64           `json:"restart_count,omitempty"`
}

func ParseCPU(r io.Reader) (Counters, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return Counters{}, fmt.Errorf("empty /proc/stat")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return Counters{}, fmt.Errorf("invalid CPU counters")
	}
	var values []uint64
	for _, field := range fields[1:] {
		v, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return Counters{}, fmt.Errorf("invalid CPU counter: %w", err)
		}
		values = append(values, v)
	}
	var total uint64
	for _, v := range values {
		total += v
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return Counters{Total: total, Idle: idle}, nil
}
func ParseMemory(r io.Reader) (total, available uint64, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		val, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = val * 1024
		case "MemAvailable:":
			available = val * 1024
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if total == 0 || available > total {
		return 0, 0, fmt.Errorf("missing or invalid MemTotal/MemAvailable")
	}
	return total, available, nil
}
func ParseSwap(r io.Reader) (total, free uint64, err error) {
	scanner := bufio.NewScanner(r)
	totalSeen, freeSeen := false, false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "SwapTotal:" && fields[0] != "SwapFree:" {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil || value > ^uint64(0)/1024 {
			return 0, 0, fmt.Errorf("invalid swap counters")
		}
		value *= 1024
		switch fields[0] {
		case "SwapTotal:":
			total, totalSeen = value, true
		case "SwapFree:":
			free, freeSeen = value, true
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if !totalSeen || !freeSeen || free > total {
		return 0, 0, fmt.Errorf("missing or invalid SwapTotal/SwapFree")
	}
	return total, free, nil
}
func ParseDiskStats(r io.Reader, isPartition ...func(string) bool) (IOKind, error) {
	scanner := bufio.NewScanner(r)
	var result IOKind
	seen := false
	devices := make([]string, 0)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			return IOKind{}, fmt.Errorf("invalid /proc/diskstats row")
		}
		// Linux diskstats sectors are 512 bytes. Exclude partitions when the
		// caller can check sysfs, avoiding double counting parent devices.
		name := fields[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		if len(isPartition) > 0 && isPartition[0](fields[0]+":"+fields[1]) {
			continue
		}
		sectorsRead, err1 := strconv.ParseUint(fields[5], 10, 64)
		sectorsWritten, err2 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil || sectorsRead > ^uint64(0)/512 || sectorsWritten > ^uint64(0)/512 {
			return IOKind{}, fmt.Errorf("invalid /proc/diskstats counters")
		}
		readBytes, writeBytes := sectorsRead*512, sectorsWritten*512
		if result.ReadBytes > ^uint64(0)-readBytes || result.WriteBytes > ^uint64(0)-writeBytes {
			return IOKind{}, fmt.Errorf("/proc/diskstats counters overflow")
		}
		result.ReadBytes += readBytes
		result.WriteBytes += writeBytes
		result.Devices++
		devices = append(devices, fields[0]+":"+fields[1])
		seen = true
	}
	if err := scanner.Err(); err != nil {
		return IOKind{}, err
	}
	if !seen {
		return IOKind{}, fmt.Errorf("no block devices in /proc/diskstats")
	}
	sort.Strings(devices)
	result.DeviceSet = strings.Join(devices, ",")
	result.Available = true
	return result, nil
}

func ParseNetworkDev(r io.Reader) (NetworkCounters, error) {
	scanner := bufio.NewScanner(r)
	var result NetworkCounters
	seen := false
	interfaces := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			return NetworkCounters{}, fmt.Errorf("invalid /proc/net/dev row")
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil || result.ReceiveBytes > ^uint64(0)-rx || result.TransmitBytes > ^uint64(0)-tx {
			return NetworkCounters{}, fmt.Errorf("invalid /proc/net/dev counters")
		}
		result.ReceiveBytes += rx
		result.TransmitBytes += tx
		result.Interfaces++
		interfaces = append(interfaces, name)
		seen = true
	}
	if err := scanner.Err(); err != nil {
		return NetworkCounters{}, err
	}
	if !seen {
		return NetworkCounters{}, fmt.Errorf("no non-loopback interfaces in /proc/net/dev")
	}
	sort.Strings(interfaces)
	result.InterfaceSet = strings.Join(interfaces, ",")
	result.Available = true
	return result, nil
}

func read() (Counters, uint64, uint64, uint64, uint64, IOKind, NetworkCounters, error) {
	cpuFile, err := os.Open("/proc/stat")
	if err != nil {
		return Counters{}, 0, 0, 0, 0, IOKind{}, NetworkCounters{}, err
	}
	defer cpuFile.Close()
	c, err := ParseCPU(cpuFile)
	if err != nil {
		return Counters{}, 0, 0, 0, 0, IOKind{}, NetworkCounters{}, err
	}
	memInfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Counters{}, 0, 0, 0, 0, IOKind{}, NetworkCounters{}, err
	}
	total, avail, err := ParseMemory(strings.NewReader(string(memInfo)))
	if err != nil {
		return Counters{}, 0, 0, 0, 0, IOKind{}, NetworkCounters{}, err
	}
	swapTotal, swapFree, err := ParseSwap(strings.NewReader(string(memInfo)))
	if err != nil {
		return Counters{}, 0, 0, 0, 0, IOKind{}, NetworkCounters{}, err
	}
	disk := IOKind{}
	if diskFile, openErr := os.Open("/proc/diskstats"); openErr == nil {
		parsed, parseErr := ParseDiskStats(diskFile, func(majorMinor string) bool {
			_, statErr := os.Stat(filepath.Join("/sys/dev/block", majorMinor, "partition"))
			return statErr == nil
		})
		_ = diskFile.Close()
		if parseErr == nil {
			disk = parsed
		}
	}
	network := NetworkCounters{}
	if netFile, openErr := os.Open("/proc/net/dev"); openErr == nil {
		parsed, parseErr := ParseNetworkDev(netFile)
		_ = netFile.Close()
		if parseErr == nil {
			network = parsed
		}
	}
	return c, total, avail, swapTotal, swapFree, disk, network, nil
}
func utilization(prev, next Counters) float64 {
	if next.Total <= prev.Total || next.Idle < prev.Idle {
		return 0
	}
	busy := float64(next.Total-prev.Total-(next.Idle-prev.Idle)) / float64(next.Total-prev.Total) * 100
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return float64(int(busy*100+0.5)) / 100
}
func Collect(ctx context.Context, duration, interval time.Duration) (Result, error) {
	return CollectWithWorkload(ctx, duration, interval, 0)
}

func CollectWithWorkload(ctx context.Context, duration, interval time.Duration, pid int) (Result, error) {
	if runtime.GOOS != "linux" {
		return Result{}, fmt.Errorf("host monitor currently supports native Linux only")
	}
	if duration < interval || interval < time.Second || interval > 30*time.Second || duration > 15*time.Minute {
		return Result{}, fmt.Errorf("duration must be >= interval, interval 1s..30s, duration <=15m")
	}
	first, _, _, _, _, firstDisk, firstNetwork, err := read()
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	result := Result{SchemaVersion: 1, Source: "linux:/proc", StartedAt: start.UTC().Format(time.RFC3339Nano), Interval: interval.String(), Note: "Host-wide CPU, memory, swap, disk, and network observations. Selected-process RSS and CPU are separate from cgroup limits. I/O deltas may be unavailable when visible counters reset or device sets change; these readings do not identify bottleneck causes."}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	previous, previousDisk, previousNetwork := first, firstDisk, firstNetwork
	var previousWorkload *Workload
	previousWorkloadAt := time.Now()
	if pid > 0 {
		previousWorkload, _ = ReadWorkload(pid)
	}
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-timer.C:
			return result, nil
		case now := <-ticker.C:
			c, total, avail, swapTotal, swapFree, disk, network, err := read()
			if err != nil {
				return result, err
			}
			swapUsed := swapTotal - swapFree
			diskCoverage, networkCoverage := "COMPLETE", "COMPLETE"
			var diskRead, diskWrite, networkRx, networkTx *uint64
			if disk.Available && previousDisk.Available && disk.DeviceSet == previousDisk.DeviceSet && disk.ReadBytes >= previousDisk.ReadBytes && disk.WriteBytes >= previousDisk.WriteBytes {
				r, w := disk.ReadBytes-previousDisk.ReadBytes, disk.WriteBytes-previousDisk.WriteBytes
				diskRead, diskWrite = &r, &w
			} else {
				diskCoverage = "INCOMPLETE"
			}
			if network.Available && previousNetwork.Available && network.InterfaceSet == previousNetwork.InterfaceSet && network.ReceiveBytes >= previousNetwork.ReceiveBytes && network.TransmitBytes >= previousNetwork.TransmitBytes {
				rx, tx := network.ReceiveBytes-previousNetwork.ReceiveBytes, network.TransmitBytes-previousNetwork.TransmitBytes
				networkRx, networkTx = &rx, &tx
			} else {
				networkCoverage = "INCOMPLETE"
			}
			sample := Sample{At: now.UTC().Format(time.RFC3339Nano), CPUPercent: utilization(previous, c), MemoryUsedBytes: total - avail, MemoryTotalBytes: total, SwapUsedBytes: &swapUsed, SwapTotalBytes: &swapTotal, DiskReadBytes: diskRead, DiskWriteBytes: diskWrite, NetworkRxBytes: networkRx, NetworkTxBytes: networkTx, DiskCoverage: diskCoverage, NetworkCoverage: networkCoverage}
			if pid > 0 {
				if workload, workloadErr := ReadWorkload(pid); workloadErr == nil {
					observedAt := time.Now()
					applyProcessCPUDelta(previousWorkload, workload, observedAt.Sub(previousWorkloadAt))
					sample.Workload = workload
					previousWorkload, previousWorkloadAt = workload, observedAt
				} else {
					previousWorkload = nil
					previousWorkloadAt = time.Now()
				}
			}
			result.Samples = append(result.Samples, sample)
			previous, previousDisk, previousNetwork = c, disk, network
		}
	}
}
