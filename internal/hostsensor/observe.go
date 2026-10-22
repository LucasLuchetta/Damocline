package hostsensor

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

const maxObservationAttempts = 120

type Failure struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

type Observation struct {
	ExperimentID string         `json:"experiment_id"`
	StartedAt    string         `json:"started_at"`
	EndedAt      string         `json:"ended_at"`
	Interval     string         `json:"interval"`
	Attempts     int            `json:"attempts"`
	Samples      []RemoteSample `json:"samples"`
	Failures     []Failure      `json:"failures,omitempty"`
	Coverage     string         `json:"coverage"`
	Reason       string         `json:"reason,omitempty"`
}

// ObserveWhile collects bounded host readings during one load phase.
func ObserveWhile(ctx context.Context, endpoint, token, experimentID string, interval time.Duration, work func()) Observation {
	start := time.Now()
	result := Observation{ExperimentID: experimentID, StartedAt: start.UTC().Format(time.RFC3339Nano), Interval: interval.String(), Samples: []RemoteSample{}, Coverage: "INCOMPLETE"}
	if err := ValidateEndpoint(endpoint, token); err != nil || !ValidExperimentID(experimentID) || interval < time.Second || interval > 30*time.Second {
		result.Reason = "invalid host sensor configuration"
		work()
		result.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return result
	}
	collectCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for result.Attempts < maxObservationAttempts {
			result.Attempts++
			sample, err := CollectRemote(collectCtx, endpoint, token, experimentID)
			if collectCtx.Err() != nil {
				return
			}
			if err != nil {
				result.Failures = append(result.Failures, Failure{At: time.Now().UTC().Format(time.RFC3339Nano), Reason: err.Error()})
			} else {
				result.Samples = append(result.Samples, sample)
			}
			select {
			case <-collectCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	work()
	end := time.Now()
	cancel()
	<-done
	result.EndedAt = end.UTC().Format(time.RFC3339Nano)
	result.Reason = coverageReason(result, start, end, interval)
	if result.Reason == "" {
		result.Coverage = "COMPLETE"
	}
	return result
}

func coverageReason(result Observation, start, end time.Time, interval time.Duration) string {
	if len(result.Failures) != 0 {
		return "one or more host sensor requests failed"
	}
	if len(result.Samples) < 2 {
		return "fewer than two host sensor samples were collected"
	}
	previous := start
	for _, sample := range result.Samples {
		at, err := time.Parse(time.RFC3339Nano, sample.ReceivedAt)
		if err != nil || at.Before(previous) || at.After(end) {
			return "host sensor sample timestamps are outside the phase"
		}
		if at.Sub(previous) > 2*interval+2*time.Second {
			return "host sensor collection gap exceeds two intervals plus sampling time"
		}
		previous = at
	}
	if end.Sub(previous) > 2*interval+2*time.Second {
		return "host sensor collection ended before phase end"
	}
	return ""
}

// ReassessCoverage recomputes collection coverage from persisted evidence.
func ReassessCoverage(result Observation) (string, error) {
	if !ValidExperimentID(result.ExperimentID) {
		return "", fmt.Errorf("invalid host sensor experiment ID")
	}
	start, err := time.Parse(time.RFC3339Nano, result.StartedAt)
	if err != nil {
		return "", fmt.Errorf("invalid host sensor start time")
	}
	end, err := time.Parse(time.RFC3339Nano, result.EndedAt)
	if err != nil || end.Before(start) {
		return "", fmt.Errorf("invalid host sensor end time")
	}
	interval, err := time.ParseDuration(result.Interval)
	if err != nil || interval < time.Second || interval > 30*time.Second {
		return "", fmt.Errorf("invalid host sensor interval")
	}
	if result.Attempts < 0 || result.Attempts > maxObservationAttempts || len(result.Samples) > result.Attempts || len(result.Failures) > result.Attempts {
		return "", fmt.Errorf("invalid host sensor collection counts")
	}
	for _, sample := range result.Samples {
		if sample.Reading.ExperimentID != result.ExperimentID || sample.Reading.SchemaVersion != 1 || sample.Reading.Host.SchemaVersion != 1 || len(sample.Reading.Host.Samples) != 1 || sample.Reading.Host.Source != "linux:/proc" {
			return "", fmt.Errorf("host sensor sample identity or schema changed")
		}
		if sample.RoundTripMS < 0 || math.IsNaN(sample.RoundTripMS) || math.IsInf(sample.RoundTripMS, 0) || math.IsNaN(sample.OffsetMinMS) || math.IsInf(sample.OffsetMinMS, 0) || math.IsNaN(sample.OffsetMaxMS) || math.IsInf(sample.OffsetMaxMS, 0) || sample.OffsetMaxMS < sample.OffsetMinMS || math.Abs(sample.OffsetMaxMS-sample.OffsetMinMS-sample.RoundTripMS) > 1 {
			return "", fmt.Errorf("invalid host sensor clock uncertainty")
		}
		host := sample.Reading.Host.Samples[0]
		if host.CPUPercent < 0 || host.CPUPercent > 100 || host.MemoryTotalBytes == 0 || host.MemoryUsedBytes > host.MemoryTotalBytes {
			return "", fmt.Errorf("invalid host sensor resource values")
		}
		if !validSwap(host) {
			return "", fmt.Errorf("invalid host sensor swap values")
		}
		if !validIO(host) {
			return "", fmt.Errorf("invalid host sensor I/O counters")
		}
		if workload := host.Workload; workload != nil {
			if workload.PID < 1 || workload.MemoryCurrentBytes != nil && workload.MemoryLimitBytes != nil && *workload.MemoryCurrentBytes > *workload.MemoryLimitBytes {
				return "", fmt.Errorf("invalid host sensor workload values")
			}
			switch workload.ProcessCPUCoverage {
			case "": // Legacy samples predate process CPU accounting.
			case "COMPLETE":
				if workload.ProcessCPUTimeTicks == nil || workload.ProcessCPUDeltaTicks == nil || workload.ProcessCPUPercent == nil || workload.ProcessCPUIntervalMS == nil || workload.ProcessCPUClockTicksPerSecond == 0 || *workload.ProcessCPUPercent < 0 || math.IsNaN(*workload.ProcessCPUPercent) || math.IsInf(*workload.ProcessCPUPercent, 0) || *workload.ProcessCPUIntervalMS <= 0 || math.IsNaN(*workload.ProcessCPUIntervalMS) || math.IsInf(*workload.ProcessCPUIntervalMS, 0) {
					return "", fmt.Errorf("invalid complete process CPU evidence")
				}
				expectedPercent := float64(*workload.ProcessCPUDeltaTicks) / float64(workload.ProcessCPUClockTicksPerSecond) / (*workload.ProcessCPUIntervalMS / 1000) * 100
				if math.Abs(expectedPercent-*workload.ProcessCPUPercent) > math.Max(0.000001, expectedPercent*0.000001) {
					return "", fmt.Errorf("process CPU percentage does not match its tick delta and interval")
				}
			case "INCOMPLETE":
				if workload.ProcessCPUDeltaTicks != nil || workload.ProcessCPUPercent != nil || workload.ProcessCPUIntervalMS != nil {
					return "", fmt.Errorf("incomplete process CPU evidence includes a delta")
				}
			default:
				return "", fmt.Errorf("invalid process CPU coverage")
			}
		}
	}
	reason := coverageReason(result, start, end, interval)
	if reason != result.Reason {
		return "", fmt.Errorf("stale host sensor coverage reason")
	}
	if reason == "" {
		return "COMPLETE", nil
	}
	return "INCOMPLETE", nil
}

func validIO(sample hostmon.Sample) bool {
	return validIOPair(sample.DiskCoverage, sample.DiskReadBytes, sample.DiskWriteBytes) &&
		validIOPair(sample.NetworkCoverage, sample.NetworkRxBytes, sample.NetworkTxBytes)
}

func validIOPair(coverage string, first, second *uint64) bool {
	known := (first != nil) == (second != nil)
	switch coverage {
	case "":
		return known
	case "COMPLETE":
		return known && first != nil
	case "INCOMPLETE":
		return known && first == nil
	default:
		return false
	}
}

func validSwap(sample hostmon.Sample) bool {
	if (sample.SwapTotalBytes == nil) != (sample.SwapUsedBytes == nil) {
		return false
	}
	return sample.SwapTotalBytes == nil || *sample.SwapUsedBytes <= *sample.SwapTotalBytes
}
