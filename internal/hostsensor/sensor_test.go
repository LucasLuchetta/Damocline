package hostsensor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
)

const testToken = "local-fixture-token"
const testExperiment = "experiment_12345678"

func testHandler(t *testing.T, collect Collector) http.Handler {
	t.Helper()
	handler, err := NewHandler(testToken, collect)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func fakeCollect(context.Context) (hostmon.Result, error) {
	return hostmon.Result{SchemaVersion: 1, Source: "linux:/proc", Samples: []hostmon.Sample{{At: time.Now().UTC().Format(time.RFC3339Nano), CPUPercent: 25, MemoryUsedBytes: 100, MemoryTotalBytes: 200}}}, nil
}

func TestHandlerRequiresAuthorizationAndExperimentID(t *testing.T) {
	server := httptest.NewServer(testHandler(t, fakeCollect))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+Path, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized request: response=%v err=%v", response, err)
	}
	response.Body.Close()
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing ID: response=%v err=%v", response, err)
	}
	response.Body.Close()
	request.Header.Set("X-Damocline-Experiment-ID", testExperiment)
	response, err = http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("valid request: response=%v err=%v", response, err)
	}
	defer response.Body.Close()
	var reading Reading
	if err := json.NewDecoder(response.Body).Decode(&reading); err != nil || reading.ExperimentID != testExperiment || reading.SchemaVersion != 1 || len(reading.Host.Samples) != 1 {
		t.Fatalf("invalid reading: %+v err=%v", reading, err)
	}
}

func TestHandlerRejectsBodyAndQuery(t *testing.T) {
	handler := testHandler(t, fakeCollect)
	for _, path := range []string{Path + "?secret=1", Path} {
		request := httptest.NewRequest(http.MethodGet, path, strings.NewReader("x"))
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("X-Damocline-Experiment-ID", testExperiment)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("request with body/query accepted: %s (%d)", path, response.Code)
		}
	}
}

func TestHandlerBoundsConcurrentCollection(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	handler := testHandler(t, func(ctx context.Context) (hostmon.Result, error) {
		close(entered)
		select {
		case <-release:
			return fakeCollect(ctx)
		case <-ctx.Done():
			return hostmon.Result{}, ctx.Err()
		}
	})
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, Path, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("X-Damocline-Experiment-ID", testExperiment)
		return r
	}
	first := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(first, request()); close(done) }()
	<-entered
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request())
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent collection was not bounded: %d", second.Code)
	}
	close(release)
	<-done
}

func TestValidateBindRequiresLinuxAndTLSForRemote(t *testing.T) {
	for _, tc := range []struct {
		goos, addr, cert, key string
		valid                 bool
	}{
		{"linux", "127.0.0.1:9467", "", "", true},
		{"linux", "0.0.0.0:9467", "", "", false},
		{"linux", "0.0.0.0:9467", "cert.pem", "key.pem", true},
		{"windows", "127.0.0.1:9467", "", "", false},
	} {
		if got := validateBindForOS(tc.goos, tc.addr, tc.cert, tc.key); (got == nil) != tc.valid {
			t.Fatalf("bind validation for %+v: %v", tc, got)
		}
	}
}
