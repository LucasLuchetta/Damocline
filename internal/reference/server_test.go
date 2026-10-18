package reference

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestControlSnapshotDoesNotEnterWorkRequestLedger(t *testing.T) {
	h, err := New(Config{ResponseBytes: 4096, MaxConcurrent: 1, MaxRequests: 10})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__damocline_snapshot", ServeReferenceControl(h))
	mux.Handle("/", h)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	response, err := http.Get(srv.URL + "/__damocline_snapshot")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	err = json.NewDecoder(response.Body).Decode(&snapshot)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || snapshot != (Snapshot{}) {
		t.Fatalf("control read altered or failed to read the work ledger: status=%d snapshot=%+v err=%v", response.StatusCode, snapshot, err)
	}
	response, err = http.Get(srv.URL + "/work")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || h.Snapshot().Received != 1 || h.Snapshot().Completed != 1 {
		t.Fatalf("work request was not independently counted: status=%d snapshot=%+v", response.StatusCode, h.Snapshot())
	}
}

func TestBoundedQueueAndRecovery(t *testing.T) {
	h, err := New(Config{Delay: 80 * time.Millisecond, FailureFirst: 1, MaxConcurrent: 1, MaxRequests: 5})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	var wg sync.WaitGroup
	status := make(chan int, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: time.Second}
			res, err := client.Get(srv.URL + "/work")
			if err != nil {
				status <- 0
				return
			}
			defer res.Body.Close()
			status <- res.StatusCode
		}()
	}
	wg.Wait()
	close(status)
	counts := map[int]int{}
	for code := range status {
		counts[code]++
	}
	snapshot := h.Snapshot()
	if counts[http.StatusServiceUnavailable] != 1 || counts[http.StatusOK] != 4 ||
		snapshot.PeakConcurrent != 1 || snapshot.PeakQueued < 2 || snapshot.Received != 5 || snapshot.Completed != 5 {
		t.Fatalf("unexpected reference behavior: statuses=%v snapshot=%+v", counts, snapshot)
	}
}

func TestRejectUnboundedReferenceWorkload(t *testing.T) {
	if _, err := New(Config{MaxConcurrent: 51, MaxRequests: 1}); err == nil {
		t.Fatal("accepted unbounded concurrency")
	}
	if _, err := New(Config{MaxConcurrent: 1, MaxRequests: 0}); err == nil {
		t.Fatal("accepted unbounded request count")
	}
	if _, err := New(Config{MaxConcurrent: 1, MaxRequests: 1, ResponseBytes: 64<<10 + 1}); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestKnownBodyAndConnectionClose(t *testing.T) {
	h, err := New(Config{ResponseBytes: 128, CloseEvery: 2, MaxConcurrent: 1, MaxRequests: 2})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := &http.Client{Timeout: time.Second}
	for i := 1; i <= 2; i++ {
		res, err := client.Get(srv.URL + "/work")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(res.Body)
		closeErr := res.Body.Close()
		if readErr != nil || closeErr != nil || res.StatusCode != http.StatusOK || len(body) != 128 || res.Close != (i == 2) {
			t.Fatalf("request %d: status=%d bytes=%d close=%t read=%v body-close=%v", i, res.StatusCode, len(body), res.Close, readErr, closeErr)
		}
	}
	s := h.Snapshot()
	if s.Received != 2 || s.Completed != 2 || s.Success != 2 || s.PeakQueued != 0 {
		t.Fatalf("unexpected counters: %+v", s)
	}
}

func TestWorkloadProfilesReconcileRequestAndResponsePayloadBytes(t *testing.T) {
	profiles := []WorkloadProfile{
		{ID: "get-empty", Method: http.MethodGet, Path: "/matrix/get-empty", ResponseBodyBytes: 0},
		{ID: "get-1k", Method: http.MethodGet, Path: "/matrix/get-1k", ResponseBodyBytes: 1 << 10},
		{ID: "get-64k", Method: http.MethodGet, Path: "/matrix/get-64k", ResponseBodyBytes: 64 << 10},
		{ID: "post-4k", Method: http.MethodPost, Path: "/matrix/post-4k", RequestBodyBytes: 4 << 10, ResponseBodyBytes: 4 << 10},
	}
	h, err := New(Config{Delay: time.Millisecond, MaxConcurrent: 2, MaxRequests: 10, Profiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	client := &http.Client{Timeout: time.Second}
	var expectedResponseBytes int64
	for _, profile := range profiles {
		requestBody := make([]byte, profile.RequestBodyBytes)
		request, err := http.NewRequest(profile.Method, srv.URL+profile.Path, bytes.NewReader(requestBody))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || len(body) != profile.ResponseBodyBytes {
			t.Fatalf("profile %s response: status=%d bytes=%d read=%v close=%v", profile.ID, response.StatusCode, len(body), readErr, closeErr)
		}
		expectedResponseBytes += int64(profile.ResponseBodyBytes)
	}

	snapshot := h.Snapshot()
	if snapshot.Received != int64(len(profiles)) || snapshot.Completed != int64(len(profiles)) ||
		snapshot.Success != int64(len(profiles)) || snapshot.Failures != 0 ||
		snapshot.RequestBodyBytes != 4<<10 || snapshot.ResponseBodyBytes != expectedResponseBytes {
		t.Fatalf("profile byte counters did not reconcile independently: %+v", snapshot)
	}
}

func TestWorkloadProfilesRejectMismatchedRequestBodyLength(t *testing.T) {
	h, err := New(Config{MaxConcurrent: 1, MaxRequests: 10, Profiles: []WorkloadProfile{
		{ID: "post-4k", Method: http.MethodPost, Path: "/matrix/post-4k", RequestBodyBytes: 4 << 10, ResponseBodyBytes: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	response, err := http.Post(srv.URL+"/matrix/post-4k", "application/octet-stream", bytes.NewReader(make([]byte, (4<<10)-1)))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("mismatched payload returned %d, want %d", response.StatusCode, http.StatusRequestEntityTooLarge)
	}
	snapshot := h.Snapshot()
	if snapshot.Received != 1 || snapshot.Completed != 1 || snapshot.Failures != 1 || snapshot.Success != 0 {
		t.Fatalf("mismatched profile request was not reconciled: %+v", snapshot)
	}
}

func TestRejectUnboundedWorkloadProfileMatrix(t *testing.T) {
	profiles := make([]WorkloadProfile, 17)
	for i := range profiles {
		profiles[i] = WorkloadProfile{ID: "get", Method: http.MethodGet, Path: "/matrix/get"}
	}
	if _, err := New(Config{MaxConcurrent: 1, MaxRequests: 1, Profiles: profiles}); err == nil {
		t.Fatal("accepted too many workload profiles")
	}
	if _, err := New(Config{MaxConcurrent: 1, MaxRequests: 1, Profiles: []WorkloadProfile{
		{ID: "get", Method: http.MethodGet, Path: "/matrix/get?secret=value"},
	}}); err == nil {
		t.Fatal("accepted a query-bearing fixture path")
	}
}
