package labtarget

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKnownQueueRestrictionAndMetricSeries(t *testing.T) {
	handler, err := NewHandler(Options{Delay: 300 * time.Millisecond, Parallel: 1, QueueLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	firstDone := make(chan error, 1)
	go func() {
		response, err := http.Get(server.URL + "/work")
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = io.ErrUnexpectedEOF
			}
		}
		firstDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		response, err := http.Get(server.URL + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.Contains(string(body), "lab_active 1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first synthetic request never entered its work slot")
		}
		time.Sleep(5 * time.Millisecond)
	}
	response, err := http.Get(server.URL + "/work")
	if err != nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("queue restriction not observed: response=%v err=%v", response, err)
	}
	response.Body.Close()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), "lab_requests_total{outcome=\"completed\"} 1") || !strings.Contains(string(body), "lab_requests_total{outcome=\"rejected\"} 1") || !strings.Contains(string(body), "# TYPE process_start_time_seconds gauge") || !strings.Contains(string(body), "# UNIT process_start_time_seconds seconds") {
		t.Fatalf("known outcomes missing from exporter: %s", body)
	}
}
