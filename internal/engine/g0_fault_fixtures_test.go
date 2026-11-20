package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func signalCodes(r report.Result) map[string]bool {
	codes := map[string]bool{}
	for _, s := range r.Signals {
		codes[s.Code] = true
	}
	return codes
}

// G0-05: the same constant-latency loopback server is driven with an
// identical open-arrival schedule twice. Only the client connection cap
// differs. The capped run must show client transport acquisition waits while
// the independent server ledger proves the server never queued and never ran
// more than one request at once; the uncapped run must show server-side
// concurrency without the acquisition signal. Neither run may report worker
// exhaustion, so the finding is distinct from server slowness and from
// generator saturation.
func TestIsolatedConnectionPoolExhaustionIsDistinctFromServerSlownessAndGeneratorSaturation(t *testing.T) {
	run := func(t *testing.T, connectionCap int) (report.Result, reference.Snapshot) {
		t.Helper()
		fixture, err := reference.New(reference.Config{Delay: 60 * time.Millisecond, MaxConcurrent: 50, MaxRequests: 64})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(fixture)
		defer server.Close()
		v, err := config.Parse(config.Scenario{
			Name: "isolated connection pool fixture", BaseURL: server.URL,
			Users: 32, Mode: "arrival", ArrivalRate: 25, Duration: "1s", MaxRPS: 100,
			Timeout: "3s", P95LimitMS: 5000, MaxErrorRate: 0, MinSamplesPerStep: 1,
			MaxConnectionsPerHost: connectionCap,
			Steps:                 []config.Step{{Name: "work", Method: http.MethodGet, Path: "/work"}},
		}, config.Options{Authorized: true})
		if err != nil {
			t.Fatal(err)
		}
		r := Run(context.Background(), v)
		return r, fixture.Snapshot()
	}

	capped, cappedLedger := run(t, 1)
	codes := signalCodes(capped)
	if !codes["CLIENT_CONNECTION_ACQUISITION_WAIT"] || capped.Steps[0].HTTPTrace.ConnectionWaitCount == 0 {
		t.Fatalf("capped pool did not produce client acquisition evidence: signals=%+v trace=%+v", capped.Signals, capped.Steps[0].HTTPTrace)
	}
	if cappedLedger.PeakConcurrent != 1 || cappedLedger.PeakQueued != 0 || cappedLedger.Rejected != 0 ||
		cappedLedger.Received != int64(capped.Metrics.Total) || cappedLedger.Completed != cappedLedger.Received {
		t.Fatalf("server ledger must show no server-side queue under the client cap: ledger=%+v total=%d", cappedLedger, capped.Metrics.Total)
	}
	if capped.ArrivalsDroppedBusy != 0 || codes["CLIENT_WORKERS_BUSY"] || codes["GENERATOR_CPU_PRESSURE"] {
		t.Fatalf("pool queue was conflated with generator saturation: dropped_busy=%d signals=%+v", capped.ArrivalsDroppedBusy, capped.Signals)
	}

	uncapped, uncappedLedger := run(t, 0)
	codes = signalCodes(uncapped)
	// Go's transport can occasionally hand a just-released idle connection
	// to a request whose own dial is still pending, so a rare >10 ms reused
	// acquisition is possible without a cap. The isolated cap must dominate.
	cappedWaits, uncappedWaits := capped.Steps[0].HTTPTrace.ConnectionWaitCount, uncapped.Steps[0].HTTPTrace.ConnectionWaitCount
	t.Logf("connection waits: capped=%d uncapped=%d", cappedWaits, uncappedWaits)
	if cappedWaits < capped.Metrics.Total/2 || uncappedWaits*4 > cappedWaits {
		t.Fatalf("acquisition waits did not isolate the client cap: capped=%d/%d uncapped=%d/%d", cappedWaits, capped.Metrics.Total, uncappedWaits, uncapped.Metrics.Total)
	}
	if uncappedLedger.PeakConcurrent < 2 || uncappedLedger.Received != int64(uncapped.Metrics.Total) ||
		uncappedLedger.Completed != uncappedLedger.Received || uncapped.ArrivalsDroppedBusy != 0 {
		t.Fatalf("uncapped control did not overlap server work: ledger=%+v run_total=%d", uncappedLedger, uncapped.Metrics.Total)
	}
	// Server service time is identical; the capped client latency includes
	// the local pool queue that the server never observed.
	if capped.Metrics.P95MS <= uncapped.Metrics.P95MS {
		t.Fatalf("client-side queue did not raise client latency: capped p95=%.1f uncapped p95=%.1f", capped.Metrics.P95MS, uncapped.Metrics.P95MS)
	}
}

