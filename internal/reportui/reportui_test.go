package reportui

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestHandlerRendersReport(t *testing.T) {
	h := Handler(report.Result{Name: "Local report", SchemaVersion: 13, Outcome: "PASS", ObservationSeconds: 2, ArrivalsScheduled: 12, ArrivalsDropped: 2, ArrivalsDroppedBusy: 1, ArrivalsDroppedLate: 1, Generator: &generatorhealth.Summary{Samples: 8, PeakHeapAllocBytes: 1024, PeakGoroutines: 4, PeakProcessRSSBytes: 2048}, Config: report.RunConfig{MaxRPS: 5, P95LimitMS: 200, MaxErrorRate: 0.05}, Metrics: report.MetricSet{Total: 2, P95MS: 100, HTTP: report.HTTPDiagnostics{StatusCodes: map[int]int{429: 1}, Status4xx: 1, RateLimitedResponses: 1}}, Journeys: report.MetricSet{Total: 1, Success: 1, P95MS: 250}, ObservedThinkTimeMS: 60, ThinkTimeObservations: 1, Calibration: &report.CalibrationEvidence{Coverage: calibration.Coverage{Status: "INCONCLUSIVE", Dimensions: []calibration.Dimension{{Name: "observed_response_size", Status: "MATCHED", Reason: "every step matched"}}}}, Quality: &report.Quality{State: report.QualityLimited, Findings: []string{"endpoint identity remains unverified"}}, Steps: []report.StepResult{{Name: "page", Metrics: report.MetricSet{ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: 2, MinBytes: 12, MaxBytes: 40, TotalBytes: 52}}, HTTPTrace: report.HTTPTrace{FirstByteCount: 1, FirstByteMS: 12, TLSCount: 1, TLSMS: 3}}}, Windows: []report.WindowResult{
		{StartSeconds: 0, EndSeconds: 1, Metrics: report.MetricSet{Total: 10, Success: 9, Failures: 1, P95MS: 120, P99MS: 250}, Journeys: report.MetricSet{Total: 5, Success: 4, Failures: 1, P95MS: 300, P99MS: 450}, Steps: []report.WindowMetric{{Name: "page", Metrics: report.MetricSet{Total: 2, Success: 2, P95MS: 120, P99MS: 200}}}, Profiles: []report.WindowMetric{{Name: "checkout", Metrics: report.MetricSet{Total: 1, Success: 1, P95MS: 300, P99MS: 400}}}},
		{StartSeconds: 1, EndSeconds: 2, Metrics: report.MetricSet{Total: 10, Success: 10, P95MS: 80, P99MS: 100}, Journeys: report.MetricSet{Total: 4, Success: 4, P95MS: 180, P99MS: 220}},
	}})
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
	for _, expected := range []string{"Observation-window timeline", "Request p95 and p99 by observation window", `points="182.5,89.8 457.5,108.2"`, `points="182.5,30.0 457.5,99.0"`, "0-1s", "1-2s", "aggregate journeys", "Success", "Run-wide context (not aligned to windows)", "dropped starts: 2", "peak process RSS 2048 bytes"} {
		if !strings.Contains(w.Body.String(), expected) {
			t.Fatalf("report view omitted timeline evidence %q: %s", expected, w.Body.String())
		}
	}
}

func TestHandlerPlotsApplicationMetricsAsObservedPoints(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeline := &appmetrics.Timeline{SchemaVersion: 1, Source: "http://127.0.0.1", Interval: "5s",
		StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(10 * time.Second).Format(time.RFC3339Nano), Note: "test",
		Samples: []appmetrics.TimedSample{
			{StartedAt: start.Add(time.Second).Format(time.RFC3339Nano), FinishedAt: start.Add(time.Second).Format(time.RFC3339Nano), Samples: []appmetrics.Sample{{Name: "queue_depth", Value: 3}}},
			{StartedAt: start.Add(9 * time.Second).Format(time.RFC3339Nano), FinishedAt: start.Add(9 * time.Second).Format(time.RFC3339Nano), Samples: []appmetrics.Sample{{Name: "queue_depth", Value: 7}}},
		},
	}
	h := Handler(report.Result{Name: "Metric report", ApplicationMetrics: timeline})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	for _, expected := range []string{"Application-metric timeline", "queue_depth", `cx="100.0" cy="140.0"`, `cx="540.0" cy="30.0"`, "without Prometheus labels", "not mapped to Damocline capacity"} {
		if w.Code != http.StatusOK || !strings.Contains(body, expected) {
			t.Fatalf("report view omitted observed metric evidence %q: %d %s", expected, w.Code, body)
		}
	}
	if strings.Contains(body, `polyline fill="none"`) {
		t.Fatal("sparse application observations were connected by an invented interpolation")
	}
}

func TestCampaignHandlerRendersSeparatePhaseEvidenceAndUnits(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeline := &appmetrics.Timeline{SchemaVersion: 1, Source: "http://127.0.0.1", Interval: "5s",
		StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(10 * time.Second).Format(time.RFC3339Nano), Note: "test",
		Samples: []appmetrics.TimedSample{{StartedAt: start.Add(time.Second).Format(time.RFC3339Nano), FinishedAt: start.Add(time.Second).Format(time.RFC3339Nano), Samples: []appmetrics.Sample{{Name: "workers_busy", Value: 2}}}},
	}
	h := CampaignHandler(CampaignView{Kind: "Open-arrival capacity", Unit: "scheduled journey starts/s", Scenario: "Synthetic", Target: "http://127.0.0.1", StartedAt: start.Format(time.RFC3339), Outcome: "INCONCLUSIVE", Pending: true,
		Trials: []TrialView{{Number: 1, Outcome: "IN_PROGRESS", Pending: true, Phases: []PhaseView{{Name: "observation", EvidenceState: "INSUFFICIENT_EVIDENCE", Load: "5 scheduled journey starts/s", Run: report.Result{Outcome: "PASS", ObservationSeconds: 10, Config: report.RunConfig{Mode: "arrival", ArrivalRate: 5}, Metrics: report.MetricSet{Total: 12, P95MS: 30}}, Metrics: MetricCharts(timeline)}}}}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	for _, expected := range []string{"Open-arrival capacity", "scheduled journey starts/s", "checkpoint in progress", "5 scheduled journey starts/s", "workers_busy", "Application metric: workers_busy", "12 HTTP requests", "No interpolation"} {
		if w.Code != http.StatusOK || !strings.Contains(body, expected) {
			t.Fatalf("campaign dashboard omitted evidence %q: %d %s", expected, w.Code, body)
		}
	}
}

func TestMetricChartsKeepExtremeFiniteValuesInsideThePlot(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeline := &appmetrics.Timeline{StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(time.Second).Format(time.RFC3339Nano),
		Samples: []appmetrics.TimedSample{
			{FinishedAt: start.Format(time.RFC3339Nano), Samples: []appmetrics.Sample{{Name: "extreme", Value: -math.MaxFloat64}}},
			{FinishedAt: start.Add(time.Second).Format(time.RFC3339Nano), Samples: []appmetrics.Sample{{Name: "extreme", Value: math.MaxFloat64}}},
		},
	}
	charts := MetricCharts(timeline)
	if len(charts) != 1 || len(charts[0].Points) != 2 || charts[0].Points[0].Y != "140.0" || charts[0].Points[1].Y != "30.0" {
		t.Fatalf("finite extreme values produced invalid chart coordinates: %+v", charts)
	}
}

func TestListenAddressRejectsNonLoopback(t *testing.T) {
	if ListenAddress("0.0.0.0:8787") == nil {
		t.Fatal("public bind accepted")
	}
}
