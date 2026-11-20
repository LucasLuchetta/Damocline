package engine

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// Fixture tolerances for the timing-reference-under-host-pressure fixture
// (ADR 0145). They are CI screening limits for one loopback fixture, not
// validated timing accuracy claims for any host class.
const (
	timingReferenceArrivalRate = 10
	timingReferenceDuration    = 3 * time.Second
	timingReferenceArrivals    = 30
	timingReferenceServerDelay = 20 * time.Millisecond
	// The server hold is observed inside the client request window, so the
	// client first-byte time may not be shorter than it beyond clock
	// resolution.
	timingReferenceClockSlackMS = 2.0
	// Mean client first-byte excess over the independent server hold.
	timingReferencePhaseExcessToleranceMS = 100.0
	// Server-observed arrival lateness relative to the first arrival may not
	// exceed engine-reported dispatch lag by more than this transport slack.
	timingReferenceLatenessSlackMS = 100.0
	// Hard deadline for in-process CPU pressure, independent of the run.
	timingReferencePressureDeadline = 6 * time.Second
)

var timingReferencePressureSink atomic.Uint64

type timingReferenceServer struct {
	mu      sync.Mutex
	origin  time.Time
	arrived []time.Duration
	holds   []time.Duration
}

func (s *timingReferenceServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	entered := time.Now()
	time.Sleep(timingReferenceServerDelay)
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	hold := time.Since(entered)
	_, _ = w.Write([]byte("ok"))
	s.mu.Lock()
	s.arrived = append(s.arrived, entered.Sub(s.origin))
	s.holds = append(s.holds, hold)
	s.mu.Unlock()
}

// startBoundedCPUPressure saturates every Go processor with busy goroutines
// that stop at the earlier of stop() or a hard deadline.
func startBoundedCPUPressure(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timingReferencePressureDeadline)
	workers := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			value := seed
			for ctx.Err() == nil {
				for j := 0; j < 4096; j++ {
					value = value*6364136223846793005 + 1442695040888963407
				}
			}
			timingReferencePressureSink.Add(value)
		}(uint64(i + 1))
	}
	return func() {
		cancel()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("bounded CPU pressure did not stop")
		}
	}
}

func TestTimingReferenceBaselineWithoutHostPressure(t *testing.T) {
	exerciseTimingReference(t, false)
}

func TestTimingReferenceUnderBoundedCPUPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("CPU pressure fixture skipped in -short mode")
	}
	exerciseTimingReference(t, true)
}

