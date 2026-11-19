//go:build integration

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/testresource"
)

const (
	resourceRuns       = 4 // One warm-up, then three measured repeats in the same child.
	resourceWorkers    = 50
	resourceArrivals   = 200
	resourceBodyBytes  = 2 << 20
	resourcePeakLimit  = 512 << 20
	resourceHeapGrowth = 16 << 20
	resourceChildEnv   = "DAMOCLINE_ENGINE_MEMORY_CHILD"
	resourceTargetEnv  = "DAMOCLINE_ENGINE_MEMORY_TARGET"
)

type engineMemoryRun struct {
	Scheduled int64  `json:"scheduled"`
	Admitted  int64  `json:"admitted"`
	Dropped   int64  `json:"dropped"`
	Requests  int64  `json:"requests"`
	BodyBytes int64  `json:"body_bytes"`
	HeapBytes uint64 `json:"retained_heap_bytes"`
}

type engineMemoryObservation struct {
	Runs          []engineMemoryRun `json:"runs"`
	PeakRSSBytes  uint64            `json:"process_peak_rss_bytes"`
	PeakRSSMethod string            `json:"peak_rss_method"`
}

type engineMemoryLedger struct {
	mu                         sync.Mutex
	received, completed, bytes int64
	active, maximumActive      int
	prefixes                   int
	release                    chan struct{}
	finished                   chan struct{}
}

func TestNativeEngineRepeatedMemoryWithSeparateServerLedger(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("process peak RSS is supported only on Windows and Linux")
	}
	if resultPath := os.Getenv(resourceChildEnv); resultPath != "" {
		observation := exerciseRepeatedEngineMemory(t, os.Getenv(resourceTargetEnv))
		data, err := json.Marshal(observation)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resultPath, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}

	// The HTTP fixture stays in the parent: its buffers and runtime costs never
	// enter the generator child's heap or process high-water observations.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	body := bytes.Repeat([]byte("x"), resourceBodyBytes)
	ledgers := make([]*engineMemoryLedger, resourceRuns)
	for i := range ledgers {
		ledgers[i] = &engineMemoryLedger{release: make(chan struct{}), finished: make(chan struct{}, resourceArrivals)}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var index int
		if _, err := fmt.Sscanf(r.URL.Path, "/batch/%d", &index); err != nil || index < 0 || index >= len(ledgers) {
			http.NotFound(w, r)
			return
		}
		ledger := ledgers[index]
		ledger.mu.Lock()
		if ledger.received >= resourceArrivals {
			ledger.mu.Unlock()
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ledger.received++
		ordinal := ledger.received
		ledger.active++
		if ledger.active > ledger.maximumActive {
			ledger.maximumActive = ledger.active
		}
		ledger.mu.Unlock()
		defer func() {
			ledger.mu.Lock()
			ledger.active--
			ledger.mu.Unlock()
			ledger.finished <- struct{}{}
		}()
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		n, err := w.Write(body[:len(body)-1])
		if err == nil {
			w.(http.Flusher).Flush()
		}
		if err == nil && ordinal <= resourceWorkers {
			// Hold the final byte until 50 response prefixes have been sent.
			// This exercises overlapping large reads without a server allocation
			// per response. It is not a measurement of live client buffer sizes.
			ledger.mu.Lock()
			ledger.prefixes++
			if ledger.prefixes == resourceWorkers {
				close(ledger.release)
			}
			ledger.mu.Unlock()
			select {
			case <-ledger.release:
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
		}
		if err == nil {
			var last int
			last, err = w.Write(body[len(body)-1:])
			n += last
		}
		ledger.mu.Lock()
		ledger.bytes += int64(n)
		if err == nil && n == len(body) {
			ledger.completed++
		}
		ledger.mu.Unlock()
	}))
	defer func() {
		cancel()
		server.CloseClientConnections()
		server.Close()
	}()
	resultPath := filepath.Join(t.TempDir(), "generator.json")
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeEngineRepeatedMemoryWithSeparateServerLedger$", "-test.v")
	scratch := t.TempDir()
	command.Env = append(os.Environ(), resourceChildEnv+"="+resultPath, resourceTargetEnv+"="+server.URL,
		"TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generator child failed: %v context=%v output=%s", err, ctx.Err(), output)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var observation engineMemoryObservation
	if err := json.Unmarshal(data, &observation); err != nil {
		t.Fatal(err)
	}
	if len(observation.Runs) != resourceRuns || observation.PeakRSSBytes == 0 ||
		observation.PeakRSSBytes > resourcePeakLimit || observation.PeakRSSMethod != testresource.PeakRSSMethod {
		t.Fatalf("invalid generator resource observation: %+v peak_limit_bytes=%d", observation, resourcePeakLimit)
	}
	for i, run := range observation.Runs {
		ledger := ledgers[i]
		for n := int64(0); n < run.Requests; n++ {
			select {
			case <-ledger.finished:
			case <-ctx.Done():
				t.Fatal("server did not finish admitted work")
			}
		}
		ledger.mu.Lock()
		received, completed, written, maximum, active := ledger.received, ledger.completed, ledger.bytes, ledger.maximumActive, ledger.active
		ledger.mu.Unlock()
		if run.Scheduled != resourceArrivals || run.Scheduled != run.Admitted+run.Dropped ||
			run.Admitted != run.Requests || run.Requests < resourceWorkers || run.Requests != received || received != completed ||
			run.BodyBytes != written || written != received*resourceBodyBytes || maximum < resourceWorkers || active != 0 {
			t.Fatalf("run %d ledger mismatch: client=%+v server_received=%d completed=%d bytes=%d maximum_active=%d active=%d",
				i, run, received, completed, written, maximum, active)
		}
		if i > 0 && run.HeapBytes > observation.Runs[0].HeapBytes+resourceHeapGrowth {
			t.Fatalf("run %d retained heap exceeded warm-up by more than %d bytes: %+v", i, resourceHeapGrowth, observation)
		}
		t.Logf("run=%d scheduled=%d admitted=%d dropped=%d server_requests=%d server_bytes=%d maximum_server_handlers=%d retained_heap_bytes=%d",
			i, run.Scheduled, run.Admitted, run.Dropped, received, written, maximum, run.HeapBytes)
	}
	t.Logf("generator_peak_rss_bytes=%d peak_limit_bytes=%d peak_rss_method=%s retained_growth_limit_bytes=%d observation=%s",
		observation.PeakRSSBytes, resourcePeakLimit, observation.PeakRSSMethod, resourceHeapGrowth, data)
}

