package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func scenario(t *testing.T, url string, steps []config.Step) config.Validated {
	t.Helper()
	raw := config.Scenario{Name: "local test", BaseURL: url, Users: 2, Duration: "900ms", MaxRPS: 20, P95LimitMS: 2000, MaxErrorRate: 0, RampUp: "100ms", Timeout: "300ms", Steps: steps}
	// For fast unit tests, apply the duration limit at the config layer by testing
	// a short execution interval through direct construction below.
	raw.Duration = "1s"
	validated, err := config.Parse(raw, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	validated.Duration = 300 * time.Millisecond
	return validated
}

func TestMillisecondsConversionRetainsSubMicrosecondDurations(t *testing.T) {
	for _, duration := range []time.Duration{1 * time.Nanosecond, 999 * time.Nanosecond} {
		if got := ms(duration); got <= 0 {
			t.Fatalf("positive duration %s was truncated to %.9f ms", duration, got)
		}
	}
	if got, want := ms(250*time.Microsecond), 0.25; got != want {
		t.Fatalf("milliseconds conversion got %.9f, want %.9f", got, want)
	}
}

func TestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("expected page content"))
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/page", MustContain: "expected page"}})
	v.Window = v.Duration
	result := Run(context.Background(), v)
	if result.Metrics.Success < 1 || result.Metrics.Failures != 0 || result.Outcome != "PASS" {
		t.Fatalf("unexpected report: %+v", result)
	}
	if result.SchemaVersion != report.CurrentSchemaVersion || result.Config.WorkloadFingerprint == "" || result.ObservationSeconds != v.Duration.Seconds() || result.Generator == nil || result.Generator.ProcessSamples < 2 || result.Generator.SamplingIntervalMS != 250 || result.Generator.MaxSampleGapMS <= 0 || result.Generator.SampleCollectionTotalMS < result.Generator.SampleCollectionMaxMS {
		t.Fatalf("report must include workload fingerprint, observation window and generator process observations: %+v", result)
	}
	if len(result.Windows) == 0 || result.Windows[0].Journeys.Total == 0 || result.Windows[0].Journeys.Success != result.Windows[0].Journeys.Total {
		t.Fatalf("observation window must retain completed journey evidence: %+v", result.Windows)
	}
}

func TestCompletedJourneyLatencyCanFailWhileRequestLatencyPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}})
	v.Duration = 450 * time.Millisecond
	v.Window = v.Duration
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 5
	v.Scenario.P95LimitMS = 100
	result := Run(context.Background(), v)
	if result.Metrics.P95MS >= v.Scenario.P95LimitMS || result.Journeys.P95MS <= v.Scenario.P95LimitMS || result.Outcome != "FAIL" {
		t.Fatalf("full-journey latency breach was hidden by request metrics: requests=%+v journeys=%+v outcome=%s warnings=%v", result.Metrics, result.Journeys, result.Outcome, result.Warnings)
	}
}

func TestHTTPTraceMeasuresLoopbackPhasesAndConnectionReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("trace body"))
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Duration = 180 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	if len(r.Steps) != 1 {
		t.Fatalf("expected one step: %+v", r.Steps)
	}
	trace := r.Steps[0].HTTPTrace
	if trace.CompleteCount != trace.RequestAttemptCount || trace.FirstByteCount == 0 || trace.BodyCount == 0 {
		t.Fatalf("expected first-byte, body and complete observations: %+v", trace)
	}
	if trace.FirstByteMS < 0 || trace.BodyMS < 0 || trace.CompleteMS < trace.FirstByteMS {
		t.Fatalf("invalid phase boundaries: %+v", trace)
	}
	if trace.ConnectCount < 1 || trace.ConnectCount >= trace.CompleteCount {
		t.Fatalf("expected initial connection plus reused connections, got %+v", trace)
	}
	if trace.WriteCount != trace.CompleteCount {
		t.Fatalf("request-write callbacks did not reconcile: %+v", trace)
	}
	if trace.RateWaitCount < trace.CompleteCount || trace.RateWaitMS < 0 {
		t.Fatalf("rate wait should be measured separately for each admitted step: %+v", trace)
	}
}

func TestHTTPTraceReconcilesFailedWriteAndTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	url := server.URL
	server.CloseClientConnections()
	server.Close()
	v := scenario(t, url, []config.Step{{Name: "gone", Path: "/"}})
	v.Duration = 400 * time.Millisecond
	v.Timeout = 50 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	if len(r.Steps) != 1 || r.Steps[0].Metrics.Failures == 0 {
		t.Fatalf("expected a recorded local transport failure: %+v", r.Steps)
	}
	step := r.Steps[0]
	if step.HTTPTrace.CompleteCount != step.HTTPTrace.RequestAttemptCount || step.HTTPTrace.RateWaitCount < step.HTTPTrace.CompleteCount || step.HTTPTrace.CompleteMS <= 0 {
		t.Fatalf("transport-failure phase counters did not reconcile: %+v", step.HTTPTrace)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("valid failed-request trace must not make accounting invalid: %+v", r.Quality)
	}
}

func TestHTTPTraceBodyTimeoutIsMarkedIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "stream", Path: "/"}})
	v.Duration = 400 * time.Millisecond
	v.Timeout = 50 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	if len(r.Steps) != 1 || r.Steps[0].HTTPTrace.FirstByteCount == 0 || r.Steps[0].HTTPTrace.BodyIncompleteCount == 0 || r.Steps[0].Metrics.Reasons["timeout"] == 0 {
		t.Fatalf("incomplete streamed body was not captured: %+v", r.Steps)
	}
}

func TestTruncatedBodyReconcilesIndependentRequestLedger(t *testing.T) {
	var received, handlerCompleted atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		defer handlerCompleted.Add(1)
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
	}))
	defer server.Close()

	v := scenario(t, server.URL, []config.Step{{Name: "truncated", Path: "/"}})
	v.Duration = 350 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	step := r.Steps[0]
	if r.Metrics.Total < 2 || received.Load() != int64(r.Metrics.Total) || handlerCompleted.Load() != received.Load() {
		t.Fatalf("client and independent server request ledgers disagree: client=%+v received=%d handler_completed=%d", r.Metrics, received.Load(), handlerCompleted.Load())
	}
	if r.Metrics.Failures != r.Metrics.Total || r.Metrics.Reasons["body_read"] != r.Metrics.Total ||
		r.Metrics.HTTP.TransportFailures != r.Metrics.Total || r.Metrics.HTTP.NetworkTimeouts != 0 ||
		r.Metrics.ResponseBody.IncompleteObservations != int64(r.Metrics.Total) ||
		step.HTTPTrace.BodyIncompleteCount != r.Metrics.Total || r.Outcome == "PASS" {
		t.Fatalf("truncated response bodies were not classified as incomplete failures: aggregate=%+v step=%+v", r.Metrics, step)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("consistent truncated-body evidence must not invalidate request accounting: quality=%+v trace=%+v metrics=%+v", r.Quality, step.HTTPTrace, r.Metrics)
	}
}

