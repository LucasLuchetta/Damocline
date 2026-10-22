// Package reference provides bounded, deterministic loopback HTTP workloads
// for validating the load engine. It is not an application benchmark.
package reference

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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
	EmptyResponse bool
	CloseEvery    int64
	MaxConcurrent int
	MaxRequests   int64
	Profiles      []WorkloadProfile
}

// WorkloadProfile selects a bounded synthetic request and response shape by
// an exact local fixture path.
type WorkloadProfile struct {
	ID                string `json:"id"`
	CounterID         string `json:"counter_id,omitempty"`
	Method            string `json:"method"`
	Path              string `json:"path"`
	RequestBodyBytes  int    `json:"request_body_bytes"`
	ResponseBodyBytes int    `json:"response_body_bytes"`
}

type Snapshot struct {
	Received               int64
	Completed              int64
	Success                int64
	Failures               int64
	Cancelled              int64
	Rejected               int64
	PeakConcurrent         int64
	PeakQueued             int64
	AcceptedTLSConnections int64 `json:"accepted_tls_connections,omitempty"`
	RequestBodyBytes       int64
	ResponseBodyBytes      int64
	ProfileTCPConnections  map[string]int64 `json:"profile_tcp_connections,omitempty"`
	ProfileReceived        map[string]int64 `json:"profile_received,omitempty"`
	ProfileCompleted       map[string]int64 `json:"profile_completed,omitempty"`
	ProfileRequestBytes    map[string]int64 `json:"profile_request_body_bytes,omitempty"`
	ProfileResponseBytes   map[string]int64 `json:"profile_response_body_bytes,omitempty"`
}

type acceptedConnectionContextKey struct{}

type Handler struct {
	config                 Config
	slots                  chan struct{}
	received               atomic.Int64
	completed              atomic.Int64
	success                atomic.Int64
	failures               atomic.Int64
	cancelled              atomic.Int64
	rejected               atomic.Int64
	active                 atomic.Int64
	queued                 atomic.Int64
	peakActive             atomic.Int64
	peakQueued             atomic.Int64
	acceptedTLSConnections atomic.Int64
	requestBodyBytes       atomic.Int64
	responseBodyBytes      atomic.Int64
	nextConnectionID       atomic.Int64
	connectionsMu          sync.Mutex
	tcpConnectionIDsByPath map[string]map[int64]struct{}
	profileReceived        map[string]*atomic.Int64
	profileCompleted       map[string]*atomic.Int64
	profileRequestBytes    map[string]*atomic.Int64
	profileResponseBytes   map[string]*atomic.Int64
	profiles               map[string]WorkloadProfile
}

func New(config Config) (*Handler, error) {
	if config.Delay < 0 || config.Delay > 2*time.Second || config.ExtraDelay < 0 || config.ExtraDelay > 2*time.Second ||
		config.ExtraEvery < 0 || config.FailureEvery < 0 || config.FailureFirst < 0 || config.CloseEvery < 0 ||
		config.ResponseBytes < 0 || config.ResponseBytes > 64<<10 ||
		config.MaxConcurrent < 1 || config.MaxConcurrent > 50 || config.MaxRequests < 1 || config.MaxRequests > 10000 {
		return nil, errors.New("reference workload is outside bounded limits")
	}
	if len(config.Profiles) > 20 {
		return nil, errors.New("reference workload has too many profiles")
	}
	profiles := make(map[string]WorkloadProfile, len(config.Profiles))
	profileIDs := make(map[string]bool, len(config.Profiles))
	counterIDs := make(map[string]bool, len(config.Profiles))
	profilePaths := make(map[string]bool, len(config.Profiles))
	for _, profile := range config.Profiles {
		validMethod := profile.Method == http.MethodGet || profile.Method == http.MethodPost
		validPath := (strings.HasPrefix(profile.Path, "/matrix/") && profile.Path != "/matrix/" || profile.Path == "/work") &&
			len(profile.Path) <= 128 && !strings.ContainsAny(profile.Path, "?#") && !profilePaths[profile.Path]
		validSizes := profile.RequestBodyBytes >= 0 && profile.RequestBodyBytes <= 64<<10 &&
			profile.ResponseBodyBytes >= 0 && profile.ResponseBodyBytes <= 64<<10 &&
			(profile.Method != http.MethodGet || profile.RequestBodyBytes == 0)
		counterIDValid := profile.CounterID == "" || len(profile.CounterID) <= 64 && !counterIDs[profile.CounterID]
		if profile.ID == "" || len(profile.ID) > 64 || !counterIDValid || profileIDs[profile.ID] || !validMethod || !validPath || !validSizes {
			return nil, errors.New("reference profile is outside bounded limits")
		}
		profileIDs[profile.ID] = true
		if profile.CounterID != "" {
			counterIDs[profile.CounterID] = true
		}
		profilePaths[profile.Path] = true
		profiles[profile.Path] = profile
	}
	profileReceived := make(map[string]*atomic.Int64, len(counterIDs))
	profileCompleted := make(map[string]*atomic.Int64, len(counterIDs))
	profileRequestBytes := make(map[string]*atomic.Int64, len(counterIDs))
	profileResponseBytes := make(map[string]*atomic.Int64, len(counterIDs))
	for id := range counterIDs {
		profileReceived[id] = &atomic.Int64{}
		profileCompleted[id] = &atomic.Int64{}
		profileRequestBytes[id] = &atomic.Int64{}
		profileResponseBytes[id] = &atomic.Int64{}
	}
	return &Handler{
		config: config, slots: make(chan struct{}, config.MaxConcurrent), profiles: profiles,
		tcpConnectionIDsByPath: make(map[string]map[int64]struct{}),
		profileReceived:        profileReceived, profileCompleted: profileCompleted,
		profileRequestBytes: profileRequestBytes, profileResponseBytes: profileResponseBytes,
	}, nil
}

