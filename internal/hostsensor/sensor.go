// Package hostsensor exposes bounded Linux host observations to an authorized
// campaign. It does not infer which process or dependency limits capacity.
package hostsensor

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

const Path = "/v1/host"

type Reading struct {
	SchemaVersion int            `json:"schema_version"`
	ExperimentID  string         `json:"experiment_id"`
	ObservedAt    string         `json:"observed_at"`
	Host          hostmon.Result `json:"host"`
}

type Collector func(context.Context) (hostmon.Result, error)

func NativeCollector(pid int) Collector {
	return func(ctx context.Context) (hostmon.Result, error) {
		return hostmon.CollectWithWorkload(ctx, 1100*time.Millisecond, time.Second, pid)
	}
}

func NewHandler(token string, collect Collector) (http.Handler, error) {
	if token == "" || len(token) > 4096 || collect == nil {
		return nil, fmt.Errorf("sensor requires a bounded nonempty token and collector")
	}
	permit := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != Path {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || len(r.URL.RawQuery) != 0 {
			http.Error(w, "request body and query are not accepted", http.StatusBadRequest)
			return
		}
		want := "Bearer " + token
		given := r.Header.Get("Authorization")
		if len(given) != len(want) || subtle.ConstantTimeCompare([]byte(given), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := r.Header.Get("X-Damocline-Experiment-ID")
		if !ValidExperimentID(id) {
			http.Error(w, "invalid experiment ID", http.StatusBadRequest)
			return
		}
		select {
		case permit <- struct{}{}:
			defer func() { <-permit }()
		default:
			http.Error(w, "sensor busy", http.StatusTooManyRequests)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		host, err := collect(ctx)
		if err != nil || len(host.Samples) != 1 {
			http.Error(w, "host collection unavailable", http.StatusServiceUnavailable)
			return
		}
		reading := Reading{SchemaVersion: 1, ExperimentID: id, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Host: host}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reading)
	}), nil
}

func ValidExperimentID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// ValidateBind requires TLS for any non-loopback listener.
func ValidateBind(address, certFile, keyFile string) error {
	return validateBindForOS(runtime.GOOS, address, certFile, keyFile)
}

func validateBindForOS(goos, address, certFile, keyFile string) error {
	if goos != "linux" {
		return fmt.Errorf("host sensor currently supports native Linux only")
	}
	if (certFile == "") != (keyFile == "") {
		return fmt.Errorf("TLS certificate and key must be supplied together")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("sensor listen address must include host and port: %w", err)
	}
	if certFile == "" && !loopbackHost(host) {
		return fmt.Errorf("non-loopback sensor listener requires TLS")
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