// G0-09: repeated bounded runs must not accumulate goroutines or retained
// heap. This is an aggregate screen across the run lifecycle (scheduler,
// workers, transport, generator monitor), not proof of leak freedom.
func TestRepeatedBoundedRunsDoNotAccumulateGoroutinesOrHeap(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: 2 * time.Millisecond, ResponseBytes: 32 << 10, MaxConcurrent: 32, MaxRequests: 10000})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "repeated bounded runs", BaseURL: server.URL,
		Users: 8, Mode: "arrival", ArrivalRate: 80, Duration: "1s", MaxRPS: 100,
		Timeout: "1s", P95LimitMS: 5000, MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "work", Method: http.MethodGet, Path: "/work"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 250 * time.Millisecond
	settle := func() (int, uint64) {
		var stats runtime.MemStats
		goroutines := 0
		for range 50 {
			server.CloseClientConnections()
			runtime.GC()
			runtime.GC()
			goroutines = runtime.NumGoroutine()
			if goroutines <= baselineGoroutines(t) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		runtime.ReadMemStats(&stats)
		return goroutines, stats.HeapAlloc
	}
	// Warm-up establishes lazily initialized runtime and library state.
	if r := Run(context.Background(), v); r.Metrics.Total == 0 {
		t.Fatalf("warm-up run produced no requests: %+v", r)
	}
	warmGoroutines, warmHeap := settle()
	const runs = 6
	var lastGoroutines int
	var lastHeap uint64
	for i := range runs {
		r := Run(context.Background(), v)
		if r.Metrics.Total == 0 || r.Metrics.Failures != 0 {
			t.Fatalf("run %d did not complete bounded work: %+v", i, r.Metrics)
		}
		lastGoroutines, lastHeap = settle()
	}
	if lastGoroutines > warmGoroutines+2 {
		t.Fatalf("goroutines grew across %d runs: warm=%d last=%d", runs, warmGoroutines, lastGoroutines)
	}
	const heapSlack = 8 << 20
	if lastHeap > warmHeap+heapSlack {
		t.Fatalf("retained heap grew across %d runs: warm=%d last=%d", runs, warmHeap, lastHeap)
	}
	t.Logf("warm_goroutines=%d last_goroutines=%d warm_heap=%d last_heap=%d", warmGoroutines, lastGoroutines, warmHeap, lastHeap)
}

var goroutineBaseline int

func baselineGoroutines(t *testing.T) int {
	t.Helper()
	if goroutineBaseline == 0 {
		goroutineBaseline = runtime.NumGoroutine()
	}
	return goroutineBaseline
}

