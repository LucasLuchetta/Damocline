// Package labtarget provides a bounded synthetic HTTP target with a known
// concurrency restriction for controlled capacity experiments.
package labtarget

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type Options struct {
	Delay      time.Duration
	Parallel   int
	QueueLimit int
}

func NewHandler(options Options) (http.Handler, error) {
	if options.Delay < time.Millisecond || options.Delay > time.Second || options.Parallel < 1 || options.Parallel > 16 || options.QueueLimit < options.Parallel || options.QueueLimit > 64 {
		return nil, fmt.Errorf("lab target requires delay 1ms..1s, parallel 1..16, and queue limit between parallel and 64")
	}
	active := make(chan struct{}, options.Parallel)
	admitted := make(chan struct{}, options.QueueLimit)
	var completed, rejected atomic.Uint64
	processStartTime := float64(time.Now().UnixNano()) / float64(time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /work", func(w http.ResponseWriter, r *http.Request) {
		select {
		case admitted <- struct{}{}:
			defer func() { <-admitted }()
		default:
			rejected.Add(1)
			http.Error(w, "synthetic queue full", http.StatusServiceUnavailable)
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		case <-r.Context().Done():
			return
		}
		timer := time.NewTimer(options.Delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			completed.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("synthetic work complete\n"))
		case <-r.Context().Done():
			return
		}
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE process_start_time_seconds gauge\n# UNIT process_start_time_seconds seconds\nprocess_start_time_seconds %.9f\n# TYPE lab_requests_total counter\nlab_requests_total{outcome=\"completed\"} %d\nlab_requests_total{outcome=\"rejected\"} %d\n# TYPE lab_active gauge\nlab_active %d\n# TYPE lab_admitted gauge\nlab_admitted %d\n", processStartTime, completed.Load(), rejected.Load(), len(active), len(admitted))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux, nil
}
