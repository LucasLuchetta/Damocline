// Package hostmon samples Linux host-level CPU and memory from procfs.
// It does not infer a bottleneck or expose a listening port.
package hostmon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Counters struct{ Total, Idle uint64 }
type Sample struct {
	At               string  `json:"at"`
	CPUPercent       float64 `json:"cpu_percent"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes uint64  `json:"memory_total_bytes"`
}
type Result struct {
	SchemaVersion int      `json:"schema_version"`
	Source        string   `json:"source"`
	StartedAt     string   `json:"started_at"`
	Interval      string   `json:"interval"`
	Samples       []Sample `json:"samples"`
	Note          string   `json:"note"`
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
func read() (Counters, uint64, uint64, error) {
	cpuFile, err := os.Open("/proc/stat")
	if err != nil {
		return Counters{}, 0, 0, err
	}
	defer cpuFile.Close()
	c, err := ParseCPU(cpuFile)
	if err != nil {
		return Counters{}, 0, 0, err
	}
	memFile, err := os.Open("/proc/meminfo")
	if err != nil {
		return Counters{}, 0, 0, err
	}
	defer memFile.Close()
	total, avail, err := ParseMemory(memFile)
	return c, total, avail, err
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
	if runtime.GOOS != "linux" {
		return Result{}, fmt.Errorf("host monitor currently supports native Linux only")
	}
	if duration < interval || interval < time.Second || interval > 30*time.Second || duration > 15*time.Minute {
		return Result{}, fmt.Errorf("duration must be >= interval, interval 1s..30s, duration <=15m")
	}
	first, _, _, err := read()
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	result := Result{SchemaVersion: 1, Source: "linux:/proc", StartedAt: start.UTC().Format(time.RFC3339Nano), Interval: interval.String(), Note: "Host-wide CPU and MemAvailable estimates. Does not isolate containers, processes, databases, or root causes; run natively on the target Linux host."}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	previous := first
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-timer.C:
			return result, nil
		case now := <-ticker.C:
			c, total, avail, err := read()
			if err != nil {
				return result, err
			}
			result.Samples = append(result.Samples, Sample{At: now.UTC().Format(time.RFC3339Nano), CPUPercent: utilization(previous, c), MemoryUsedBytes: total - avail, MemoryTotalBytes: total})
			previous = c
		}
	}
}
