// Package reference provides bounded, deterministic loopback HTTP workloads
// for validating the load engine. It is not an application benchmark.
package reference

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"
)

// Config defines a synthetic service with known response and queue behavior.
type Config struct {
	Delay         time.Duration
	ExtraDelay    time.Duration
	ExtraEvery    int64
	FailureEvery  int64
	FailureFirst  int64
	ResponseBytes int
	CloseEvery    int64
	MaxConcurrent int
	MaxRequests   int64
}

type Snapshot struct {
	Received       int64
	Completed      int64
	Success        int64
	Failures       int64
	Cancelled      int64
	Rejected       int64
	PeakConcurrent int64
	PeakQueued     int64
}

type Handler struct {
	config     Config
	slots      chan struct{}
	received   atomic.Int64
	completed  atomic.Int64
	success    atomic.Int64
	failures   atomic.Int64
	cancelled  atomic.Int64
	rejected   atomic.Int64
	active     atomic.Int64
	queued     atomic.Int64
	peakActive atomic.Int64
	peakQueued atomic.Int64
}

func New(config Config) (*Handler, error) {
	if config.Delay < 0 || config.Delay > 2*time.Second || config.ExtraDelay < 0 || config.ExtraDelay > 2*time.Second ||
		config.ExtraEvery < 0 || config.FailureEvery < 0 || config.FailureFirst < 0 || config.CloseEvery < 0 ||
		config.ResponseBytes < 0 || config.ResponseBytes > 64<<10 ||
		config.MaxConcurrent < 1 || config.MaxConcurrent > 50 || config.MaxRequests < 1 || config.MaxRequests > 10000 {
		return nil, errors.New("reference workload is outside bounded limits")
	}
	return &Handler{config: config, slots: make(chan struct{}, config.MaxConcurrent)}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/work" {
		http.NotFound(w, r)
		return
	}
	index := h.received.Add(1)
	if index > h.config.MaxRequests {
		h.rejected.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		queued := h.queued.Add(1)
		peak(&h.peakQueued, queued)
		select {
		case h.slots <- struct{}{}:
			h.queued.Add(-1)
		case <-r.Context().Done():
			h.queued.Add(-1)
			h.cancelled.Add(1)
			return
		}
	}
	active := h.active.Add(1)
	peak(&h.peakActive, active)
	defer func() {
		h.active.Add(-1)
		<-h.slots
	}()
	delay := h.config.Delay
	if h.config.ExtraEvery > 0 && index%h.config.ExtraEvery == 0 {
		delay += h.config.ExtraDelay
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			h.cancelled.Add(1)
			return
		}
	}
	if r.Context().Err() != nil {
		h.cancelled.Add(1)
		return
	}
	if h.config.CloseEvery > 0 && index%h.config.CloseEvery == 0 {
		w.Header().Set("Connection", "close")
	}
	if index <= h.config.FailureFirst || (h.config.FailureEvery > 0 && index%h.config.FailureEvery == 0) {
		h.failures.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		h.completed.Add(1)
		return
	}
	body := []byte("reference-ok")
	if h.config.ResponseBytes > 0 {
		body = make([]byte, h.config.ResponseBytes)
		copy(body, "reference-ok")
		for i := len("reference-ok"); i < len(body); i++ {
			body[i] = 'x'
		}
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.cancelled.Add(1)
		return
	}
	h.success.Add(1)
	h.completed.Add(1)
}

func (h *Handler) Snapshot() Snapshot {
	return Snapshot{
		Received: h.received.Load(), Completed: h.completed.Load(), Success: h.success.Load(),
		Failures: h.failures.Load(), Cancelled: h.cancelled.Load(), Rejected: h.rejected.Load(),
		PeakConcurrent: h.peakActive.Load(), PeakQueued: h.peakQueued.Load(),
	}
}

func peak(maximum *atomic.Int64, value int64) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}
