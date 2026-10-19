package reportui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestHandlerRendersReport(t *testing.T) {
	h := Handler(report.Result{Name: "Local report", SchemaVersion: 13, Outcome: "PASS", ObservationSeconds: 2, Config: report.RunConfig{MaxRPS: 5}, Metrics: report.MetricSet{Total: 2, P95MS: 100, HTTP: report.HTTPDiagnostics{StatusCodes: map[int]int{429: 1}, Status4xx: 1, RateLimitedResponses: 1}}, Journeys: report.MetricSet{Total: 1, Success: 1, P95MS: 250}, ObservedThinkTimeMS: 60, ThinkTimeObservations: 1, Calibration: &report.CalibrationEvidence{Coverage: calibration.Coverage{Status: "INCONCLUSIVE", Dimensions: []calibration.Dimension{{Name: "observed_response_size", Status: "MATCHED", Reason: "every step matched"}}}}, Quality: &report.Quality{State: report.QualityLimited, Findings: []string{"endpoint identity remains unverified"}}, Steps: []report.StepResult{{Name: "page", Metrics: report.MetricSet{ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: 2, MinBytes: 12, MaxBytes: 40, TotalBytes: 52}}, HTTPTrace: report.HTTPTrace{FirstByteCount: 1, FirstByteMS: 12, TLSCount: 1, TLSMS: 3}}}, Windows: []report.WindowResult{{StartSeconds: 0, EndSeconds: 1, Steps: []report.WindowMetric{{Name: "page", Metrics: report.MetricSet{Total: 2, P95MS: 120}}}, Profiles: []report.WindowMetric{{Name: "checkout", Metrics: report.MetricSet{Total: 1, P95MS: 300}}}}}})
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
	for _, expected := range []string{"HTTP requests/s", "Successful configured journeys/s", "0.50", "max_rps ceiling: 5", "Cancelled / incomplete journeys"} {
		if !strings.Contains(w.Body.String(), expected) {
			t.Fatalf("report view omitted throughput evidence %q: %s", expected, w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), "2 (12-40 bytes, 52 bytes)") {
		t.Fatalf("report view omitted response-body shape observations: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Calibration coverage: INCONCLUSIVE") || !strings.Contains(w.Body.String(), "observed_response_size") || !strings.Contains(w.Body.String(), "Measurement quality: LIMITED") {
		t.Fatalf("report view omitted calibration dimension or quality state: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "0-1s") || !strings.Contains(w.Body.String(), "step page") || !strings.Contains(w.Body.String(), "profile checkout") {
		t.Fatalf("report view omitted per-step or per-profile window evidence: %s", w.Body.String())
	}
}

func TestListenAddressRejectsNonLoopback(t *testing.T) {
	if ListenAddress("0.0.0.0:8787") == nil {
		t.Fatal("public bind accepted")
	}
}
