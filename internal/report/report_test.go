package report

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

func TestP99RankIntervalStatesAndSchemaValidation(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want P99IntervalState
	}{{n: 999, want: P99IntervalInsufficient}, {n: 1_000, want: P99IntervalConditionalIID}, {n: 1_000_001, want: P99IntervalResourceLimit}} {
		metrics := MetricSet{Total: tc.n, P99MS: 99}
		if tc.n == 1_000 {
			metrics.P99IID95Exact, metrics.P99IID95LowerMS, metrics.P99IID95UpperMS = true, 90, 100
		}
		if got := metrics.P99RankIntervalState(); got != tc.want {
			t.Errorf("n=%d: state=%s want %s", tc.n, got, tc.want)
		}
	}
	base := Result{SchemaVersion: CurrentSchemaVersion, StatisticalMethods: CurrentStatisticalMethods(), Metrics: MetricSet{Total: 1_000, P99MS: 99, P99IID95Exact: true, P99IID95LowerMS: 90, P99IID95UpperMS: 100}}
	if !HasValidStatisticalMethods(base) {
		t.Fatal("valid schema-v32 p99 interval rejected")
	}
	base.Metrics.P99IID95Exact = false
	if HasValidStatisticalMethods(base) {
		t.Fatal("adequate schema-v32 metric set without its required interval accepted")
	}
	base.Metrics.P99IID95Exact = true
	base.Metrics.P95IID95Exact = true
	base.Metrics.P95MS, base.Metrics.P95IID95LowerMS, base.Metrics.P95IID95UpperMS = 80, 85, 90
	if HasValidStatisticalMethods(base) {
		t.Fatal("p95 interval that excludes the reported percentile was accepted")
	}
	legacy := Result{SchemaVersion: 31, StatisticalMethods: StatisticalMethodsV30(), Metrics: MetricSet{Total: 1_000}}
	if !HasValidStatisticalMethods(legacy) {
		t.Fatal("schema-v31 p95-only method metadata rejected")
	}
}

func TestWriteAnyJSONAtomicallyReplacesRestrictedReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, []byte("previous report"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnyJSON(path, map[string]string{"result": "complete"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil || saved["result"] != "complete" {
		t.Fatalf("replacement is not a complete JSON report: %s err=%v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("report permissions = %04o; want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "report.json" {
		t.Fatalf("temporary report was left behind: entries=%v err=%v", entries, err)
	}
}

func TestWriteAnyJSONFailurePreservesDestinationAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "existing")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnyJSON(path, map[string]string{"result": "complete"}); err == nil {
		t.Fatal("replacing a directory with a report unexpectedly succeeded")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("failed report replacement changed destination data: %q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "report.json" {
		t.Fatalf("temporary report was left behind: entries=%v err=%v", entries, err)
	}
}

func TestWriteAnyJSONMarshalFailurePreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	previous := []byte("previous report")
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnyJSON(path, map[string]any{"unsupported": make(chan int)}); err == nil {
		t.Fatal("unsupported JSON value unexpectedly serialized")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(previous) {
		t.Fatalf("marshal failure changed the existing report: %q err=%v", data, err)
	}
}

func TestWriteAnyJSONEnforcesExactSerializedByteLimit(t *testing.T) {
	base, err := json.MarshalIndent(map[string]string{"payload": ""}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes := maxReportBytes - len(base) - 1
	path := filepath.Join(t.TempDir(), "report.json")
	if err := WriteAnyJSON(path, map[string]string{"payload": strings.Repeat("x", payloadBytes)}); err != nil {
		t.Fatalf("report at exact serialized limit was rejected: %v", err)
	}
	if err := WriteAnyJSON(path, map[string]string{"payload": strings.Repeat("x", payloadBytes+1)}); err == nil {
		t.Fatal("report over exact serialized byte limit was accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != maxReportBytes {
		t.Fatalf("oversized write replaced the exact-limit report: len=%d err=%v", len(data), err)
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
	if r.P95IID95Approximation || !r.P95IID95Exact || r.P95IID95LowerMS != 936 || r.P95IID95UpperMS != 964 {
		t.Fatalf("unexpected p95 IID interval: %+v", r)
	}
}

func TestNearestRankPercentilesUseCeilingRankWithoutInterpolation(t *testing.T) {
	values := make([]time.Duration, 1_000)
	for i := range values {
		values[i] = time.Duration(i+1) * time.Millisecond
	}
	for n := 1; n <= len(values); n++ {
		for _, method := range []struct {
			value float64
			per   int
		}{{0.50, 50}, {0.95, 95}, {0.99, 99}} {
			rank := (n*method.per + 99) / 100
			got := percentile(values[:n], method.value)
			if want := time.Duration(rank) * time.Millisecond; got != want {
				t.Fatalf("n=%d p=%.2f: nearest-rank value=%s, want rank %d (%s)", n, method.value, got, rank, want)
			}
		}
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