func TestJourneyTimingSeparatesRequestsAndObservedThinkTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	s := config.Scenario{Name: "known journey timing", BaseURL: server.URL, Users: 1, Duration: "1s", MaxRPS: 50, P95LimitMS: 1000, MaxErrorRate: 0, ThinkTime: "60ms", Steps: []config.Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 360 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Journeys.Total == 0 || r.Journeys.MeanMS <= r.Steps[0].Metrics.MeanMS || r.Steps[0].Metrics.MeanMS > 100 || r.Steps[1].Metrics.MeanMS > 100 {
		t.Fatalf("request and journey timing boundaries were not distinct: journey=%+v steps=%+v", r.Journeys, r.Steps)
	}
	if r.ThinkTimeObservations == 0 || r.ObservedThinkTimeMS < float64(r.ThinkTimeObservations)*50 || r.ObservedThinkTimeMS > float64(r.ThinkTimeObservations)*200 {
		t.Fatalf("observed think-time accounting outside local fixture tolerance: total=%.1fms observations=%d", r.ObservedThinkTimeMS, r.ThinkTimeObservations)
	}
	if len(r.Profiles) != 0 {
		t.Fatalf("legacy single-workflow reports should retain top-level think-time totals only: %+v", r.Profiles)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("well-formed journey timing must reconcile: %+v", r.Quality)
	}
}

func TestInterruptedMultiStepJourneyHasNoCompletedLatencySample(t *testing.T) {
	secondStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/second" {
			secondStarted <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	s := config.Scenario{Name: "interrupted journey", BaseURL: server.URL, Users: 1, Duration: "1s", MaxRPS: 50, P95LimitMS: 1000, MaxErrorRate: 0, ThinkTime: "40ms", Steps: []config.Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-secondStarted; cancel() }()
	r := Run(ctx, v)
	if r.Metrics.Success != 1 || r.Metrics.Cancelled != 1 || r.Journeys.Total != 0 || r.Journeys.Cancelled != 1 || r.Journeys.P95MS != 0 || r.Outcome != "ABORTED" {
		t.Fatalf("partial multi-step journey entered completed latency metrics: %+v", r)
	}
	if r.ThinkTimeObservations != 1 || r.ObservedThinkTimeMS < 20 {
		t.Fatalf("completed think interval before interruption was not retained: %.1fms/%d", r.ObservedThinkTimeMS, r.ThinkTimeObservations)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("interrupted journey timing must remain arithmetically valid: %+v", r.Quality)
	}
}

func TestHTTPDiagnosticsCountLocalFailureMatrix(t *testing.T) {
	seen := map[string]int{}
	var seenMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		seen[r.URL.Path]++
		seenMu.Unlock()
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/destination", http.StatusFound)
		case "/limited":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/server-error":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/assertion":
			_, _ = w.Write([]byte("other body"))
		case "/expected":
			w.WriteHeader(http.StatusAccepted)
		case "/timeout":
			time.Sleep(3 * time.Second)
			_, _ = w.Write([]byte("late"))
		case "/large":
			_, _ = w.Write(make([]byte, maxResponseBytes+1))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	s := config.Scenario{Name: "bounded local diagnostics", BaseURL: server.URL, Users: 1, Duration: "1s", WindowDuration: "1s", MaxRPS: 100, MaxErrorRate: 1, Mode: "arrival", ArrivalRate: 1, P95LimitMS: 5000, Timeout: "2s", Steps: []config.Step{
		{Name: "redirect", Path: "/redirect"}, {Name: "limited", Path: "/limited"}, {Name: "server error", Path: "/server-error"}, {Name: "assertion", Path: "/assertion", MustContain: "expected"}, {Name: "expected status", Path: "/expected", ExpectedStatus: []int{http.StatusNoContent}}, {Name: "large", Path: "/large"}, {Name: "timeout", Path: "/timeout"},
	}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = time.Second
	r := Run(context.Background(), v)
	seenMu.Lock()
	ledger := map[string]int{}
	for path, count := range seen {
		ledger[path] = count
	}
	seenMu.Unlock()
	if len(r.Steps) != 7 {
		t.Fatalf("expected seven named steps: %+v", r.Steps)
	}
	for _, step := range r.Steps {
		if step.Metrics.Total == 0 {
			t.Fatalf("fixture step did not execute: %+v", step)
		}
	}
	checks := []struct {
		name      string
		got, want int
	}{
		{"302 status", r.Metrics.HTTP.StatusCodes[http.StatusFound], ledger["/redirect"]},
		{"redirect response", r.Metrics.HTTP.RedirectResponses, ledger["/redirect"]},
		{"429 status", r.Metrics.HTTP.StatusCodes[http.StatusTooManyRequests], ledger["/limited"]},
		{"rate-limited response", r.Metrics.HTTP.RateLimitedResponses, ledger["/limited"]},
		{"503 status", r.Metrics.HTTP.StatusCodes[http.StatusServiceUnavailable], ledger["/server-error"]},
		{"5xx family", r.Metrics.HTTP.Status5xx, ledger["/server-error"]},
		{"assertion failure", r.Metrics.HTTP.AssertionFailures, ledger["/assertion"] + ledger["/expected"]},
		{"network timeout", r.Metrics.HTTP.NetworkTimeouts, ledger["/timeout"]},
		{"oversized response", r.Metrics.HTTP.UnexpectedResponseSizes, ledger["/large"]},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s: got %d want %d", check.name, check.got, check.want)
		}
	}
	if len(r.Windows) == 0 || r.Windows[0].Metrics.HTTP.StatusCodes[http.StatusFound] == 0 || r.Windows[0].Metrics.HTTP.RateLimitedResponses == 0 {
		t.Fatalf("observation window omitted structured HTTP outcomes: %+v", r.Windows)
	}
	if r.Metrics.HTTP.Status3xx != r.Metrics.HTTP.RedirectResponses || r.Metrics.HTTP.Status4xx != r.Metrics.HTTP.RateLimitedResponses || r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("status family or outcome ledger mismatch: %+v quality=%+v", r.Metrics.HTTP, r.Quality)
	}
}

