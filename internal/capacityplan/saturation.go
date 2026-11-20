package capacityplan

import (
	"fmt"
	"math"
	"sort"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

// Workload saturation signal thresholds (ADR 0172). They are fixed so that a
// report cannot be tuned after the fact.
const (
	SaturationMinLoadGain        = 0.10 // offered VUs must rise at least 10%
	SaturationMaxThroughputGain  = 0.05 // HTTP requests/s rise less than 5%
	SaturationMinLatencyRise     = 0.20 // observation p95 rises at least 20%
	SaturationSingleCoreFraction = 0.90 // busiest thread or process >= 0.9 cores
	SaturationQuotaFraction      = 0.90 // cgroup CPU >= 90% of its quota
	SaturationThrottledRatio     = 0.05 // >= 5% of CFS periods throttled
)

// Saturation assessment states. Every state describes a hypothesis; none
// states a root cause as fact.
const (
	SaturationSupported    = "HYPOTHESIS_SUPPORTED_BY_MEASURED_FACTS"
	SaturationNotSupported = "HYPOTHESIS_NOT_SUPPORTED_BY_MEASURED_FACTS"
	SaturationUnknown      = "UNKNOWN"
	SaturationNoPlateau    = "NO_PLATEAU_OBSERVED"
)

// SaturationLevel summarizes one tested load level from observation phases.
type SaturationLevel struct {
	Campaign              int      `json:"campaign"`
	Users                 int      `json:"users"`
	ObservationPhases     int      `json:"observation_phases"`
	MedianThroughputRPS   float64  `json:"median_http_requests_per_second"`
	MedianP95MS           float64  `json:"median_request_p95_ms"`
	AttributionSamples    int      `json:"workload_attribution_samples"`
	BusiestThreadMaxCores *float64 `json:"busiest_thread_max_cores,omitempty"`
	BusiestThreadComplete bool     `json:"busiest_thread_coverage_complete"`
	TopProcessMaxCores    *float64 `json:"top_process_max_cores,omitempty"`
	CgroupCPUMaxCores     *float64 `json:"cgroup_cpu_max_cores,omitempty"`
	CgroupQuotaCores      *float64 `json:"cgroup_cpu_quota_cores,omitempty"`
	CgroupThrottledRatio  *float64 `json:"cgroup_throttled_period_ratio,omitempty"`
	HostCPUMaxPercent     *float64 `json:"host_cpu_max_percent,omitempty"`
}

// SaturationAssessment is a labeled hypothesis, never a root-cause verdict.
type SaturationAssessment struct {
	Label      string            `json:"label"`
	State      string            `json:"state"`
	Hypotheses []string          `json:"hypotheses,omitempty"`
	Facts      []string          `json:"measured_facts,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Levels     []SaturationLevel `json:"levels"`
	Note       string            `json:"note"`
}

const saturationNote = "Hypothesis only. Throughput plateau with rising latency and near-single-core or quota-bound workload CPU is consistent with CPU saturation but does not prove it; locks, I/O waits, dependencies, or client limits can produce the same pattern."

// AssessWorkloadSaturation compares consecutive tested levels for a throughput
// plateau with rising latency and, at the plateau level, checks measured
// workload attribution for single-core or cgroup-quota saturation.
func AssessWorkloadSaturation(campaigns []campaign.Result) SaturationAssessment {
	result := SaturationAssessment{Label: "HYPOTHESIS", Levels: []SaturationLevel{}, Note: saturationNote}
	for index, c := range campaigns {
		if level, ok := saturationLevel(index+1, c); ok {
			result.Levels = append(result.Levels, level)
		}
	}
	sort.SliceStable(result.Levels, func(i, j int) bool { return result.Levels[i].Users < result.Levels[j].Users })
	var at *SaturationLevel
	for i := 1; i < len(result.Levels); i++ {
		low, high := &result.Levels[i-1], &result.Levels[i]
		if low.Users < 1 || low.MedianThroughputRPS <= 0 || low.MedianP95MS <= 0 {
			continue
		}
		loadGain := float64(high.Users)/float64(low.Users) - 1
		throughputGain := high.MedianThroughputRPS/low.MedianThroughputRPS - 1
		latencyRise := high.MedianP95MS/low.MedianP95MS - 1
		if loadGain >= SaturationMinLoadGain && throughputGain < SaturationMaxThroughputGain && latencyRise >= SaturationMinLatencyRise {
			at = high
			result.Facts = append(result.Facts, fmt.Sprintf("From %d to %d VUs (+%.1f%%), median HTTP requests/s changed %+.1f%% (%.2f to %.2f) while median request p95 rose %+.1f%% (%.2f to %.2f ms).", low.Users, high.Users, loadGain*100, throughputGain*100, low.MedianThroughputRPS, high.MedianThroughputRPS, latencyRise*100, low.MedianP95MS, high.MedianP95MS))
			break
		}
	}
	if len(result.Levels) < 2 {
		result.State, result.Reason = SaturationUnknown, "fewer than two tested load levels have observation phases"
		return result
	}
	if at == nil {
		result.State, result.Reason = SaturationNoPlateau, "no consecutive tested levels show a throughput plateau with rising latency under the fixed thresholds"
		return result
	}
	if at.HostCPUMaxPercent != nil {
		result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, maximum host CPU across complete host samples was %.1f%%.", at.Users, *at.HostCPUMaxPercent))
	}
	if at.AttributionSamples == 0 {
		result.State, result.Reason = SaturationUnknown, fmt.Sprintf("a plateau was observed at %d VUs, but no complete workload process-tree or cgroup attribution was recorded there; whether the application saturated a single CPU core or its quota is unknown", at.Users)
		return result
	}
	supported := false
	if v := at.BusiestThreadMaxCores; v != nil {
		result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, the busiest measured workload thread used up to %.2f cores (thread coverage complete: %t).", at.Users, *v, at.BusiestThreadComplete))
		if *v >= SaturationSingleCoreFraction {
			supported = true
			result.Hypotheses = append(result.Hypotheses, "A single workload thread may have been CPU-bound near one core, limiting throughput.")
		}
	}
	if v := at.TopProcessMaxCores; v != nil {
		result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, the busiest measured workload process used up to %.2f cores.", at.Users, *v))
	}
	if v := at.CgroupCPUMaxCores; v != nil {
		if q := at.CgroupQuotaCores; q != nil && *q > 0 {
			result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, workload cgroup CPU reached %.2f of %.2f quota cores.", at.Users, *v, *q))
			if *v >= SaturationQuotaFraction**q {
				supported = true
				result.Hypotheses = append(result.Hypotheses, "The workload cgroup may have been bound by its CPU quota.")
			}
		} else {
			result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, workload cgroup CPU reached %.2f cores with no visible CPU quota.", at.Users, *v))
		}
	}
	if v := at.CgroupThrottledRatio; v != nil {
		result.Facts = append(result.Facts, fmt.Sprintf("At %d VUs, %.1f%% of workload cgroup CPU periods were throttled.", at.Users, *v*100))
		if *v >= SaturationThrottledRatio {
			supported = true
			result.Hypotheses = append(result.Hypotheses, "CFS throttling of the workload cgroup may have limited throughput.")
		}
	}
	switch {
	case supported:
		result.State = SaturationSupported
	case at.BusiestThreadMaxCores != nil && at.BusiestThreadComplete && at.CgroupCPUMaxCores != nil:
		result.State, result.Reason = SaturationNotSupported, "complete workload attribution at the plateau level shows no thread near one core, no cgroup near its quota, and no material throttling; the plateau cause remains unknown"
	default:
		result.State, result.Reason = SaturationUnknown, "workload attribution at the plateau level is partial; single-core or quota saturation can neither be supported nor rejected"
	}
	return result
}

func saturationLevel(index int, c campaign.Result) (SaturationLevel, bool) {
	level := SaturationLevel{Campaign: index, Users: c.BaseUsers}
	var throughput, p95 []float64
	var periods, throttled uint64
	throttleKnown := false
	threadSamples, threadComplete := 0, 0
	for _, trial := range c.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "observation" {
				continue
			}
			seconds := phase.Run.WindowSeconds()
			if seconds <= 0 {
				continue
			}
			throughput = append(throughput, float64(phase.Run.Metrics.Total)/seconds)
			p95 = append(p95, phase.Run.Metrics.P95MS)
			if phase.HostTelemetry == nil || phase.HostTelemetry.Coverage != "COMPLETE" {
				continue
			}
			for _, remote := range phase.HostTelemetry.Samples {
				for _, host := range remote.Reading.Host.Samples {
					level.HostCPUMaxPercent = maxPointer(level.HostCPUMaxPercent, host.CPUPercent)
					a := host.Attribution
					if a == nil || hostmon.ValidateAttribution(a) != nil {
						continue
					}
					used := false
					if a.BusiestThread != nil && (a.ThreadCoverage == hostmon.CoverageComplete || a.ThreadCoverage == hostmon.CoverageIncomplete) {
						level.BusiestThreadMaxCores = maxPointer(level.BusiestThreadMaxCores, a.BusiestThread.CPUCores)
						threadSamples++
						if a.ThreadCoverage == hostmon.CoverageComplete {
							threadComplete++
						}
						used = true
					}
					if len(a.TopProcesses) > 0 && a.TreeCoverage != hostmon.CoverageUnavailable {
						level.TopProcessMaxCores = maxPointer(level.TopProcessMaxCores, a.TopProcesses[0].CPUCores)
						used = true
					}
					if a.CgroupCoverage == hostmon.CoverageComplete && a.CgroupCPUCores != nil {
						level.CgroupCPUMaxCores = maxPointer(level.CgroupCPUMaxCores, *a.CgroupCPUCores)
						if a.CgroupQuotaCores != nil {
							level.CgroupQuotaCores = a.CgroupQuotaCores
						}
						if a.CgroupPeriodsDelta != nil {
							periods += *a.CgroupPeriodsDelta
							throttled += *a.CgroupThrottledPeriodsDelta
							throttleKnown = true
						}
						used = true
					}
					if used {
						level.AttributionSamples++
					}
				}
			}
		}
	}
	if len(throughput) == 0 {
		return SaturationLevel{}, false
	}
	if throttleKnown && periods > 0 {
		ratio := float64(throttled) / float64(periods)
		level.CgroupThrottledRatio = &ratio
	}
	level.BusiestThreadComplete = threadSamples > 0 && threadSamples == threadComplete
	level.ObservationPhases = len(throughput)
	level.MedianThroughputRPS, level.MedianP95MS = median(throughput), median(p95)
	return level, true
}

func maxPointer(current *float64, value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return current
	}
	if current == nil || value > *current {
		return &value
	}
	return current
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
