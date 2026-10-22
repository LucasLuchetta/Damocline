package appmetrics

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectPreservesLabelledSeriesAndMetricMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# HELP requests Requests\n# TYPE requests_total counter\n# UNIT requests_total requests\nrequests_total{route=\"a\"} 2\nrequests_total{route=\"b\"} 3\nqueue_depth 4\n"))
	}))
	defer server.Close()
	snapshot, err := Collect(context.Background(), server.URL+"/metrics")
	if err != nil || len(snapshot.Samples) != 3 || snapshot.Samples[0].Name != "queue_depth" || snapshot.Samples[0].Value != 4 || snapshot.Samples[1].Name != "requests_total" || snapshot.Samples[2].Name != "requests_total" || snapshot.Samples[1].Value+snapshot.Samples[2].Value != 5 || snapshot.Samples[1].Type != "counter" || snapshot.Samples[2].Type != "counter" || snapshot.Samples[1].Unit != "requests" || snapshot.Samples[2].Unit != "requests" || snapshot.Samples[1].SeriesID == "" || snapshot.Samples[2].SeriesID == "" || snapshot.Samples[1].SeriesID == snapshot.Samples[2].SeriesID || snapshot.Source != server.URL {
		t.Fatalf("unexpected snapshot: %+v err=%v", snapshot, err)
	}
}

func TestCollectRejectsNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	if _, err := Collect(context.Background(), server.URL); err == nil {
		t.Fatal("non-OK endpoint was accepted")
	}
}

func TestCollectRejectsCredentialBearingEndpointAndOversizedBody(t *testing.T) {
	for _, endpoint := range []string{"http://user:secret@127.0.0.1/metrics", "http://127.0.0.1/metrics?token=secret", "http://127.0.0.1/metrics#token"} {
		if _, err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("credential-bearing endpoint was accepted: %q", endpoint)
		}
	}
	if _, err := parse(bytes.NewReader([]byte(strings.Repeat("x", maxBodyBytes+1)))); err == nil {
		t.Fatal("oversized metrics body was accepted")
	}
}

func TestParseRejectsUnboundedOrInvalidMetricSets(t *testing.T) {
	tooMany := make([]string, 0, maxMetricNames+1)
	for i := 0; i <= maxMetricNames; i++ {
		tooMany = append(tooMany, fmt.Sprintf("metric_%d 1", i))
	}
	for _, input := range []string{"metric +Inf", "metric NaN", strings.Join(tooMany, "\n"), strings.Repeat("a", maxMetricNameBytes+1) + " 1"} {
		if _, err := parse(strings.NewReader(input)); err == nil {
			t.Errorf("invalid metrics input was accepted: %.80q", input)
		}
	}
	if _, err := parse(strings.NewReader("metric 1e308\nmetric 1e308")); err == nil {
		t.Fatal("duplicate series was accepted")
	}
}

func TestCaptureDuringCollectsBoundedTimestampedTimeline(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("requests_total{route=\"synthetic\"} 4\nqueue_depth 2\n"))
	}))
	defer server.Close()

	result, timeline := CaptureDuring(context.Background(), server.URL+"/metrics", time.Second, NewBudget(3), func() string {
		time.Sleep(1200 * time.Millisecond)
		return "run-result"
	})
	if result != "run-result" || requests.Load() < 1 || len(timeline.Samples) < 1 || timeline.FailedCollections != 0 {
		t.Fatalf("timeline did not retain successful samples: result=%q requests=%d timeline=%+v", result, requests.Load(), timeline)
	}
	if timeline.State() != "COLLECTED" || ValidateTimeline(timeline) != nil {
		t.Fatalf("captured timeline is invalid: state=%s err=%v timeline=%+v", timeline.State(), ValidateTimeline(timeline), timeline)
	}
	for _, sample := range timeline.Samples {
		if len(sample.Samples) != 2 || sample.Samples[0].Name != "queue_depth" || sample.Samples[1].Value != 4 {
			t.Fatalf("unexpected sanitized sample: %+v", sample)
		}
	}
}

func TestCaptureDuringSharesCampaignBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("up 1\n")) }))
	defer server.Close()
	budget := NewBudget(1)
	_, first := CaptureDuring(context.Background(), server.URL, time.Second, budget, func() struct{} {
		time.Sleep(1100 * time.Millisecond)
		return struct{}{}
	})
	_, second := CaptureDuring(context.Background(), server.URL, time.Second, budget, func() struct{} {
		time.Sleep(10 * time.Millisecond)
		return struct{}{}
	})
	if len(first.Samples) != 1 || first.State() != "PARTIAL" || !first.CollectionLimitHit || !second.CollectionLimitHit || second.State() != "UNAVAILABLE" {
		t.Fatalf("campaign-wide collection budget was not enforced: first=%+v second=%+v", first, second)
	}
	if err := ValidateTimeline(second); err != nil {
		t.Fatalf("budget-exhausted timeline should remain valid: %v", err)
	}
}

func TestCaptureDuringEnforcesSerializedByteBudget(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("queue_depth 5\n"))
		received <- struct{}{}
	}))
	defer server.Close()
	budget := NewBudget(2)
	budget.remainingBytes = 1
	_, timeline := CaptureDuring(context.Background(), server.URL, time.Second, budget, func() struct{} {
		select {
		case <-received:
			time.Sleep(20 * time.Millisecond)
		case <-time.After(2 * time.Second):
			t.Fatal("scrape did not reach local fixture")
		}
		return struct{}{}
	})
	if len(timeline.Samples) != 0 || !timeline.CollectionLimitHit || timeline.State() != "UNAVAILABLE" {
		t.Fatalf("byte ceiling did not stop timeline storage: %+v", timeline)
	}
	if err := ValidateTimeline(timeline); err != nil {
		t.Fatalf("bounded timeline should remain valid: %v", err)
	}
}
