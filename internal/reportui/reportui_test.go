package reportui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestHandlerRendersReport(t *testing.T) {
	h := Handler(report.Result{Name: "Local report", Outcome: "PASS", Metrics: report.MetricSet{Total: 2, P95MS: 100}, Steps: []report.StepResult{{Name: "page"}}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Local report") || !strings.Contains(w.Body.String(), "page") {
		t.Fatalf("unexpected page: %d %s", w.Code, w.Body.String())
	}
}

func TestListenAddressRejectsNonLoopback(t *testing.T) {
	if ListenAddress("0.0.0.0:8787") == nil {
		t.Fatal("public bind accepted")
	}
}
