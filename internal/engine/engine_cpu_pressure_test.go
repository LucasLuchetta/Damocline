//go:build integration

package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

var boundedCPUWorkSink atomic.Uint64

type cpuPressureObservation struct {
	Scheduled        int64   `json:"scheduled"`
	Completed        int64   `json:"completed"`
	ServerRequests   int64   `json:"server_requests"`
	CapacityCores    float64 `json:"capacity_cores"`
	MeanProcessCPU   float64 `json:"mean_process_cpu_ratio"`
	PeakProcessCPU   float64 `json:"peak_process_cpu_ratio"`
	GoCPU            float64 `json:"go_cpu_ratio"`
	GoCPUValid       bool    `json:"go_cpu_valid"`
	SchedulerP95MS   float64 `json:"scheduler_p95_ms"`
	SchedulerMaxMS   float64 `json:"scheduler_max_ms"`
	GeneratorSamples int     `json:"generator_samples"`
}

func TestNativeEngineCPUWorkProducesClientPressureSignal(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OS process CPU observations are supported only on Windows and Linux")
	}
	const childResultEnv = "DAMOCLINE_ENGINE_CPU_CHILD_RESULT"
	if resultPath := os.Getenv(childResultEnv); resultPath != "" {
		if runtime.GOMAXPROCS(0) != 1 {
			t.Fatal("CPU pressure fixture requires GOMAXPROCS=1 in its child")
		}
		observation := exerciseNativeEngineCPUWork(t)
		data, err := json.Marshal(observation)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resultPath, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	resultPath := filepath.Join(t.TempDir(), "child-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeEngineCPUWorkProducesClientPressureSignal$", "-test.v")
	scratch := t.TempDir()
	command.Env = append(os.Environ(), childResultEnv+"="+resultPath, "GOMAXPROCS=1", "TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated CPU pressure child failed: %v context=%v output=%s", err, ctx.Err(), output)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var observation cpuPressureObservation
	if err := json.Unmarshal(data, &observation); err != nil {
		t.Fatal(err)
	}
	if observation.Scheduled != 20 || observation.Completed != 20 || observation.ServerRequests != 20 ||
		observation.CapacityCores != 1 || observation.MeanProcessCPU < .9 || observation.GeneratorSamples < 2 {
		t.Fatalf("CPU fixture did not establish its declared workload and pressure: %+v", observation)
	}
	t.Logf("scheduled=%d completed=%d server_requests=%d capacity_cores=%.1f mean_process_cpu_ratio=%.6f peak_process_cpu_ratio=%.6f go_cpu_ratio=%.6f go_cpu_valid=%t scheduler_p95_ms=%.3f scheduler_max_ms=%.3f generator_samples=%d",
		observation.Scheduled, observation.Completed, observation.ServerRequests, observation.CapacityCores,
		observation.MeanProcessCPU, observation.PeakProcessCPU, observation.GoCPU, observation.GoCPUValid,
		observation.SchedulerP95MS, observation.SchedulerMaxMS, observation.GeneratorSamples)
}

func exerciseNativeEngineCPUWork(t *testing.T) cpuPressureObservation {
	t.Helper()
	const expected = 20
	var received, completed atomic.Int64
	finished := make(chan struct{}, expected)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		if n, err := w.Write([]byte("ok")); err == nil && n == 2 {
			completed.Add(1)
		}
		finished <- struct{}{}
	}))
	defer server.Close()
	validated, err := config.Parse(config.Scenario{
		Name: "bounded generator CPU fixture", BaseURL: server.URL,
		Users: 10, Mode: "arrival", ArrivalRate: 5, Duration: "4s", MaxRPS: 100,
		Timeout: "1s", P95LimitMS: 1500, MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/work", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	workCtx, stopWork := context.WithTimeout(ctx, 6*time.Second)
	defer stopWork()
	ready, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		var value uint64 = 1
		defer func() { boundedCPUWorkSink.Store(value) }()
		close(ready)
		for workCtx.Err() == nil {
			for i := 0; i < 4096; i++ {
				value = value*1664525 + 1013904223
			}
		}
	}()
	<-ready
	run := Run(ctx, validated)
	select {
	case <-stopped:
		t.Fatal("controlled CPU work ended before the engine observation completed")
	default:
	}
	stopWork()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("controlled CPU work did not stop")
	}
	for count := 0; count < expected; count++ {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("independent server handler did not finish")
		}
	}
	if run.ArrivalsScheduled != expected || run.ArrivalsAdmitted == nil || *run.ArrivalsAdmitted != expected ||
		run.ArrivalsDropped != 0 || run.IterationsStarted != expected || run.IterationsCompleted != expected ||
		run.Metrics.Total != expected || run.Metrics.Success != expected || run.Metrics.Cancelled != 0 ||
		received.Load() != expected || completed.Load() != expected || run.SchedulerLagSamples != expected {
		t.Fatalf("CPU fixture lost delivery: scheduled=%d admitted=%v dropped=%d started=%d completed=%d requests=%d success=%d server=%d completed_server=%d lag_samples=%d",
			run.ArrivalsScheduled, run.ArrivalsAdmitted, run.ArrivalsDropped, run.IterationsStarted,
			run.IterationsCompleted, run.Metrics.Total, run.Metrics.Success, received.Load(), completed.Load(), run.SchedulerLagSamples)
	}
	health := run.Generator
	if health == nil || !health.CPUCapacityKnown || health.CPUCapacityCores != 1 || health.GOMAXPROCS != 1 ||
		!health.ProcessCPUValid || health.MeanProcessCPUUtilizationRatio < .9 {
		t.Fatalf("measured sustained CPU pressure was not established: %+v", health)
	}
	found := false
	for _, signal := range run.Signals {
		if signal.Code == "GENERATOR_CPU_PRESSURE" {
			found = true
			if signal.Origin != "client" || !strings.Contains(signal.Evidence, "not proof of a target bottleneck") {
				t.Fatalf("CPU evidence was attributed outside the generator: %+v", signal)
			}
		}
		if signal.Code == "CLIENT_RATE_CEILING_NEAR" || signal.Code == "HTTP_ERROR_RESPONSE" || signal.Origin != "client" && signal.Origin != "client_transport" {
			t.Fatalf("CPU fixture emitted an unrelated or target-attributed signal: %+v", signal)
		}
	}
	quality := report.AssessQuality(run)
	if !found || run.Quality == nil || run.Quality.State != report.QualityLimited || quality.State != report.QualityLimited ||
		!strings.Contains(strings.Join(quality.Findings, " "), "Mean generator process CPU reached at least 90%") {
		t.Fatalf("CPU pressure did not retain a client-only diagnostic and LIMITED headroom: signals=%+v quality=%+v", run.Signals, quality)
	}
	return cpuPressureObservation{
		Scheduled: run.ArrivalsScheduled, Completed: run.IterationsCompleted, ServerRequests: received.Load(),
		CapacityCores: health.CPUCapacityCores, MeanProcessCPU: health.MeanProcessCPUUtilizationRatio,
		PeakProcessCPU: health.PeakProcessCPUUtilizationRatio, GoCPU: health.GoCPUUtilizationRatio, GoCPUValid: health.GoCPUValid,
		SchedulerP95MS: run.SchedulerLagP95MS, SchedulerMaxMS: run.SchedulerLagMaxMS, GeneratorSamples: health.Samples,
	}
}
