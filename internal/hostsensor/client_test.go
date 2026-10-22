package hostsensor

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

func TestCollectRemoteChecksIdentityAndClockBound(t *testing.T) {
	server := httptest.NewServer(testHandler(t, fakeCollect))
	defer server.Close()
	sample, err := CollectRemote(context.Background(), server.URL+Path, testToken, testExperiment)
	if err != nil || sample.Reading.ExperimentID != testExperiment || sample.RoundTripMS < 0 || sample.OffsetMaxMS < sample.OffsetMinMS || sample.OffsetMaxMS-sample.OffsetMinMS-sample.RoundTripMS > 1 {
		t.Fatalf("invalid remote sample: %+v err=%v", sample, err)
	}
	if err := ValidateEndpoint("http://app.example.com"+Path, testToken); err == nil {
		t.Fatal("plaintext remote sensor endpoint accepted")
	}
}

func TestObserveWhileReportsSensorOutage(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(testHandler(t, func(context.Context) (hostmon.Result, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return hostmon.Result{}, context.DeadlineExceeded
	}))
	defer server.Close()
	result := ObserveWhile(context.Background(), server.URL+Path, testToken, testExperiment, time.Second, func() {
		<-called
		time.Sleep(50 * time.Millisecond)
	})
	state, err := ReassessCoverage(result)
	if err != nil || state != "INCOMPLETE" || result.Coverage != "INCOMPLETE" || len(result.Failures) == 0 {
		t.Fatalf("sensor outage was hidden: %+v state=%s err=%v", result, state, err)
	}
}

func TestSwapEvidenceDistinguishesUnknownFromMeasuredZero(t *testing.T) {
	zero := uint64(0)
	used := uint64(1)
	if !validSwap(hostmon.Sample{}) || !validSwap(hostmon.Sample{SwapTotalBytes: &zero, SwapUsedBytes: &zero}) {
		t.Fatal("unknown or measured zero swap was rejected")
	}
	if validSwap(hostmon.Sample{SwapTotalBytes: &zero}) || validSwap(hostmon.Sample{SwapTotalBytes: &zero, SwapUsedBytes: &used}) {
		t.Fatal("partial or impossible swap evidence was accepted")
	}
}

func TestIOEvidenceRequiresConsistentCoverage(t *testing.T) {
	zero := uint64(0)
	complete := hostmon.Sample{DiskCoverage: "COMPLETE", NetworkCoverage: "COMPLETE", DiskReadBytes: &zero, DiskWriteBytes: &zero, NetworkRxBytes: &zero, NetworkTxBytes: &zero}
	if !validIO(complete) {
		t.Fatal("complete measured zero I/O should be valid")
	}
	if validIO(hostmon.Sample{DiskCoverage: "COMPLETE"}) {
		t.Fatal("complete I/O without counters should be invalid")
	}
	if validIO(hostmon.Sample{DiskCoverage: "INCOMPLETE", DiskReadBytes: &zero}) {
		t.Fatal("partial I/O counters should be invalid")
	}
	if !validIO(hostmon.Sample{DiskCoverage: "INCOMPLETE", NetworkCoverage: "INCOMPLETE"}) {
		t.Fatal("incomplete I/O with unknown deltas should be valid")
	}
	partial := hostmon.Sample{DiskCoverage: "INCOMPLETE", NetworkCoverage: "COMPLETE", NetworkRxBytes: &zero, NetworkTxBytes: &zero}
	if !validIO(partial) {
		t.Fatal("disk and network coverage should be independent")
	}
	if validIO(hostmon.Sample{DiskCoverage: "BROKEN"}) {
		t.Fatal("unknown I/O coverage should be invalid")
	}
}

func TestReassessValidatesProcessCPUFormula(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	percent, intervalMS := 100.0, 1000.0
	delta, total, clock, rss, memory, swap := uint64(100), uint64(200), uint64(100), uint64(4096), uint64(100), uint64(0)
	workload := &hostmon.Workload{PID: 7, StartTimeTicks: 10, ProcessRSSBytes: &rss, ProcessCPUTimeTicks: &total, ProcessCPUDeltaTicks: &delta, ProcessCPUPercent: &percent, ProcessCPUIntervalMS: &intervalMS, ProcessCPUClockTicksPerSecond: clock, ProcessCPUCoverage: "COMPLETE"}
	host := hostmon.Sample{At: start.Add(time.Second - 2*time.Millisecond).Format(time.RFC3339Nano), CPUPercent: 1, MemoryTotalBytes: 1000, MemoryUsedBytes: memory, SwapUsedBytes: &swap, SwapTotalBytes: &swap, Workload: workload}
	remote := RemoteSample{SentAt: start.Add(time.Second - 2*time.Millisecond).Format(time.RFC3339Nano), ReceivedAt: start.Add(time.Second).Format(time.RFC3339Nano), OffsetMinMS: -1, OffsetMaxMS: 1, RoundTripMS: 2, Reading: Reading{ExperimentID: testExperiment, SchemaVersion: 1, ObservedAt: start.Add(time.Second - time.Millisecond).Format(time.RFC3339Nano), Host: hostmon.Result{SchemaVersion: 1, Source: "linux:/proc", StartedAt: start.Format(time.RFC3339Nano), Interval: "1s", Samples: []hostmon.Sample{host}}}}
	second := remote
	second.SentAt = start.Add(2*time.Second - 2*time.Millisecond).Format(time.RFC3339Nano)
	second.ReceivedAt = start.Add(2 * time.Second).Format(time.RFC3339Nano)
	second.Reading.ObservedAt = start.Add(2*time.Second - time.Millisecond).Format(time.RFC3339Nano)
	second.Reading.Host.Samples = append([]hostmon.Sample(nil), remote.Reading.Host.Samples...)
	observation := Observation{ExperimentID: testExperiment, StartedAt: start.Format(time.RFC3339Nano), EndedAt: end.Format(time.RFC3339Nano), Interval: "1s", Attempts: 2, Samples: []RemoteSample{remote, second}, Coverage: "COMPLETE"}
	if state, err := ReassessCoverage(observation); err != nil || state != "COMPLETE" {
		t.Fatalf("valid process CPU evidence rejected: state=%s err=%v", state, err)
	}
	workload.ProcessCPUPercent = ptrFloat(99)
	if _, err := ReassessCoverage(observation); err == nil {
		t.Fatal("altered process CPU percentage was accepted")
	}
}

func ptrFloat(value float64) *float64 { return &value }
