package engine

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// idleCloseServer serves exactly one HTTP/1.1 response per connection while
// advertising keep-alive, then reads the next request on that connection and
// closes it without answering. The client has already written the request on
// the reused connection when the close happens, so the race is removed: every
// reuse deterministically fails before any response byte and Go's transport
// transparently retries the idempotent GET on a fresh connection.
type idleCloseServer struct {
	listener   net.Listener
	served     atomic.Int64
	unanswered atomic.Int64
	wg         sync.WaitGroup
}

func newIdleCloseServer(t *testing.T) *idleCloseServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &idleCloseServer{listener: ln}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer conn.Close()
				reader := bufio.NewReader(conn)
				req, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				req.Body.Close()
				s.served.Add(1)
				if _, err := conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: keep-alive\r\n\r\n")); err != nil {
					return
				}
				next, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				next.Body.Close()
				s.unanswered.Add(1)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		s.wg.Wait()
	})
	return s
}

func TestTransparentTransportRetryKeepsTraceValid(t *testing.T) {
	server := newIdleCloseServer(t)
	scenario := config.Scenario{Name: "idle close", BaseURL: "http://" + server.listener.Addr().String(), Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "page", Path: "/page"}}}
	v, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), v)
	server.listener.Close()
	server.wg.Wait()
	if r.SchemaVersion != 36 || len(r.Steps) != 1 {
		t.Fatalf("unexpected report: schema=%d steps=%d", r.SchemaVersion, len(r.Steps))
	}
	trace := r.Steps[0].HTTPTrace
	m := r.Steps[0].Metrics
	served, unanswered := int(server.served.Load()), int(server.unanswered.Load())
	if m.Failures != 0 || m.Total < 3 || m.Success != served {
		t.Fatalf("outcome ledger does not reconcile with server: metrics=%+v served=%d", m, served)
	}
	if unanswered == 0 || trace.TransportRetryCount != unanswered {
		t.Fatalf("transport retries=%d, server closed %d reused-connection requests unanswered", trace.TransportRetryCount, unanswered)
	}
	if trace.RequestAttemptCount != m.Total || trace.CompleteCount != m.Total ||
		trace.ConnectionAcquireCount != trace.RequestAttemptCount+trace.TransportRetryCount ||
		trace.WriteCount != trace.RequestAttemptCount+trace.TransportRetryCount ||
		trace.ConnectStartCount != served {
		t.Fatalf("trace counters do not reconcile: %+v served=%d unanswered=%d", trace, served, unanswered)
	}
	if q := report.AssessQuality(r); q.State == report.QualityInvalid {
		t.Fatalf("transparent retries must not invalidate the run: %+v", q.Findings)
	}
}
