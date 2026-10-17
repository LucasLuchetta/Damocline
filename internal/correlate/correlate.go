// Package correlate joins independent load and host samples by UTC timestamps.
// It reports observations, not automatically established bottleneck causes.
package correlate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

type Window struct {
	Users           int     `json:"users"`
	RunIndex        int     `json:"run_index"`
	LoadOutcome     string  `json:"load_outcome"`
	CPUAvgPercent   float64 `json:"cpu_avg_percent,omitempty"`
	CPUPeakPercent  float64 `json:"cpu_peak_percent,omitempty"`
	MemoryPeakBytes uint64  `json:"memory_peak_bytes,omitempty"`
	Samples         int     `json:"host_samples"`
}
type Result struct {
	SchemaVersion int      `json:"schema_version"`
	LoadScenario  string   `json:"load_scenario"`
	HostSource    string   `json:"host_source"`
	Windows       []Window `json:"windows"`
	Warnings      []string `json:"warnings"`
}

func Analyze(load explorer.Result, host hostmon.Result) (Result, error) {
	if len(load.Probes) == 0 || len(host.Samples) == 0 {
		return Result{}, fmt.Errorf("both load probes and host samples are required")
	}
	result := Result{SchemaVersion: 1, LoadScenario: load.Scenario, HostSource: host.Source,
		Warnings: []string{"Host-wide CPU and RAM observations can correlate with response slowdowns but do not prove a root cause.", "Synchronize host and generator clocks with NTP before interpreting time windows.", "The load report does not contain server topology, database metrics, or container-specific utilization."}}
	for _, p := range load.Probes {
		for index, run := range p.Runs {
			start, err := time.Parse(time.RFC3339, run.StartedAt)
			if err != nil {
				return Result{}, fmt.Errorf("invalid load timestamp: %w", err)
			}
			end := start.Add(time.Duration(run.ElapsedSeconds * float64(time.Second)))
			w := Window{Users: p.Users, RunIndex: index + 1, LoadOutcome: run.Outcome}
			for _, sample := range host.Samples {
				t, err := time.Parse(time.RFC3339Nano, sample.At)
				if err != nil {
					return Result{}, fmt.Errorf("invalid host timestamp: %w", err)
				}
				if t.Before(start) || t.After(end) {
					continue
				}
				w.Samples++
				w.CPUAvgPercent += sample.CPUPercent
				if sample.CPUPercent > w.CPUPeakPercent {
					w.CPUPeakPercent = sample.CPUPercent
				}
				if sample.MemoryUsedBytes > w.MemoryPeakBytes {
					w.MemoryPeakBytes = sample.MemoryUsedBytes
				}
			}
			if w.Samples > 0 {
				w.CPUAvgPercent = round(w.CPUAvgPercent / float64(w.Samples))
			}
			result.Windows = append(result.Windows, w)
		}
	}
	return result, nil
}
func round(x float64) float64 { return float64(int(x*100+0.5)) / 100 }
func LoadFiles(loadPath, hostPath string) (Result, error) {
	a, err := os.ReadFile(loadPath)
	if err != nil {
		return Result{}, err
	}
	b, err := os.ReadFile(hostPath)
	if err != nil {
		return Result{}, err
	}
	var l explorer.Result
	var h hostmon.Result
	if err := json.Unmarshal(a, &l); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return Result{}, err
	}
	return Analyze(l, h)
}