// G0-15: external abort at further stages, reconciled with the independent
// server ledger.
func TestExternalAbortStageMatrixReconcilesWithIndependentReferenceLedger(t *testing.T) {
	t.Run("abort with one request processing and one queued at server", func(t *testing.T) {
		fixture, err := reference.New(reference.Config{Delay: 2 * time.Second, MaxConcurrent: 1, MaxRequests: 4})
		if err != nil {
			t.Fatal(err)
		}
		received := make(chan struct{}, 4)
		finished := make(chan struct{}, 4)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- struct{}{}
			defer func() { finished <- struct{}{} }()
			fixture.ServeHTTP(w, r)
		}))
		defer server.Close()
		v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/work"}})
		v.Scenario.Users = 2
		v.Duration = 3 * time.Second
		v.Timeout = 3 * time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-received
			<-received
			for fixture.Snapshot().PeakQueued < 1 {
				time.Sleep(2 * time.Millisecond)
			}
			cancel()
		}()
		run := Run(ctx, v)
		for range 2 {
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("reference handler did not finish after abort")
			}
		}
		ledger := fixture.Snapshot()
		if run.Metrics.Cancelled != 2 || run.Metrics.Success != 0 || run.Outcome != "ABORTED" {
			t.Fatalf("aborted processing+queued work not reported: %+v", run.Metrics)
		}
		if ledger.Received != 2 || ledger.Cancelled != 2 || ledger.Completed != 0 || ledger.Rejected != 0 || ledger.PeakQueued != 1 {
			t.Fatalf("server ledger did not reconcile processing+queued abort: %+v", ledger)
		}
	})

	t.Run("abort while step waits for client rate token", func(t *testing.T) {
		fixture, err := reference.New(reference.Config{Delay: 5 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 4})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(fixture)
		defer server.Close()
		v, err := config.Parse(config.Scenario{
			Name: "abort during rate wait", BaseURL: server.URL, Users: 1, Duration: "5s", RampUp: "0s",
			Timeout: "1s", MaxRPS: 1, P95LimitMS: 1000,
			Steps: []config.Step{{Name: "first", Method: http.MethodGet, Path: "/work"}, {Name: "second", Method: http.MethodGet, Path: "/work"}},
		}, config.Options{Authorized: true})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			for fixture.Snapshot().Completed < 1 {
				time.Sleep(2 * time.Millisecond)
			}
			// Well inside the 1 s token interval for the second step.
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		run := Run(ctx, v)
		ledger := fixture.Snapshot()
		if len(run.Steps) != 2 || run.Metrics.Success != 1 || run.Metrics.Cancelled != 1 ||
			run.Steps[1].HTTPTrace.RequestAttemptCount != 0 || run.Steps[1].HTTPTrace.RateWaitCancelledCount != 1 || run.Outcome != "ABORTED" {
			t.Fatalf("abort during rate-token wait not reported exactly: %+v", run)
		}
		if ledger.Received != 1 || ledger.Completed != 1 || ledger.Success != 1 || ledger.Cancelled != 0 || ledger.Rejected != 0 {
			t.Fatalf("undelivered step appeared in server ledger: %+v", ledger)
		}
	})

	t.Run("abort during drain with requests queued at server", func(t *testing.T) {
		fixture, err := reference.New(reference.Config{Delay: 2 * time.Second, MaxConcurrent: 1, MaxRequests: 10})
		if err != nil {
			t.Fatal(err)
		}
		received := make(chan struct{}, 10)
		finished := make(chan struct{}, 10)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- struct{}{}
			defer func() { finished <- struct{}{} }()
			fixture.ServeHTTP(w, r)
		}))
		defer server.Close()
		v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/work"}})
		v.Scenario.Users = 10
		v.Scenario.Mode = "arrival"
		v.Scenario.ArrivalRate = 10
		v.Scenario.MaxRPS = 30
		v.Duration = 300 * time.Millisecond
		v.Timeout = 3 * time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			for range 3 {
				<-received
			}
			// The 300 ms window has ended; the engine is draining one
			// processing and two server-queued requests.
			time.Sleep(200 * time.Millisecond)
			cancel()
		}()
		run := Run(ctx, v)
		for i := int64(0); i < run.IterationsStarted; i++ {
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatalf("reference handler %d did not finish after drain abort", i+1)
			}
		}
		ledger := fixture.Snapshot()
		if run.ArrivalsScheduled != 3 || run.IterationsStarted != 3 || run.Metrics.Cancelled != 3 || run.Outcome == "PASS" {
			t.Fatalf("drain abort with queued server work not reported: %+v", run)
		}
		if ledger.Received != 3 || ledger.Cancelled != 3 || ledger.Completed != 0 || ledger.PeakQueued != 2 ||
			ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
			t.Fatalf("server ledger did not reconcile drain abort with queued work: %+v", ledger)
		}
	})
}