func TestHTTPDiagnosticsIdentifyTLSHandshakeFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "tls", Path: "/"}})
	v.Duration = 250 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	if r.Metrics.HTTP.TLSFailures == 0 || r.Metrics.HTTP.TransportFailures != 0 || r.Metrics.HTTP.Status2xx != 0 {
		t.Fatalf("local TLS handshake failure was not classified without inventing a response: %+v", r.Metrics.HTTP)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("classified TLS failure should remain arithmetically valid: %+v", r.Quality)
	}
}

func TestHTTPDiagnosticsIdentifyLoopbackConnectionRefusal(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	v := scenario(t, "http://"+address, []config.Step{{Name: "refused", Method: http.MethodGet, Path: "/"}})
	scenarioWithRefusal := v.Scenario
	scenarioWithRefusal.Users = 1
	scenarioWithRefusal.MaxRPS = 10
	scenarioWithRefusal.Timeout = "100ms"
	v, err = config.Parse(scenarioWithRefusal, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Total == 0 || r.Metrics.Failures != r.Metrics.Total ||
		r.Metrics.HTTP.TransportFailures != r.Metrics.Total ||
		r.Metrics.HTTP.ConnectionFailures != r.Metrics.Total || r.Metrics.HTTP.DNSFailures != 0 ||
		r.Metrics.HTTP.NetworkTimeouts != 0 || r.Metrics.HTTP.TLSFailures != 0 ||
		r.Metrics.HTTP.Status2xx != 0 || len(r.Steps) != 1 ||
		r.Steps[0].HTTPTrace.ConnectStartCount == 0 {
		t.Fatalf("loopback connection refusal was not classified as a transport failure: metrics=%+v", r.Metrics)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("accounted refused connections must not corrupt measurement arithmetic: %+v", r.Quality)
	}
}

func TestHTTPDiagnosticsIdentifyLoopbackTCPReset(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int64
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			accepted.Add(1)
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
		}
	}()
	defer func() {
		_ = listener.Close()
		select {
		case <-acceptDone:
		case <-time.After(time.Second):
			t.Error("loopback reset accept loop did not stop after listener close")
		}
	}()

	v := scenario(t, "http://"+listener.Addr().String(), []config.Step{{Name: "reset", Method: http.MethodGet, Path: "/"}})
	scenarioWithFreshConnections := v.Scenario
	scenarioWithFreshConnections.Users = 1
	scenarioWithFreshConnections.MaxRPS = 10
	scenarioWithFreshConnections.Timeout = "100ms"
	scenarioWithFreshConnections.ConnectionPolicy = "new_per_request"
	v, err = config.Parse(scenarioWithFreshConnections, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	<-acceptDone

	if r.Metrics.Total == 0 || r.Metrics.Failures != r.Metrics.Total ||
		r.Metrics.HTTP.TransportFailures != r.Metrics.Total ||
		r.Metrics.HTTP.NetworkTimeouts != 0 || r.Metrics.HTTP.TLSFailures != 0 ||
		r.Metrics.HTTP.StatusCodes[http.StatusOK] != 0 || accepted.Load() == 0 {
		t.Fatalf("loopback reset was not classified or no server-side connection was observed: metrics=%+v accepted_connections=%d", r.Metrics, accepted.Load())
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("accounted TCP resets must not corrupt measurement arithmetic: %+v", r.Quality)
	}
}

func TestHTTPDiagnosticsIdentifyInjectedLoopbackDNSFailure(t *testing.T) {
	dnsConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsDone := make(chan struct{})
	var queries atomic.Int64
	go func() {
		defer close(dnsDone)
		buffer := make([]byte, 1500)
		for {
			n, addr, readErr := dnsConn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			queries.Add(1)
			response, ok := dnsResponse(buffer[:n], 3)
			if !ok {
				continue
			}
			if _, writeErr := dnsConn.WriteTo(response, addr); writeErr != nil {
				return
			}
		}
	}()
	defer func() {
		_ = dnsConn.Close()
		select {
		case <-dnsDone:
		case <-time.After(time.Second):
			t.Error("loopback DNS responder did not stop after close")
		}
	}()

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", dnsConn.LocalAddr().String())
		},
	}
	dialer := &net.Dialer{Resolver: resolver}
	transport := &http.Transport{DialContext: dialer.DialContext}
	v := scenario(t, "https://damocline-g0-absent.invalid.", []config.Step{{Name: "dns_failure", Method: http.MethodGet, Path: "/"}})
	scenarioWithSingleActor := v.Scenario
	scenarioWithSingleActor.Users = 1
	scenarioWithSingleActor.MaxRPS = 10
	scenarioWithSingleActor.Timeout = "200ms"
	v, err = config.Parse(scenarioWithSingleActor, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := runWithTransport(context.Background(), v, transport)
	var dnsTrace report.HTTPTrace
	if len(r.Steps) == 1 {
		dnsTrace = r.Steps[0].HTTPTrace
	}

	if r.Metrics.Total == 0 || r.Metrics.Failures != r.Metrics.Total ||
		r.Metrics.HTTP.TransportFailures != r.Metrics.Total ||
		r.Metrics.HTTP.DNSFailures != r.Metrics.Total || r.Metrics.HTTP.ConnectionFailures != 0 ||
		r.Metrics.HTTP.NetworkTimeouts != 0 || r.Metrics.HTTP.TLSFailures != 0 ||
		r.Metrics.HTTP.Status2xx != 0 || len(r.Steps) != 1 ||
		dnsTrace.DNSStartCount == 0 || dnsTrace.DNSCount == 0 ||
		dnsTrace.ConnectStartCount != 0 || queries.Load() == 0 {
		t.Fatalf("injected NXDOMAIN was not observed and classified as a pre-connect transport failure: metrics=%+v dns_trace=%+v resolver_queries=%d", r.Metrics, dnsTrace, queries.Load())
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("accounted DNS failures must not corrupt measurement arithmetic: %+v", r.Quality)
	}
}

