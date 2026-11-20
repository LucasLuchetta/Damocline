package capacityplan

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/hostsensor"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func saturationCampaign(users int, total int, p95 float64, attribution *hostmon.Attribution) campaign.Result {
	run := report.Result{ObservationSeconds: 10}
	run.Metrics.Total = total
	run.Metrics.P95MS = p95
	phase := campaign.Phase{Name: "observation", Run: run}
	if attribution != nil {
		phase.HostTelemetry = &hostsensor.Observation{Coverage: "COMPLETE", Samples: []hostsensor.RemoteSample{{Reading: hostsensor.Reading{SchemaVersion: 2, Host: hostmon.Result{Samples: []hostmon.Sample{{CPUPercent: 22, Attribution: attribution}}}}}}}
	}
	return campaign.Result{BaseUsers: users, Trials: []campaign.Trial{{Phases: []campaign.Phase{phase}}}}
}

func singleCoreAttribution(threadCores float64, threadCoverage string) *hostmon.Attribution {
	tree, cgroupCores, quota := 1.0, 1.0, 4.0
	usage, periods, throttled := uint64(1), uint64(100), uint64(0)
	return &hostmon.Attribution{
		SchemaVersion: 1, Scope: "process_tree_and_cgroup", IntervalMS: 1000,
		TreeCoverage: hostmon.CoverageComplete, TreeCPUCores: &tree, RootPID: 40, ProcessCount: 2,
		TopProcesses:   []hostmon.ProcessUsage{{PID: 41, CPUCores: threadCores, Comm: "app"}},
		ThreadCoverage: threadCoverage, ThreadsCompared: 4, BusiestThread: &hostmon.ThreadUsage{PID: 41, TID: 41, CPUCores: threadCores},
		CgroupCoverage: hostmon.CoverageComplete, CgroupCPUUsageUsec: &usage, CgroupCPUCores: &cgroupCores, CgroupQuotaCores: &quota,
		CgroupPeriodsDelta: &periods, CgroupThrottledPeriodsDelta: &throttled, CgroupThrottledUsecDelta: &throttled,
	}
}

func TestSaturationHypothesisSupportedBySingleBusyThreadAtPlateau(t *testing.T) {
	got := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, singleCoreAttribution(0.6, hostmon.CoverageComplete)),
		saturationCampaign(100, 5100, 220, singleCoreAttribution(0.97, hostmon.CoverageComplete)),
	})
	if got.Label != "HYPOTHESIS" || got.State != SaturationSupported || len(got.Hypotheses) != 1 || !strings.Contains(got.Hypotheses[0], "single workload thread") {
		t.Fatalf("unexpected assessment: %+v", got)
	}
	joined := strings.Join(got.Facts, " ")
	if !strings.Contains(joined, "0.97 cores") || !strings.Contains(joined, "host CPU") || !strings.Contains(got.Note, "does not prove") {
		t.Fatalf("facts or note missing measured evidence: %+v", got)
	}
}

func TestSaturationUnknownWithoutAttributionAndRejectedWhenCompleteAndIdle(t *testing.T) {
	unknown := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, nil),
		saturationCampaign(100, 5100, 220, nil),
	})
	if unknown.State != SaturationUnknown || !strings.Contains(unknown.Reason, "unknown") || len(unknown.Hypotheses) != 0 {
		t.Fatalf("absent attribution must be unknown: %+v", unknown)
	}
	rejected := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, singleCoreAttribution(0.3, hostmon.CoverageComplete)),
		saturationCampaign(100, 5100, 220, singleCoreAttribution(0.4, hostmon.CoverageComplete)),
	})
	if rejected.State != SaturationNotSupported || !strings.Contains(rejected.Reason, "cause remains unknown") {
		t.Fatalf("complete idle attribution must not support the hypothesis: %+v", rejected)
	}
	partial := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, singleCoreAttribution(0.3, hostmon.CoverageIncomplete)),
		saturationCampaign(100, 5100, 220, singleCoreAttribution(0.4, hostmon.CoverageIncomplete)),
	})
	if partial.State != SaturationUnknown {
		t.Fatalf("partial thread coverage cannot reject the hypothesis: %+v", partial)
	}
	scaling := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, nil),
		saturationCampaign(100, 9800, 110, nil),
	})
	if scaling.State != SaturationNoPlateau {
		t.Fatalf("scaling throughput reported as plateau: %+v", scaling)
	}
	if one := AssessWorkloadSaturation([]campaign.Result{saturationCampaign(50, 5000, 100, nil)}); one.State != SaturationUnknown {
		t.Fatalf("single level must be unknown: %+v", one)
	}
}

func TestSaturationQuotaAndThrottlingHypotheses(t *testing.T) {
	bound := singleCoreAttribution(0.5, hostmon.CoverageComplete)
	cores, quota, throttled := 1.95, 2.0, uint64(30)
	bound.CgroupCPUCores, bound.CgroupQuotaCores, bound.CgroupThrottledPeriodsDelta = &cores, &quota, &throttled
	got := AssessWorkloadSaturation([]campaign.Result{
		saturationCampaign(50, 5000, 100, nil),
		saturationCampaign(100, 5100, 220, bound),
	})
	if got.State != SaturationSupported || len(got.Hypotheses) != 2 {
		t.Fatalf("expected quota and throttling hypotheses: %+v", got)
	}
}
