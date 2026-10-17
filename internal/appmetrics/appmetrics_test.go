package appmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectAggregatesLabelledSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# HELP requests Requests\nrequests_total{route=\"a\"} 2\nrequests_total{route=\"b\"} 3\nqueue_depth 4\n"))
	}))
	defer server.Close()
	snapshot, err := Collect(context.Background(), server.URL+"/metrics")
	if err != nil || len(snapshot.Samples) != 2 || snapshot.Samples[1].Name != "requests_total" || snapshot.Samples[1].Value != 5 {
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
