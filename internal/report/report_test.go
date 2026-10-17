package report

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAnyJSONRejectsOversizedReportWithoutCreatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, []byte("existing report"), 0600); err != nil {
		t.Fatal(err)
	}
	data := map[string]string{"payload": strings.Repeat("x", maxReportBytes)}
	if err := WriteAnyJSON(path, data); err == nil || !strings.Contains(err.Error(), "16 MiB output limit") {
		t.Fatalf("oversized report was not rejected clearly: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil || string(written) != "existing report" {
		t.Fatalf("oversized report changed the existing output: %q err=%v", written, err)
	}
}

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
	if r.P95IID95Approximation {
		t.Fatal("p95 rank interval must be absent when the expected tail has fewer than ten samples")
	}
	if r.Reasons["timeout"] != 1 {
		t.Fatalf("reasons: %+v", r.Reasons)
	}
}

func TestP95IIDRankIntervalUsesSortedLatencyRanks(t *testing.T) {
	m := NewMetricSet()
	for i := 1; i <= 1_000; i++ {
		m.Record(time.Duration(i)*time.Millisecond, true, false, "")
	}
	r := m.Snapshot()
	if !r.P95IID95Approximation || r.P95IID95LowerMS != 937 || r.P95IID95UpperMS != 964 {
		t.Fatalf("unexpected p95 IID interval: %+v", r)
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