func exerciseRepeatedEngineMemory(t *testing.T, target string) engineMemoryObservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var observation engineMemoryObservation
	for i := 0; i < resourceRuns; i++ {
		run := runEngineMemoryBatch(t, ctx, target, i)
		// Two collections discard sync.Pool victim caches. These are retained
		// Go heap observations after each run, not peak or leak-freedom proof.
		runtime.GC()
		runtime.GC()
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		run.HeapBytes = memory.HeapAlloc
		observation.Runs = append(observation.Runs, run)
	}
	peak, err := testresource.PeakRSS()
	if err != nil || peak == 0 {
		t.Fatalf("generator peak RSS unavailable: bytes=%d error=%v", peak, err)
	}
	observation.PeakRSSBytes, observation.PeakRSSMethod = peak, testresource.PeakRSSMethod
	return observation
}

func runEngineMemoryBatch(t *testing.T, ctx context.Context, target string, index int) engineMemoryRun {
	t.Helper()
	validated, err := config.Parse(config.Scenario{
		Name: "bounded engine memory fixture", BaseURL: target,
		Users: resourceWorkers, Mode: "arrival", ArrivalRate: 100, Duration: "2s", MaxRPS: 100,
		Timeout: "3s", P95LimitMS: 5000, MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "large", Method: http.MethodGet, Path: fmt.Sprintf("/batch/%d", index), ExpectedStatus: []int{200}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	run := Run(ctx, validated)
	if run.ArrivalsAdmitted == nil || run.ArrivalsCancelledBeforeAdmission == nil ||
		*run.ArrivalsCancelledBeforeAdmission != 0 || run.ArrivalsDropped != run.ArrivalsDroppedBusy+run.ArrivalsDroppedLate ||
		*run.ArrivalsAdmitted != run.IterationsStarted || run.IterationsStarted != run.IterationsCompleted ||
		run.IterationsCompleted != int64(run.Metrics.Success) || run.Metrics.Total != run.Metrics.Success ||
		run.Metrics.Failures != 0 || run.Metrics.Cancelled != 0 || run.Journeys.Cancelled != 0 {
		t.Fatalf("run %d failed delivery conservation: %+v", index, run)
	}
	body := run.Metrics.ResponseBody
	if body.CompleteObservations != int64(run.Metrics.Success) || body.IncompleteObservations != 0 || body.OversizedObservations != 0 ||
		body.MinBytes != resourceBodyBytes || body.MaxBytes != resourceBodyBytes || body.TotalBytes != int64(run.Metrics.Success)*resourceBodyBytes {
		t.Fatalf("run %d lost response bytes: %+v", index, body)
	}
	if run.Quality == nil || run.Quality.State != report.QualityLimited || report.AssessQuality(run).State != report.QualityLimited {
		t.Fatalf("memory fixture must retain LIMITED quality: %+v", run.Quality)
	}
	return engineMemoryRun{Scheduled: run.ArrivalsScheduled, Admitted: *run.ArrivalsAdmitted, Dropped: run.ArrivalsDropped,
		Requests: int64(run.Metrics.Success), BodyBytes: body.TotalBytes}
}
