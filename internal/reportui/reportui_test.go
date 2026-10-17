package reportui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestHandlerRendersReport(t *testing.T) {
	h := Handler(report.Result{Name: "Local report", Outcome: "PASS", Metrics: report.MetricSet{Total: 2, P95MS: 100, HTTP: report.HTTPDiagnostics{StatusCodes: map[int]int{429: 1}, Status4xx: 1, RateLimitedResponses: 1}}, Journeys: report.MetricSet{Total: 1, P95MS: 250}, ObservedThinkTimeMS: 60, ThinkTimeObservations: 1, Steps: []report.StepResult{{Name: "page", HTTPTrace: report.HTTPTrace{FirstByteCount: 1, FirstByteMS: 12, TLSCount: 1, TLSMS: 3}}}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Local report") || !strings.Contains(w.Body.String(), "page") {
		t.Fatalf("unexpected page: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "First byte") || !strings.Contains(w.Body.String(), "12.0 ms") || !strings.Contains(w.Body.String(), "3.0 ms (1)") {
		t.Fatalf("report view omitted client phase timings: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "250.0 ms") || !strings.Contains(w.Body.String(), "60.0 ms") || !strings.Contains(w.Body.String(), "think-time") {
		t.Fatalf("report view omitted journey and think-time boundaries: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "429 responses 1") || !strings.Contains(w.Body.String(), "4xx 1") {
		t.Fatalf("report view omitted HTTP outcome diagnostics: %s", w.Body.String())
	}
}

func TestListenAddressRejectsNonLoopback(t *testing.T) {
	if ListenAddress("0.0.0.0:8787") == nil {
		t.Fatal("public bind accepted")
	}
}
