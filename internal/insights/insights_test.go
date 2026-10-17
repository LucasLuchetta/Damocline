package insights

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func synthetic() report.Result {
	return report.Result{
		SchemaVersion: 2, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: 1, Duration: "30s", ThinkTime: "1s", MaxRPS: 20, P95LimitMS: 1500, MaxErrorRate: 0.01, Mode: "closed", MinSamplesPerStep: 5},
		Metrics: report.MetricSet{Total: 21, Success: 21, P50MS: 178, P95MS: 412, P99MS: 4804}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 1550},
		Steps:   []report.StepResult{{Name: "Dashboard", Path: "/dashboard", Method: "GET", Metrics: report.MetricSet{Total: 21, Success: 21, P95MS: 412}}},
		Outcome: "PASS", IterationsCompleted: 20,
	}
}
func TestReportCallsOutSingleVUAndTailNotServerUpgrade(t *testing.T) {
	r := synthetic()
	a := Evaluate(r, nil)
	if a.Performance != "OBJECTIVES MET IN THIS RUN" || a.Capacity != "NOT DETERMINED" || a.Infrastructure != "NOT MEASURED" {
		t.Fatalf("unexpected verdict: %+v", a)
	}
	var b bytes.Buffer
	Write(&b, r, nil, nil)
	for _, part := range []string{"SERVER UPGRADE: NOT JUSTIFIED", "Only one virtual user", "Long-tail latency", "max_rps setting", "Complete journey p95"} {
		if !strings.Contains(b.String(), part) {
			t.Fatalf("expected %q in output: %s", part, b.String())
		}
	}
}
func TestFailureAndDroppedArrivals(t *testing.T) {
	r := synthetic()
	r.Outcome = "FAIL"
	r.Metrics.Failures = 2
	r.Metrics.Success = 19
	r.ArrivalsDropped = 3
	r.Config.Users = 15
	a := Evaluate(r, nil)
	if a.Performance != "OBJECTIVES NOT MET" {
		t.Fatal(a.Performance)
	}
	var b bytes.Buffer
	Write(&b, r, nil, nil)
	if !strings.Contains(b.String(), "3 scheduled journey starts were dropped") {
		t.Fatal(b.String())
	}
	if !strings.Contains(b.String(), "independent authenticated sessions were not verified") {
		t.Fatal(b.String())
	}
}
func TestHostAlignedAndNoProofOfRootCause(t *testing.T) {
	r := synthetic()
	h := &hostmon.Result{SchemaVersion: 1, Source: "linux:/proc"}
	start, _ := time.Parse(time.RFC3339, r.StartedAt)
	for i := 0; i < 4; i++ {
		h.Samples = append(h.Samples, hostmon.Sample{At: start.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339), CPUPercent: 95, MemoryUsedBytes: 900, MemoryTotalBytes: 1000})
	}
	a := Evaluate(r, h)
	if a.Infrastructure != "HOST OBSERVED (CPU/RAM ONLY)" {
		t.Fatal(a.Infrastructure)
	}
	var b bytes.Buffer
	Write(&b, r, h, nil)
	if !strings.Contains(b.String(), "Possible host resource pressure") || !strings.Contains(b.String(), "before upgrading") {
		t.Fatal(b.String())
	}
}
func TestHostWithoutOverlap(t *testing.T) {
	r := synthetic()
	h := &hostmon.Result{SchemaVersion: 1, Source: "linux:/proc", Samples: []hostmon.Sample{{At: "2026-10-02T12:00:00Z"}}}
	if got := Evaluate(r, h).Infrastructure; got != "HOST DATA NOT ALIGNED" {
		t.Fatal(got)
	}
}
func TestComparisonExplainsDifferentRPSCaps(t *testing.T) {
	prev := synthetic()
	prev.Config.MaxRPS = 5
	now := synthetic()
	now.Metrics.Total = 50
	now.Metrics.Success = 50
	now.ElapsedSeconds = 60
	now.Config.Duration = "60s"
	now.Metrics.P95MS = 368
	var b bytes.Buffer
	Write(&b, now, nil, &prev)
	for _, x := range []string{"max_rps ceilings differ", "Test durations differ", "HTTP p95: 412 -> 368 ms", "Achieved throughput"} {
		if !strings.Contains(b.String(), x) {
			t.Fatalf("missing %q in %s", x, b.String())
		}
	}
}
func TestReadRunRejectsBrokenInput(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "broken.json")
	if err := os.WriteFile(p, []byte(`{"schema_version":2,"name":"x","metrics":{"total":1,"success":9}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRun(p); err == nil {
		t.Fatal("invalid report accepted")
	}
}
func TestNoPrivateTargetInFixture(t *testing.T) {
	r := synthetic()
	if !strings.Contains(r.Target, "example.com") {
		t.Fatal("fixture must use reserved documentation domain")
	}
}

func TestErrorUncertaintyDoesNotMisrepresentSmallPassingSample(t *testing.T) {
	r := synthetic() // 21 requests, zero failures, configured 1% error budget.
	var output bytes.Buffer
	Write(&output, r, nil, nil)
	if !strings.Contains(output.String(), "one-sided 95% upper bound") ||
		!strings.Contains(output.String(), "NOT statistically established") ||
		!strings.Contains(output.String(), "299 independent error-free operations") {
		t.Fatalf("missing conservative error-rate interpretation: %s", output.String())
	}
	if r.Outcome != "PASS" {
		t.Fatal("observed threshold PASS must not be silently recast as statistical certification")
	}
}

func TestErrorUncertaintyReportsStatisticalAssumptions(t *testing.T) {
	r := synthetic()
	r.Metrics.Total = 1000
	r.Metrics.Success = 1000
	var output bytes.Buffer
	Write(&output, r, nil, nil)
	if !strings.Contains(output.String(), "independence and stationarity") ||
		!strings.Contains(output.String(), "does not validate sustained capacity") {
		t.Fatalf("statistical assumptions absent: %s", output.String())
	}
}
