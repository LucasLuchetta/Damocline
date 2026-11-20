package selfcalibration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
)

// Ledger is the mirror's independent server-side count of what it received
// and completed. It never stores request or response content.
type Ledger struct {
	Received          int64
	Completed         int64
	Unmatched         int64
	RequestBodyBytes  int64
	ResponseBodyBytes int64
	CookieRequests    int64
}

type mirrorRoute struct {
	calibration.MirrorRoute
	body []byte
}

// Mirror is a bounded loopback HTTP server that answers each declared route
// with a fixed status, delay and body size.
type Mirror struct {
	routes   []mirrorRoute
	server   *http.Server
	listener net.Listener
	wg       sync.WaitGroup

	received, completed, unmatched, requestBytes, responseBytes, cookies atomic.Int64
}

// StartMirror binds only to 127.0.0.1. Bodies start with the given prefix
// (for example a step's must_contain text) and are padded to the declared size.
func StartMirror(routes []calibration.MirrorRoute, prefixes []string) (*Mirror, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	m := &Mirror{listener: listener}
	for i, route := range routes {
		body := bytes.Repeat([]byte{'.'}, route.ResponseBytes)
		if i < len(prefixes) && len(prefixes[i]) <= len(body) {
			copy(body, prefixes[i])
		}
		m.routes = append(m.routes, mirrorRoute{MirrorRoute: route, body: body})
	}
	m.server = &http.Server{Handler: http.HandlerFunc(m.handle), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 64 << 10}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_ = m.server.Serve(listener)
	}()
	return m, nil
}

// BaseURL is the loopback origin of the mirror.
func (m *Mirror) BaseURL() string { return "http://" + m.listener.Addr().String() }

// Reset clears the ledger between calibration levels.
func (m *Mirror) Reset() {
	for _, counter := range []*atomic.Int64{&m.received, &m.completed, &m.unmatched, &m.requestBytes, &m.responseBytes, &m.cookies} {
		counter.Store(0)
	}
}

// Snapshot returns the current ledger.
func (m *Mirror) Snapshot() Ledger {
	return Ledger{Received: m.received.Load(), Completed: m.completed.Load(), Unmatched: m.unmatched.Load(),
		RequestBodyBytes: m.requestBytes.Load(), ResponseBodyBytes: m.responseBytes.Load(), CookieRequests: m.cookies.Load()}
}

// Close stops the mirror and waits for its serve loop.
func (m *Mirror) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := m.server.Shutdown(ctx)
	m.wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (m *Mirror) match(method, path string) *mirrorRoute {
	var best *mirrorRoute
	for i := range m.routes {
		route := &m.routes[i]
		if route.Method == method && strings.HasPrefix(path, route.PathPrefix) && (best == nil || len(route.PathPrefix) > len(best.PathPrefix)) {
			best = route
		}
	}
	return best
}

func (m *Mirror) handle(w http.ResponseWriter, r *http.Request) {
	m.received.Add(1)
	if r.Header.Get("Cookie") != "" {
		m.cookies.Add(1)
	}
	n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, calibration.MaxMirrorResponseBytes+1))
	m.requestBytes.Add(n)
	route := m.match(r.Method, r.URL.Path)
	if route == nil {
		m.unmatched.Add(1)
		http.Error(w, "unmatched mirror route", http.StatusNotFound)
		return
	}
	if route.DelayMS > 0 {
		timer := time.NewTimer(time.Duration(route.DelayMS * float64(time.Millisecond)))
		select {
		case <-timer.C:
		case <-r.Context().Done():
			timer.Stop()
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(route.Status)
	written, err := w.Write(route.body)
	m.responseBytes.Add(int64(written))
	if err == nil && written == len(route.body) {
		m.completed.Add(1)
	}
}