func exerciseTimingReference(t *testing.T, pressure bool) {
	t.Helper()
	reference := &timingReferenceServer{origin: time.Now()}
	server := httptest.NewServer(reference)
	defer server.Close()
	validated, err := config.Parse(config.Scenario{
		Name: "timing reference fixture", BaseURL: server.URL,
		Users: 10, Mode: "arrival", ArrivalRate: timingReferenceArrivalRate,
		Duration: timingReferenceDuration.String(), MaxRPS: 100,
		Timeout: "2s", P95LimitMS: 1500, MaxErrorRate: 0, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "delayed", Method: http.MethodGet, Path: "/delay", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stop := func() {}
	if pressure {
		stop = startBoundedCPUPressure(t)
	}
	run := Run(ctx, validated)
	stop()

	reference.mu.Lock()
	arrived := append([]time.Duration(nil), reference.arrived...)
	holds := append([]time.Duration(nil), reference.holds...)
	reference.mu.Unlock()

	// Arrival ledger: every planned arrival is either admitted or dropped,
	// admitted arrivals start, and every start reaches the independent server.
	admitted := int64(-1)
	if run.ArrivalsAdmitted != nil {
		admitted = *run.ArrivalsAdmitted
	}
	if run.ArrivalsScheduled != timingReferenceArrivals || admitted+run.ArrivalsDropped != run.ArrivalsScheduled ||
		run.IterationsStarted != admitted || run.SchedulerLagSamples != run.ArrivalsScheduled ||
		int64(run.Metrics.Total) != run.IterationsStarted || len(arrived) != run.Metrics.Total {
		t.Fatalf("arrival ledger does not reconcile: scheduled=%d admitted=%d dropped=%d started=%d requests=%d server=%d lag_samples=%d",
			run.ArrivalsScheduled, admitted, run.ArrivalsDropped, run.IterationsStarted, run.Metrics.Total, len(arrived), run.SchedulerLagSamples)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(run.Steps))
	}
	trace := run.Steps[0].HTTPTrace
	if trace.FirstByteCount != len(holds) || len(holds) == 0 {
		t.Fatalf("first-byte phase count %d does not match server hold count %d", trace.FirstByteCount, len(holds))
	}

	var serverHoldSumMS float64
	for _, hold := range holds {
		serverHoldSumMS += ms(hold)
		if hold < timingReferenceServerDelay {
			t.Fatalf("server hold %s shorter than injected delay", hold)
		}
	}
	serverHoldMeanMS := serverHoldSumMS / float64(len(holds))
	clientFirstByteMeanMS := trace.FirstByteMS / float64(trace.FirstByteCount)
	phaseExcessMS := clientFirstByteMeanMS - serverHoldMeanMS
	// Hard invariant: the client cannot observe first byte before the server
	// finished holding the response.
	if phaseExcessMS < -timingReferenceClockSlackMS {
		t.Fatalf("client first-byte mean %.3f ms is below independent server hold mean %.3f ms", clientFirstByteMeanMS, serverHoldMeanMS)
	}

	// Independent arrival reference anchored at the first server-observed
	// arrival; planned offsets are i/rate.
	sort.Slice(arrived, func(i, j int) bool { return arrived[i] < arrived[j] })
	period := time.Second / timingReferenceArrivalRate
	maxLatenessMS := math.Inf(-1)
	for i, at := range arrived {
		lateness := ms(at-arrived[0]) - ms(time.Duration(i)*period)
		if lateness > maxLatenessMS {
			maxLatenessMS = lateness
		}
	}
	if run.ArrivalsDropped == 0 && maxLatenessMS < -timingReferenceLatenessSlackMS {
		t.Fatalf("server observed arrivals %.3f ms early relative to plan", -maxLatenessMS)
	}

	withinTolerance := run.ArrivalsDropped == 0 &&
		phaseExcessMS <= timingReferencePhaseExcessToleranceMS &&
		maxLatenessMS <= run.SchedulerLagMaxMS+timingReferenceLatenessSlackMS &&
		run.SchedulerLagP95MS <= 50 && run.SchedulerLagMaxMS < 100
	quality := report.AssessQuality(run)
	t.Logf("pressure=%t scheduled=%d dropped=%d lag_p95_ms=%.3f lag_max_ms=%.3f server_hold_mean_ms=%.3f client_first_byte_mean_ms=%.3f phase_excess_ms=%.3f reference_max_lateness_ms=%.3f within_tolerance=%t quality=%s",
		pressure, run.ArrivalsScheduled, run.ArrivalsDropped, run.SchedulerLagP95MS, run.SchedulerLagMaxMS,
		serverHoldMeanMS, clientFirstByteMeanMS, phaseExcessMS, maxLatenessMS, withinTolerance, quality.State)
	clientDiagnostic := false
	for _, signal := range run.Signals {
		if signal.Code != "BOTTLENECK_NOT_ESTABLISHED" && signal.Origin != "client" && signal.Origin != "client_transport" {
			t.Fatalf("host pressure was attributed outside the generator: %+v", signal)
		}
		switch signal.Code {
		case "CLIENT_SCHEDULER_LATE", "GENERATOR_CPU_PRESSURE", "CLIENT_CONNECTION_ACQUISITION_WAIT":
			clientDiagnostic = true
		}
	}
	if !withinTolerance && (quality.State == report.QualityValid || !clientDiagnostic) {
		t.Fatalf("timing exceeded fixture tolerances without degraded classification: quality=%s findings=%q signals=%+v", quality.State, quality.Findings, run.Signals)
	}
	if run.ArrivalsDropped > 0 && !strings.Contains(strings.Join(quality.Findings, " "), "not completely delivered") {
		t.Fatalf("dropped arrivals were not reported as undelivered demand: %q", quality.Findings)
	}
	if !pressure && !withinTolerance {
		t.Logf("baseline exceeded fixture tolerances on this host; classification check above still applied")
	}
}