func TestHTTPDiagnosticsClassifyInjectedDNSLookupTimeoutAsTimeout(t *testing.T) {
	dnsConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsDone := make(chan struct{})
	var queries atomic.Int64
	go func() {
		defer close(dnsDone)
		buffer := make([]byte, 1500)
		for {
			if _, _, readErr := dnsConn.ReadFrom(buffer); readErr != nil {
				return
			}
			queries.Add(1)
		}
	}()
	defer func() {
		_ = dnsConn.Close()
		select {
		case <-dnsDone:
		case <-time.After(time.Second):
			t.Error("loopback DNS blackhole did not stop after close")
		}
	}()

	resolver := &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", dnsConn.LocalAddr().String())
		},
	}
	dialer := &net.Dialer{Resolver: resolver}
	transport := &http.Transport{DialContext: dialer.DialContext}
	v := scenario(t, "https://damocline-g0-timeout.invalid.", []config.Step{{Name: "dns_timeout", Method: http.MethodGet, Path: "/"}})
	s := v.Scenario
	s.Users = 1
	s.MaxRPS = 100
	s.Timeout = "250ms"
	v, err = config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = time.Second
	r := runWithTransport(context.Background(), v, transport)
	var dnsTrace report.HTTPTrace
	if len(r.Steps) == 1 {
		dnsTrace = r.Steps[0].HTTPTrace
	}

	if r.Metrics.Total == 0 || r.Metrics.HTTP.NetworkTimeouts == 0 ||
		r.Metrics.HTTP.NetworkTimeouts+r.Metrics.Cancelled != r.Metrics.Total ||
		r.Metrics.HTTP.DNSFailures != 0 || r.Metrics.HTTP.ConnectionFailures != 0 ||
		r.Metrics.HTTP.TransportFailures != 0 || r.Metrics.HTTP.TLSFailures != 0 ||
		len(r.Steps) != 1 || dnsTrace.DNSStartCount == 0 || dnsTrace.ConnectStartCount != 0 || queries.Load() == 0 {
		t.Fatalf("injected DNS lookup timeout was not kept separate from resolver and transport failures: metrics=%+v dns_trace=%+v resolver_queries=%d", r.Metrics, dnsTrace, queries.Load())
	}
}

func TestHTTPDiagnosticsClassifyInjectedDNSServFailAsDNSFailure(t *testing.T) {
	dnsConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsDone := make(chan struct{})
	var queries atomic.Int64
	go func() {
		defer close(dnsDone)
		buffer := make([]byte, 1500)
		for {
			n, addr, readErr := dnsConn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			queries.Add(1)
			response, ok := dnsResponse(buffer[:n], 2)
			if !ok {
				continue
			}
			if _, writeErr := dnsConn.WriteTo(response, addr); writeErr != nil {
				return
			}
		}
	}()
	defer func() {
		_ = dnsConn.Close()
		select {
		case <-dnsDone:
		case <-time.After(time.Second):
			t.Error("loopback DNS SERVFAIL responder did not stop after close")
		}
	}()

	resolver := &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", dnsConn.LocalAddr().String())
		},
	}
	dialer := &net.Dialer{Resolver: resolver}
	transport := &http.Transport{DialContext: dialer.DialContext}
	v := scenario(t, "https://damocline-g0-servfail.invalid.", []config.Step{{Name: "dns_servfail", Method: http.MethodGet, Path: "/"}})
	s := v.Scenario
	s.Users = 1
	s.MaxRPS = 10
	s.Timeout = "500ms"
	v, err = config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := runWithTransport(context.Background(), v, transport)
	var dnsTrace report.HTTPTrace
	if len(r.Steps) == 1 {
		dnsTrace = r.Steps[0].HTTPTrace
	}

	if r.Metrics.Total == 0 || r.Metrics.Failures != r.Metrics.Total ||
		r.Metrics.HTTP.DNSFailures != r.Metrics.Total || r.Metrics.HTTP.NetworkTimeouts != 0 ||
		r.Metrics.HTTP.ConnectionFailures != 0 || r.Metrics.HTTP.TransportFailures != r.Metrics.Total ||
		r.Metrics.HTTP.TLSFailures != 0 || r.Metrics.HTTP.Status2xx != 0 ||
		len(r.Steps) != 1 || dnsTrace.DNSStartCount == 0 || dnsTrace.ConnectStartCount != 0 || queries.Load() == 0 {
		t.Fatalf("injected DNS SERVFAIL was not classified as a DNS resolution failure: metrics=%+v dns_trace=%+v resolver_queries=%d", r.Metrics, dnsTrace, queries.Load())
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("accounted DNS SERVFAIL must not corrupt measurement arithmetic: %+v", r.Quality)
	}
}

func dnsResponse(query []byte, responseCode byte) ([]byte, bool) {
	if len(query) < 17 || binary.BigEndian.Uint16(query[4:6]) != 1 {
		return nil, false
	}
	offset := 12
	for {
		if offset >= len(query) {
			return nil, false
		}
		labelLength := int(query[offset])
		offset++
		if labelLength == 0 {
			break
		}
		if labelLength&0xc0 != 0 || offset+labelLength > len(query) {
			return nil, false
		}
		offset += labelLength
	}
	if offset+4 > len(query) {
		return nil, false
	}
	response := append([]byte(nil), query[:offset+4]...)
	response[2] |= 0x80                        // Mark this DNS packet as a response.
	response[3] = 0x80 | (responseCode & 0x0f) // Response with the requested DNS status.
	for index := 6; index < 12; index++ {
		response[index] = 0
	}
	return response, true
}

func TestNativeEngineReconcilesTrustedLocalTLSRequests(t *testing.T) {
	var received, completed atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		defer completed.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	server.EnableHTTP2 = false
	server.StartTLS()
	defer server.Close()

	previousTransport := http.DefaultTransport
	tlsTransport := server.Client().Transport.(*http.Transport).Clone()
	tlsTransport.Proxy = nil
	http.DefaultTransport = tlsTransport
	defer func() { http.DefaultTransport = previousTransport }()

	v := scenario(t, server.URL, []config.Step{{Name: "work", Method: http.MethodGet, Path: "/work"}})
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || r.Metrics.Total < 1 || r.Metrics.Success != r.Metrics.Total ||
		r.Metrics.HTTP.TLSFailures != 0 || r.Steps[0].HTTPTrace.TLSCount < 1 {
		t.Fatalf("native engine did not complete trusted TLS requests: outcome=%s metrics=%+v trace=%+v", r.Outcome, r.Metrics, r.Steps[0].HTTPTrace)
	}
	body := r.Steps[0].Metrics.ResponseBody
	if body.CompleteObservations != int64(r.Metrics.Total) || body.MinBytes != 4096 || body.MaxBytes != 4096 ||
		received.Load() != int64(r.Metrics.Total) || completed.Load() != received.Load() {
		t.Fatalf("TLS body or independent server ledger did not reconcile: client=%+v server_received=%d server_completed=%d", body, received.Load(), completed.Load())
	}
}