// ConnContext assigns a server-side identity to each accepted TCP connection.
// Configure it as http.Server.ConnContext to count distinct connections that
// carry requests for each synthetic fixture path.
func (h *Handler) ConnContext(ctx context.Context, conn net.Conn) context.Context {
	if _, ok := conn.(*tls.Conn); ok {
		h.acceptedTLSConnections.Add(1)
	}
	return context.WithValue(ctx, acceptedConnectionContextKey{}, h.nextConnectionID.Add(1))
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	selected, profiled := h.profiles[r.URL.Path]
	profile := selected
	validLegacyRequest := !profiled && r.Method == http.MethodGet && r.URL.Path == "/work"
	validProfileRequest := profiled && r.Method == profile.Method
	if !validLegacyRequest && !validProfileRequest {
		http.NotFound(w, r)
		return
	}
	h.recordTCPConnection(r)
	if profiled && profile.CounterID != "" {
		h.incrementProfile(h.profileReceived, profile.CounterID)
	}
	index := h.received.Add(1)
	if profiled {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, int64(profile.RequestBodyBytes)+1))
		h.requestBodyBytes.Add(int64(len(body)))
		if profiled && profile.CounterID != "" {
			h.addProfile(profile.CounterID, h.profileRequestBytes, int64(len(body)))
		}
		if readErr != nil || len(body) != profile.RequestBodyBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			h.failures.Add(1)
			h.completed.Add(1)
			if profiled && profile.CounterID != "" {
				h.incrementProfile(h.profileCompleted, profile.CounterID)
			}
			return
		}
	}
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
		if profiled && profile.CounterID != "" {
			h.incrementProfile(h.profileCompleted, profile.CounterID)
		}
		return
	}
	responseBytes := h.config.ResponseBytes
	if profiled {
		responseBytes = profile.ResponseBodyBytes
	}
	body := []byte("reference-ok")
	if h.config.EmptyResponse || (profiled && responseBytes == 0) {
		body = nil
	} else if responseBytes > 0 {
		body = make([]byte, responseBytes)
		copy(body, "reference-ok")
		for i := len("reference-ok"); i < len(body); i++ {
			body[i] = 'x'
		}
	}
	w.WriteHeader(http.StatusOK)
	written, err := w.Write(body)
	h.responseBodyBytes.Add(int64(written))
	if profiled && profile.CounterID != "" {
		h.addProfile(profile.CounterID, h.profileResponseBytes, int64(written))
	}
	if err != nil || written != len(body) {
		h.cancelled.Add(1)
		return
	}
	h.success.Add(1)
	h.completed.Add(1)
	if profiled && profile.CounterID != "" {
		h.incrementProfile(h.profileCompleted, profile.CounterID)
	}
}

// ServeReferenceControl exposes counters only when explicitly mounted by the
// calibrator child process. Control requests are not part of the work ledger.
func ServeReferenceControl(h *Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/__damocline_snapshot" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.Snapshot())
	}
}

func (h *Handler) Snapshot() Snapshot {
	h.connectionsMu.Lock()
	profileConnections := make(map[string]int64, len(h.tcpConnectionIDsByPath))
	for path, connectionIDs := range h.tcpConnectionIDsByPath {
		profileConnections[path] = int64(len(connectionIDs))
	}
	h.connectionsMu.Unlock()
	profileReceived := snapshotProfileCounts(h.profileReceived)
	profileCompleted := snapshotProfileCounts(h.profileCompleted)
	profileRequestBytes := snapshotProfileCounts(h.profileRequestBytes)
	profileResponseBytes := snapshotProfileCounts(h.profileResponseBytes)
	return Snapshot{
		Received: h.received.Load(), Completed: h.completed.Load(), Success: h.success.Load(),
		Failures: h.failures.Load(), Cancelled: h.cancelled.Load(), Rejected: h.rejected.Load(),
		PeakConcurrent: h.peakActive.Load(), PeakQueued: h.peakQueued.Load(),
		AcceptedTLSConnections: h.acceptedTLSConnections.Load(),
		RequestBodyBytes:       h.requestBodyBytes.Load(), ResponseBodyBytes: h.responseBodyBytes.Load(),
		ProfileTCPConnections: profileConnections, ProfileReceived: profileReceived, ProfileCompleted: profileCompleted,
		ProfileRequestBytes: profileRequestBytes, ProfileResponseBytes: profileResponseBytes,
	}
}

func snapshotProfileCounts(counts map[string]*atomic.Int64) map[string]int64 {
	result := make(map[string]int64, len(counts))
	for id, count := range counts {
		result[id] = count.Load()
	}
	return result
}

func (h *Handler) incrementProfile(counts map[string]*atomic.Int64, id string) {
	h.addProfile(id, counts, 1)
}

func (h *Handler) addProfile(id string, counts map[string]*atomic.Int64, value int64) {
	if count := counts[id]; count != nil {
		count.Add(value)
	}
}

func (h *Handler) recordTCPConnection(r *http.Request) {
	connectionID, ok := r.Context().Value(acceptedConnectionContextKey{}).(int64)
	if !ok {
		return
	}
	h.connectionsMu.Lock()
	defer h.connectionsMu.Unlock()
	counterID := r.URL.Path
	if profile, ok := h.profiles[r.URL.Path]; ok && profile.CounterID != "" {
		counterID = profile.CounterID
	}
	connections := h.tcpConnectionIDsByPath[counterID]
	if connections == nil {
		connections = make(map[int64]struct{})
		h.tcpConnectionIDsByPath[counterID] = connections
	}
	connections[connectionID] = struct{}{}
}

func peak(maximum *atomic.Int64, value int64) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}
