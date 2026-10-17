package reference

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

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
