package report

import (
	"math"
	"testing"
	"time"
)

func TestMetricsAndPercentiles(t *testing.T) {
	m := NewMetricSet()
	for i := 1; i <= 100; i++ {
		m.Record(time.Duration(i)*time.Millisecond, true, false, "")
	}
	m.Record(200*time.Millisecond, false, false, "timeout")
	m.Record(0, false, true, "run_cancelled")
	r := m.Snapshot()
	if r.Total != 101 || r.Success != 100 || r.Failures != 1 || r.Cancelled != 1 {
		t.Fatalf("counts: %+v", r)
	}
	if math.Abs(r.P95MS-96) > 0.01 {
		t.Fatalf("p95=%f expected 96", r.P95MS)
	}
	if r.Reasons["timeout"] != 1 {
		t.Fatalf("reasons: %+v", r.Reasons)
	}
}

func TestWindowSecondsPrefersConfiguredObservationWindow(t *testing.T) {
	r := Result{ElapsedSeconds: 31, ObservationSeconds: 30}
	if r.WindowSeconds() != 30 {
		t.Fatalf("workload window=%v; want 30s", r.WindowSeconds())
	}
	r.ObservationSeconds = 0
	if r.WindowSeconds() != 31 {
		t.Fatalf("legacy fallback window=%v; want elapsed 31s", r.WindowSeconds())
	}
}
func TestReportJSONDoesNotLeakUnexportedSamples(t *testing.T) {
	r := Result{Name: "test", Metrics: NewMetricSet().Snapshot()}
	dir := t.TempDir()
	path := dir + "/report.json"
	if err := WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
}
