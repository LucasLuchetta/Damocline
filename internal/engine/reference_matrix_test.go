package engine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// TestNativeEngineReferencePayloadMatrix exercises the ACTUAL load scheduler
// against a local controlled fixture, unlike protocolcheck's Go-transport-only
// matrix. This only qualifies bounded HTTP loopback delivery, not HTTPS,
// browser timing, remote DNS, generator maxima or production capacity.
func TestNativeEngineReferencePayloadMatrix(t *testing.T) {
	cases := []struct {
		name       string
		bytes      int
		delay      time.Duration
		extraEvery int64
		extraDelay time.Duration
		closeEvery int64
		emptyBody  bool
	}{
		{"empty-body", 0, 10 * time.Millisecond, 0, 0, 0, true},
		{"1k", 1024, 10 * time.Millisecond, 0, 0, 0, false},
		{"4k", 4096, 10 * time.Millisecond, 0, 0, 0, false},
		{"64k", 65536, 10 * time.Millisecond, 0, 0, 0, false},
		{"latency-tail", 4096, 10 * time.Millisecond, 3, 80 * time.Millisecond, 0, false},
		{"connection-close", 4096, 10 * time.Millisecond, 0, 0, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := reference.New(reference.Config{
				Delay: tc.delay, ExtraEvery: tc.extraEvery,
				ExtraDelay: tc.extraDelay, ResponseBytes: tc.bytes, EmptyResponse: tc.emptyBody,
				CloseEvery: tc.closeEvery, MaxConcurrent: 10, MaxRequests: 100,
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(fixture)
			defer server.Close()
			s := config.Scenario{
				Name: "bounded local reference", BaseURL: server.URL,
				Users: 10, Duration: "2s", Mode: "arrival", ArrivalRate: 5,
				MaxRPS: 20, Timeout: "1s", P95LimitMS: 900,
				MaxErrorRate: 0, MinSamplesPerStep: 1,
				Steps: []config.Step{{Name: "reference", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}},
			}
			v, err := config.Parse(s, config.Options{Authorized: true})
			if err != nil {
				t.Fatal(err)
			}
			out := Run(context.Background(), v)
			ledger := fixture.Snapshot()
			const expected = 10
			if out.ArrivalsScheduled != expected || out.IterationsStarted != expected ||
				out.IterationsCompleted != expected || out.ArrivalsDropped != 0 ||
				out.Metrics.Total != expected || out.Metrics.Success != expected ||
				out.Metrics.Failures != 0 || out.Outcome != "PASS" ||
				ledger.Received != expected || ledger.Completed != expected ||
				ledger.Success != expected || ledger.Cancelled != 0 ||
				ledger.Failures != 0 || ledger.Rejected != 0 {
				t.Fatalf("%s: native engine and independent server ledger disagree: %+v %+v",
					tc.name, out, ledger)
			}
			if out.Metrics.ResponseBody.CompleteObservations != expected ||
				out.Metrics.ResponseBody.MinBytes != int64(tc.bytes) ||
				out.Metrics.ResponseBody.MaxBytes != int64(tc.bytes) ||
				out.Metrics.ResponseBody.TotalBytes != int64(expected*tc.bytes) {
				t.Fatalf("%s: complete response-body byte ledger differs from fixture: %+v", tc.name, out.Metrics.ResponseBody)
			}
			if out.Quality == nil || out.Quality.State != report.QualityLimited {
				t.Fatalf("%s: passing fixture cannot imply validated arbitrary capacity: %+v",
					tc.name, out.Quality)
			}
		})
	}
}

func TestConnectionPolicyMatchesIndependentServerConnectionEvents(t *testing.T) {
	for _, policy := range []string{"reuse", "new_per_request"} {
		t.Run(policy, func(t *testing.T) {
			fixture, err := reference.New(reference.Config{Delay: 5 * time.Millisecond, ResponseBytes: 1, MaxConcurrent: 10, MaxRequests: 100})
			if err != nil {
				t.Fatal(err)
			}
			var accepted atomic.Int64
			server := httptest.NewUnstartedServer(fixture)
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					accepted.Add(1)
				}
			}
			server.Start()
			defer server.Close()
			scenario := config.Scenario{
				Name: "bounded local connection-policy fixture", BaseURL: server.URL,
				Users: 10, Duration: "2s", Mode: "arrival", ArrivalRate: 5,
				MaxRPS: 20, Timeout: "1s", P95LimitMS: 900,
				ConnectionPolicy: policy,
				Steps:            []config.Step{{Name: "work", Method: "GET", Path: "/work"}},
			}
			validated, err := config.Parse(scenario, config.Options{Authorized: true})
			if err != nil {
				t.Fatal(err)
			}
			result := Run(context.Background(), validated)
			serverLedger := fixture.Snapshot()
			if result.Metrics.Success != 10 || result.ArrivalsDropped != 0 || serverLedger.Received != 10 || serverLedger.Completed != 10 {
				t.Fatalf("request ledgers did not reconcile: result=%+v server=%+v", result, serverLedger)
			}
			connections := accepted.Load()
			if policy == "reuse" && connections >= 10 {
				t.Fatalf("reusable policy opened %d connections for 10 requests", connections)
			}
			if policy == "new_per_request" && connections != 10 {
				t.Fatalf("new-per-request policy opened %d connections for 10 requests", connections)
			}
			if result.Config.ConnectionPolicy != policy {
				t.Fatalf("report omitted connection policy: %+v", result.Config)
			}
		})
	}
}