func TestWeightedProfilesAreMeasuredSeparately(t *testing.T) {
	seen := map[string]int{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := config.Scenario{
		Name: "weighted local profiles", BaseURL: srv.URL, Users: 4, Duration: "1s", MaxRPS: 50,
		P95LimitMS: 1000, MaxErrorRate: 0,
		Profiles: []config.Profile{
			{Name: "browse", Weight: 3, Steps: []config.Step{{Name: "dashboard", Path: "/dashboard"}}},
			{Name: "search", Weight: 1, Steps: []config.Step{{Name: "search", Path: "/search"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || len(r.Profiles) != 2 || r.Profiles[0].AssignedVUs != 3 || r.Profiles[1].AssignedVUs != 1 ||
		r.Profiles[0].Journeys.Total == 0 || r.Profiles[1].Journeys.Total == 0 {
		t.Fatalf("weighted profile evidence missing: %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["/dashboard"] == 0 || seen["/search"] == 0 {
		t.Fatalf("expected both workflows, got %+v", seen)
	}
}

func TestSyntheticDataIDsAreUniqueAndNeverReported(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("DAMOCLINE_TEST_DATA_PREFIX", "damocline-test")
	s := config.Scenario{Name: "synthetic write", BaseURL: srv.URL, Users: 2, Duration: "1s", MaxRPS: 30, P95LimitMS: 1000, MaxErrorRate: 0,
		SyntheticDataPrefixEnv: "DAMOCLINE_TEST_DATA_PREFIX", Steps: []config.Step{{Name: "create", Method: "POST", Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{204}}, {Name: "cleanup", Method: "DELETE", Path: "/records/{{synthetic_id}}", ExpectedStatus: []int{204}}}}
	v, err := config.Parse(s, config.Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Success == 0 || len(seen) < 2 {
		t.Fatalf("synthetic workflow did not execute: %+v paths=%v", r, seen)
	}
	for path := range seen {
		if !strings.HasPrefix(path, "/records/damocline-test-") {
			t.Fatalf("unexpected synthetic path %q", path)
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || strings.Contains(string(encoded), "damocline-test-") {
		t.Fatalf("synthetic IDs must not be reported: %s err=%v", encoded, err)
	}
}

func TestJourneyExtractsResponseValuesForLaterSteps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("X-Test-Token", "token-123")
			_, _ = w.Write([]byte(`{"record":{"id":"alpha/beta"}}`))
		case "/records/alpha/beta":
			if r.Header.Get("X-Token") != "Bearer token-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("record found"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := config.Scenario{Name: "dependent local journey", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{
			{Name: "session", Path: "/session", Extract: map[string]config.Extract{"record_id": {JSONPointer: "/record/id"}, "token": {Header: "X-Test-Token"}}},
			{Name: "record", Path: "/records/{{record_id}}", Headers: map[string]string{"X-Token": "Bearer {{token}}"}, MustContain: "record found"},
		}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || r.Steps[0].Metrics.Success == 0 || r.Steps[1].Metrics.Success == 0 {
		t.Fatalf("dependent journey did not complete: %+v", r)
	}
}

func TestFailedExtractionFailsStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"missing":true}`))
	}))
	defer srv.Close()
	s := config.Scenario{Name: "failed extraction", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 10, P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "session", Path: "/", Extract: map[string]config.Extract{"record_id": {JSONPointer: "/id"}}}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 200 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Outcome == "PASS" || r.Metrics.Reasons["extraction_failed"] == 0 {
		t.Fatalf("missing extraction was not reported: %+v", r)
	}
}

func TestObservationWindowsExposeLateSLOBreach(t *testing.T) {
	started := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if time.Since(started) > 150*time.Millisecond {
			time.Sleep(60 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Duration = 350 * time.Millisecond
	v.Window = 100 * time.Millisecond
	v.Scenario.P95LimitMS = 20
	r := Run(context.Background(), v)
	if len(r.Windows) < 2 || r.Outcome != "FAIL" || len(r.Windows[0].Steps) != 1 || len(r.Windows[1].Steps) != 1 {
		t.Fatalf("late window degradation was not visible: %+v", r)
	}
	if r.Windows[0].Steps[0].Name != "page" || r.Windows[1].Steps[0].Metrics.Total == 0 {
		t.Fatalf("per-step window evidence was not retained: %+v", r.Windows)
	}
	found := false
	for _, warning := range r.Warnings {
		if strings.Contains(warning, "breached an SLO") {
			found = true
		}
	}
	if !found {
		t.Fatalf("window breach warning missing: %+v", r.Warnings)
	}
}

func TestSlowMinorityProfileCannotHideInsideWindowAggregate(t *testing.T) {
	started := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(150 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := config.Scenario{
		Name: "weighted local window fixture", BaseURL: srv.URL, Users: 10,
		Duration: "3s", Mode: "arrival", ArrivalRate: 100, MaxRPS: 100,
		P95LimitMS: 75, MaxErrorRate: 0, MinSamplesPerStep: 1, WindowDuration: "1s",
		Profiles: []config.Profile{
			{Name: "rare_slow", Weight: 1, Steps: []config.Step{{Name: "rare_step", Path: "/slow"}}},
			{Name: "common_fast", Weight: 99, Steps: []config.Step{{Name: "common_step", Path: "/fast"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	if r.Metrics.P95MS >= s.P95LimitMS {
		t.Fatalf("fixture did not make the aggregate look healthy: aggregate p95 %.1f ms", r.Metrics.P95MS)
	}
	if r.Outcome == "PASS" {
		t.Fatalf("slow minority journey must prevent an aggregate-only pass: %+v", r)
	}
	foundSlowWindow := false
	for _, window := range r.Windows {
		for _, profile := range window.Profiles {
			if profile.Name == "rare_slow" && profile.Metrics.Total > 0 && profile.Metrics.P95MS > s.P95LimitMS {
				foundSlowWindow = true
			}
		}
	}
	if !foundSlowWindow {
		t.Fatalf("per-profile journey window evidence did not retain the slow workflow: %+v", r.Windows)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("bounded window fixture exceeded its five-second test ceiling")
	}
}

func TestArrivalProfilesFollowConfiguredWeights(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := config.Scenario{
		Name: "weighted arrival profiles", BaseURL: srv.URL, Users: 2, Duration: "1s", Mode: "arrival", ArrivalRate: 10, MaxRPS: 20,
		P95LimitMS: 1000, MaxErrorRate: 0,
		Profiles: []config.Profile{
			{Name: "browse", Weight: 3, Steps: []config.Step{{Name: "dashboard", Path: "/dashboard"}}},
			{Name: "search", Weight: 1, Steps: []config.Step{{Name: "search", Path: "/search"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	if r.Outcome != "PASS" || r.ArrivalsScheduled != 10 || r.ArrivalsDropped != 0 || len(r.Profiles) != 2 ||
		r.Profiles[0].Journeys.Total != 8 || r.Profiles[1].Journeys.Total != 2 ||
		r.Profiles[0].AssignedVUs != 0 || r.Profiles[1].AssignedVUs != 0 {
		t.Fatalf("arrival profile mix was not measured deterministically: %+v", r)
	}
}
func TestLoginRedirectIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", 302) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/private"}})
	r := Run(context.Background(), v)
	if r.Outcome != "FAIL" || r.Metrics.Reasons["http_302"] == 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestExpiredRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(500 * time.Millisecond); w.WriteHeader(200) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "slow", Path: "/slow"}})
	v.Timeout = 80 * time.Millisecond
	r := Run(context.Background(), v)
	if r.Metrics.Reasons["timeout"] < 1 {
		t.Fatalf("did not account for timeouts: %+v", r)
	}
}
func TestSecretNeverAppearsInOutput(t *testing.T) {
	t.Setenv("SECRET_FOR_ENGINE_TEST", "secret-session-value")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "secret-session-value" {
			t.Errorf("cookie missing")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "auth", Path: "/dashboard?private=secret", Headers: map[string]string{"Cookie": "${SECRET_FOR_ENGINE_TEST}"}}})
	r := Run(context.Background(), v)
	if strings.Contains(r.Steps[0].Path, "private") || strings.Contains(r.Steps[0].Path, "secret") {
		t.Fatalf("query leaked in path: %s", r.Steps[0].Path)
	}
}

func TestDistinctCookieSessions(t *testing.T) {
	t.Setenv("DAMOCLINE_VU_TEST_1", "sid=alpha")
	t.Setenv("DAMOCLINE_VU_TEST_2", "sid=beta")
	var mu sync.Mutex
	observed := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("sid")
		if err != nil {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		observed[ck.Value]++
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := config.Scenario{Name: "distinct", BaseURL: srv.URL, Users: 2, Duration: "1s", MaxRPS: 30, P95LimitMS: 2000, MinSamplesPerStep: 2, PerVUCookieEnvPrefix: "DAMOCLINE_VU_TEST_", Steps: []config.Step{{Name: "page", Path: "/"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	mu.Lock()
	defer mu.Unlock()
	if r.Outcome != "PASS" || observed["alpha"] == 0 || observed["beta"] == 0 {
		t.Fatalf("session isolation failed result=%+v observed=%v", r, observed)
	}
}
func TestArrivalDropReporting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(250 * time.Millisecond); w.WriteHeader(200) }))
	defer srv.Close()
	s := config.Scenario{Name: "arrival", BaseURL: srv.URL, Users: 1, Duration: "1s", MaxRPS: 30, Mode: "arrival", ArrivalRate: 20, P95LimitMS: 1000, Steps: []config.Step{{Name: "slow", Path: "/"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), v)
	if result.ArrivalsDropped == 0 || result.Outcome != "FAIL" {
		t.Fatalf("underdelivery must fail: %+v", result)
	}
}

func TestPlannedArrivalAccountingWithSlowLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(220 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	s := config.Scenario{
		Name: "local arrival fixture", BaseURL: server.URL, Users: 1,
		Duration: "1s", Mode: "arrival", ArrivalRate: 20, MaxRPS: 30,
		P95LimitMS: 1500, Steps: []config.Step{{Name: "page", Method: "GET", Path: "/"}},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	out := Run(context.Background(), v)
	if out.ArrivalsScheduled != 20 {
		t.Fatalf("lost planned events: expected 20, got %d: %+v", out.ArrivalsScheduled, out)
	}
	if out.ArrivalsScheduled != out.IterationsStarted+out.ArrivalsDropped {
		t.Fatalf("planned events do not reconcile: %+v", out)
	}
	if out.ArrivalsAdmitted == nil || *out.ArrivalsAdmitted != out.IterationsStarted || out.ArrivalsCancelledBeforeAdmission == nil || out.ArrivalsDropped != out.ArrivalsDroppedBusy+out.ArrivalsDroppedLate+*out.ArrivalsCancelledBeforeAdmission {
		t.Fatalf("dropped event reasons do not reconcile: %+v", out)
	}
	if out.ArrivalsDropped == 0 || out.Outcome == "PASS" {
		t.Fatalf("undelivered workload must not pass: %+v", out)
	}
}

func TestArrivalRunDrainsAdmittedWorkAfterScheduleWindow(t *testing.T) {
	// A 160ms response spans several 50ms arrival intervals. The last
	// admitted request must still be running at the 300ms cutoff,
	// independent of Windows/Linux scheduling jitter.
	fixture, err := reference.New(reference.Config{Delay: 160 * time.Millisecond, MaxConcurrent: 10, MaxRequests: 10})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	s := config.Scenario{Name: "arrival drain", BaseURL: server.URL, Users: 10,
		Duration: "1s", Mode: "arrival", ArrivalRate: 20, MaxRPS: 30, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	r := Run(context.Background(), v)
	ledger := fixture.Snapshot()
	if r.ArrivalsScheduled != 6 || r.ArrivalsDropped != 0 || r.IterationsCompleted != r.IterationsStarted ||
		r.ArrivalsAdmitted == nil || *r.ArrivalsAdmitted != r.IterationsStarted ||
		r.Metrics.Cancelled != 0 || r.Outcome != "PASS" || r.ObservationSeconds != v.Duration.Seconds() ||
		r.ElapsedSeconds <= r.ObservationSeconds {
		t.Fatalf("admitted arrivals should drain after scheduling stops: %+v", r)
	}
	if ledger.Received != 6 || ledger.Completed != 6 || ledger.Success != 6 || ledger.Failures != 0 || ledger.Cancelled != 0 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("graceful-drain work did not reconcile with the independent server ledger: %+v", ledger)
	}
}

func TestMultiStepJourneyWithThinkTimeDrainsAfterObservationWindow(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: 60 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 3})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()

	s := config.Scenario{
		Name:       "multi-step drain",
		BaseURL:    server.URL,
		Users:      1,
		Duration:   "1s",
		RampUp:     "0s",
		ThinkTime:  "40ms",
		Timeout:    "100ms",
		MaxRPS:     50,
		P95LimitMS: 1000,
		Steps: []config.Step{
			{Name: "first", Method: http.MethodGet, Path: "/work"},
			{Name: "second", Method: http.MethodGet, Path: "/work"},
			{Name: "third", Method: http.MethodGet, Path: "/work"},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 100 * time.Millisecond

	run := Run(context.Background(), v)
	ledger := fixture.Snapshot()
	if run.IterationsStarted != 1 || run.IterationsCompleted != 1 || run.Journeys.Success != 1 ||
		run.Metrics.Total != 3 || run.Metrics.Success != 3 || run.Metrics.Cancelled != 0 ||
		run.ObservationSeconds != v.Duration.Seconds() || run.ElapsedSeconds <= run.ObservationSeconds {
		t.Fatalf("multi-step work should finish during bounded drain after observation closes: %+v", run)
	}
	if ledger.Received != 3 || ledger.Completed != 3 || ledger.Success != 3 || ledger.Failures != 0 || ledger.Cancelled != 0 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("multi-step drain did not reconcile with the independent server ledger: %+v", ledger)
	}
}

func TestNaturalDrainDeadlineCancelsStepWaitingForRateToken(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: 10 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 2})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture)
	defer server.Close()

	s := config.Scenario{
		Name:       "natural drain deadline",
		BaseURL:    server.URL,
		Users:      1,
		Duration:   "1s",
		RampUp:     "0s",
		Timeout:    "100ms",
		MaxRPS:     1,
		P95LimitMS: 1000,
		Steps: []config.Step{
			{Name: "first", Method: http.MethodGet, Path: "/work"},
			{Name: "second", Method: http.MethodGet, Path: "/work"},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 1500 * time.Millisecond

	run := Run(context.Background(), v)
	ledger := fixture.Snapshot()
	if len(run.Steps) != 2 || run.IterationsStarted != 1 || run.IterationsCompleted != 0 ||
		run.Journeys.Cancelled != 1 || run.Metrics.Success != 1 || run.Metrics.Cancelled != 1 ||
		run.Steps[0].HTTPTrace.RequestAttemptCount != 1 || run.Steps[1].HTTPTrace.RequestAttemptCount != 0 ||
		run.Steps[1].HTTPTrace.RateWaitCancelledCount != 1 || run.Outcome == "PASS" ||
		run.Quality == nil || run.Quality.State != report.QualityLimited {
		t.Fatalf("natural drain expiry must count the undelivered step and limit quality: %+v", run)
	}
	if ledger.Received != 1 || ledger.Completed != 1 || ledger.Success != 1 || ledger.Failures != 0 || ledger.Cancelled != 0 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("the rate-token-cancelled step must not appear as server work: %+v", ledger)
	}
}

func TestCancelledRunCannotPass(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-requestStarted
		cancel()
	}()
	r := Run(ctx, v)
	if r.Metrics.Cancelled == 0 || r.Outcome == "PASS" {
		t.Fatalf("a run with cancelled requests must not pass: %+v", r)
	}
}

func TestInterruptedJourneyIsCountedAndCannotPass(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/"}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-requestStarted
		cancel()
	}()
	r := Run(ctx, v)
	if r.Journeys.Cancelled == 0 || r.Metrics.Cancelled == 0 ||
		r.Journeys.Total+r.Journeys.Cancelled != int(r.IterationsStarted) || r.Outcome != "ABORTED" {
		t.Fatalf("interrupted journey was not counted explicitly: %+v", r)
	}
}

func TestCancelledRequestReconcilesWithIndependentReferenceLedger(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: time.Second, MaxConcurrent: 1, MaxRequests: 10})
	if err != nil {
		t.Fatal(err)
	}
	requestStarted := make(chan struct{}, 1)
	requestFinished := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		defer func() { requestFinished <- struct{}{} }()
		fixture.ServeHTTP(w, r)
	}))
	defer server.Close()

	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/work"}})
	v.Scenario.Users = 1
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-requestStarted
		cancel()
	}()
	run := Run(ctx, v)
	defer cancel()
	select {
	case <-requestFinished:
	case <-time.After(time.Second):
		t.Fatal("reference handler did not finish after the client cancelled")
	}
	ledger := fixture.Snapshot()
	if run.Metrics.Cancelled != 1 || run.Metrics.Success != 0 || run.Journeys.Cancelled != 1 || run.Outcome != "ABORTED" {
		t.Fatalf("cancelled client work was not reported exactly: %+v", run)
	}
	if ledger.Received != 1 || ledger.Completed != 0 || ledger.Success != 0 || ledger.Failures != 0 || ledger.Cancelled != 1 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("independent reference ledger did not reconcile cancellation: %+v", ledger)
	}
}

func TestExternalCancellationDuringDrainReconcilesWithIndependentReferenceLedger(t *testing.T) {
	fixture, err := reference.New(reference.Config{Delay: 1500 * time.Millisecond, MaxConcurrent: 10, MaxRequests: 10})
	if err != nil {
		t.Fatal(err)
	}
	requestStarted := make(chan struct{}, 10)
	requestFinished := make(chan struct{}, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		defer func() { requestFinished <- struct{}{} }()
		fixture.ServeHTTP(w, r)
	}))
	defer server.Close()

	v := scenario(t, server.URL, []config.Step{{Name: "slow", Path: "/work"}})
	v.Scenario.Users = 10
	v.Scenario.Mode = "arrival"
	v.Scenario.ArrivalRate = 20
	v.Scenario.MaxRPS = 30
	v.Duration = 300 * time.Millisecond
	v.Timeout = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for range 6 {
			<-requestStarted
		}
		// All scheduled work is admitted before this delay expires; abort while
		// the engine is draining in-flight journeys after the observation window.
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	run := Run(ctx, v)
	cancel()
	for i := int64(0); i < run.IterationsStarted; i++ {
		select {
		case <-requestFinished:
		case <-time.After(time.Second):
			t.Fatalf("reference handler %d did not finish after cancellation during drain", i+1)
		}
	}
	ledger := fixture.Snapshot()
	if run.ArrivalsScheduled != 6 || run.IterationsStarted != 6 || run.IterationsCompleted != 0 ||
		run.Metrics.Cancelled != 6 || run.Journeys.Cancelled != 6 || run.Outcome == "PASS" {
		t.Fatalf("work interrupted during drain was not reported as incomplete: %+v", run)
	}
	if ledger.Received != 6 || ledger.Completed != 0 || ledger.Success != 0 || ledger.Failures != 0 || ledger.Cancelled != 6 || ledger.Rejected != 0 ||
		ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
		t.Fatalf("cancellation during drain did not reconcile with the independent server ledger: %+v", ledger)
	}
}

func TestRunIncludesLocalGeneratorHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	result := Run(context.Background(), v)
	if result.SchemaVersion != report.CurrentSchemaVersion || result.ArrivalsAdmitted == nil || *result.ArrivalsAdmitted != 0 || result.Generator == nil || result.Generator.Samples < 2 ||
		result.Generator.PeakGoroutines < 1 || result.Generator.PeakHeapAllocBytes == 0 {
		t.Fatalf("generator health not sampled: %+v", result.Generator)
	}
	if result.Outcome != "PASS" {
		t.Fatalf("monitoring changed successful workload: %+v", result)
	}
}

func TestInsufficientSamplesCannotPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	v := scenario(t, srv.URL, []config.Step{{Name: "page", Path: "/"}})
	v.Scenario.MinSamplesPerStep = 1000
	r := Run(context.Background(), v)
	if r.Metrics.Success == 0 || r.Metrics.Total >= 1000 || r.Outcome == "PASS" {
		t.Fatalf("insufficient samples must not pass: %+v", r)
	}
}

func TestResponseBodyLimitIsReported(t *testing.T) {
	for _, tc := range []struct {
		name string
		body int
	}{
		{name: "at limit", body: maxResponseBytes},
		{name: "over limit", body: maxResponseBytes + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received, written atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				n, err := w.Write(make([]byte, tc.body))
				if err != nil {
					t.Errorf("write synthetic body: %v", err)
				}
				written.Add(int64(n))
			}))
			defer srv.Close()
			v := scenario(t, srv.URL, []config.Step{{Name: "large", Path: "/"}})
			r := Run(context.Background(), v)
			step := r.Steps[0]
			if received.Load() != int64(r.Metrics.Total) || r.Metrics.Total == 0 || written.Load() != int64(tc.body)*received.Load() {
				t.Fatalf("client and independent server ledgers differ: total=%d received=%d bytes=%d body_size=%d", r.Metrics.Total, received.Load(), written.Load(), tc.body)
			}
			if tc.body == maxResponseBytes {
				for label, body := range map[string]report.ResponseBodyDiagnostics{"aggregate": r.Metrics.ResponseBody, "step": step.Metrics.ResponseBody} {
					if body.CompleteObservations != received.Load() || body.TotalBytes != written.Load() || body.MinBytes != int64(tc.body) || body.MaxBytes != int64(tc.body) || body.OversizedObservations != 0 || body.IncompleteObservations != 0 {
						t.Errorf("%s body at the exact limit was not fully accounted: %+v", label, body)
					}
				}
				if r.Metrics.HTTP.UnexpectedResponseSizes != 0 {
					t.Errorf("response at the exact limit was marked oversized: %+v", r.Metrics.HTTP)
				}
				return
			}
			if r.Metrics.Reasons["response_too_large"] != r.Metrics.Total || r.Outcome == "PASS" {
				t.Errorf("oversized responses must be bounded and fail visibly: %+v", r)
			}
			for label, body := range map[string]report.ResponseBodyDiagnostics{"aggregate": r.Metrics.ResponseBody, "step": step.Metrics.ResponseBody} {
				if body.OversizedObservations != received.Load() || body.CompleteObservations != 0 || body.IncompleteObservations != 0 || body.TotalBytes != 0 {
					t.Errorf("%s oversized bodies must not claim a complete or full byte count: %+v", label, body)
				}
			}
			if r.Metrics.HTTP.UnexpectedResponseSizes != r.Metrics.Total {
				t.Errorf("oversized response diagnostic differs from independent ledger: %+v", r.Metrics.HTTP)
			}
		})
	}
}

func TestResponseBodySizesMatchIndependentLoopbackLedger(t *testing.T) {
	sizes := []int{0, 16, 1024, 4096}
	var mu sync.Mutex
	var requests, total int64
	var minimum, maximum int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		size := sizes[requests%int64(len(sizes))]
		requests++
		total += int64(size)
		if requests == 1 || int64(size) < minimum {
			minimum = int64(size)
		}
		if int64(size) > maximum {
			maximum = int64(size)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("synthetic-private-response"), (size+25)/26)[:size])
	}))
	defer server.Close()

	v := scenario(t, server.URL, []config.Step{{Name: "payload", Path: "/"}})
	v.Duration = 900 * time.Millisecond
	v.Scenario.Users = 1
	v.Scenario.MaxRPS = 10
	r := Run(context.Background(), v)
	mu.Lock()
	ledger := struct{ requests, total, minimum, maximum int64 }{requests, total, minimum, maximum}
	mu.Unlock()
	if ledger.requests < 3 || r.SchemaVersion != report.CurrentSchemaVersion || len(r.Steps) != 1 {
		t.Fatalf("expected bounded current-schema run with independent loopback requests: ledger=%+v report=%+v", ledger, r)
	}
	for label, observed := range map[string]report.ResponseBodyDiagnostics{
		"aggregate": r.Metrics.ResponseBody,
		"step":      r.Steps[0].Metrics.ResponseBody,
	} {
		if observed.CompleteObservations != ledger.requests || observed.TotalBytes != ledger.total || observed.MinBytes != ledger.minimum || observed.MaxBytes != ledger.maximum || observed.IncompleteObservations != 0 || observed.OversizedObservations != 0 {
			t.Fatalf("%s response-size summary differs from independent server ledger: summary=%+v ledger=%+v", label, observed, ledger)
		}
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("response-body ledger did not validate against the independent server ledger: %+v", r.Quality)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "synthetic-private-response") {
		t.Fatal("report must not preserve response-body values")
	}
}
