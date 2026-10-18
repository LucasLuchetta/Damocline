package insights

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func synthetic() report.Result {
	return report.Result{
		SchemaVersion: 11, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30,
		Config:  report.RunConfig{WorkloadRevision: "workload-v1", ApplicationRevision: "app-build-1", WorkloadFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Environment: "staging-small", Users: 1, Duration: "30s", ThinkTime: "1s", MaxRPS: 20, P95LimitMS: 1500, MaxErrorRate: 0.01, Mode: "closed", MinSamplesPerStep: 5},
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

func TestReportExplainsArrivalSchedulerLagDistribution(t *testing.T) {
	r := synthetic()
	r.Config.Mode = "arrival"
	r.SchedulerLagSamples = 4
	r.SchedulerLagP50MS = 1
	r.SchedulerLagP95MS = 2
	r.SchedulerLagMaxMS = 3
	var output bytes.Buffer
	Write(&output, r, nil, nil)
	for _, expected := range []string{
		"over 4 planned arrivals", "p50 1.0 ms", "p95 2.0 ms", "max 3.0 ms",
		"monotonic slot-to-admission", "not a calibrated clock-accuracy bound",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("report is missing %q: %s", expected, output.String())
		}
	}
}

func TestReportExplainsResponseBodySizesWithoutBodyContent(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 12
	r.Steps[0].Metrics.ResponseBody = report.ResponseBodyDiagnostics{
		CompleteObservations:   8,
		IncompleteObservations: 1,
		OversizedObservations:  2,
		MinBytes:               16,
		MaxBytes:               4096,
		TotalBytes:             12000,
	}
	var output bytes.Buffer
	Write(&output, r, nil, nil)
	for _, expected := range []string{
		"8 complete bodies (16-4096 bytes; 12000 bytes total)",
		"1 interrupted reads", "2 oversized bodies", "do not establish expected-workload equivalence",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("report is missing %q: %s", expected, output.String())
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

func TestComparisonRequiresRevisionAndRecordsEnvironment(t *testing.T) {
	prev := synthetic()
	now := synthetic()
	prev.Config.WorkloadRevision = "checkout-v4"
	now.Config.WorkloadRevision = "checkout-v4"
	prev.Config.Environment = "staging-small"
	now.Config.Environment = "staging-large"
	var b bytes.Buffer
	Write(&b, now, nil, &prev)
	if !strings.Contains(b.String(), "Comparison eligibility: NOT_COMPARABLE") {
		t.Fatalf("different environment labels must not be treated as comparable: %s", b.String())
	}
	if !strings.Contains(b.String(), "Environment: staging-small -> staging-large") || !strings.Contains(b.String(), "not proof of cause") || !strings.Contains(b.String(), "NOT_COMPARABLE") {
		t.Fatalf("environment comparison missing limitation: %s", b.String())
	}
}

func TestComparisonRequiresMatchingWorkloadFingerprint(t *testing.T) {
	prev, current := synthetic(), synthetic()
	current.Config.WorkloadFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var out strings.Builder
	writeComparison(&out, prev, current)
	if !strings.Contains(out.String(), "Comparison eligibility: NOT_COMPARABLE") || !strings.Contains(out.String(), "Workload fingerprint") {
		t.Fatalf("different workload fingerprints must block controlled comparison: %s", out.String())
	}
}

func TestComparisonOnlyMatchesDeclaredWorkload(t *testing.T) {
	prev, current := synthetic(), synthetic()
	var out strings.Builder
	writeComparison(&out, prev, current)
	if !strings.Contains(out.String(), "MATCHED_DECLARED_WORKLOAD") || !strings.Contains(out.String(), "do not embed loopback preflight/calibration evidence") || strings.Contains(out.String(), "MATCHED_DECLARED_CONDITIONS") {
		t.Fatalf("matching declared workload was overstated as controlled evidence: %s", out.String())
	}
}

func TestComparisonReportsApplicationRevisionAsTreatment(t *testing.T) {
	prev, current := synthetic(), synthetic()
	current.Config.ApplicationRevision = "app-build-2"
	var out strings.Builder
	writeComparison(&out, prev, current)
	if !strings.Contains(out.String(), "Application revision: app-build-1 -> app-build-2") || !strings.Contains(out.String(), "does not establish its causal effect") {
		t.Fatalf("application revision change was not recorded as an unproven treatment: %s", out.String())
	}
}

func TestComparisonWarnsWhenApplicationRevisionIsMissing(t *testing.T) {
	prev, current := synthetic(), synthetic()
	current.Config.ApplicationRevision = ""
	var out strings.Builder
	writeComparison(&out, prev, current)
	if !strings.Contains(out.String(), "Application revision is missing") {
		t.Fatalf("missing application revision was not made explicit: %s", out.String())
	}
}

func TestComparisonRequiresSchemaElevenFingerprint(t *testing.T) {
	prev, current := synthetic(), synthetic()
	prev.SchemaVersion = 10
	current.SchemaVersion = 10
	var out strings.Builder
	writeComparison(&out, prev, current)
	if !strings.Contains(out.String(), "Comparison eligibility: NOT_COMPARABLE") {
		t.Fatalf("pre-fingerprint reports must not be treated as controlled comparisons: %s", out.String())
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

func TestReadRunAcceptsSchemaThreeGeneratorHealth(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 3
	r.Generator = &generatorhealth.Summary{ProcessSamples: 4, ProcessCPUSamples: 3,
		PeakProcessRSSBytes: 1 << 20, PeakProcessCPUUtilizationRatio: 0.1, ProcessCPUValid: true}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-v3.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 3 || got.Generator == nil || !got.Generator.ProcessCPUValid {
		t.Fatalf("schema-v3 generator health not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaFourObservationWindow(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 4
	r.ElapsedSeconds = 31
	r.ObservationSeconds = 30
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-v4.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 4 || got.WindowSeconds() != 30 {
		t.Fatalf("schema-v4 workload window not readable: %+v err=%v", got, err)
	}
	got.ObservationSeconds = 0
	bad, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRun(path); err == nil {
		t.Fatal("schema-v4 report without observation window was accepted")
	}
}

func TestReadRunAcceptsSchemaFiveProfiles(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 5
	r.ElapsedSeconds = 31
	r.ObservationSeconds = 30
	r.Config.Profiles = []report.ProfileConfig{{Name: "browse", Weight: 3, ThinkTime: "200ms"}, {Name: "search", Weight: 1, ThinkTime: "0s"}}
	r.Profiles = []report.ProfileResult{{Name: "browse", Weight: 3, AssignedVUs: 3}, {Name: "search", Weight: 1, AssignedVUs: 1}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-v5.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 5 || len(got.Profiles) != 2 || got.Profiles[0].Name != "browse" {
		t.Fatalf("schema-v5 profiles not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaSixP95RankInterval(t *testing.T) {
	r := report.Result{
		SchemaVersion: 6, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z",
		ElapsedSeconds: 30, ObservationSeconds: 30, Config: report.RunConfig{Users: 1},
		Metrics: report.MetricSet{Total: 1, Success: 1, P95IID95Approximation: true, P95IID95LowerMS: 10, P95IID95UpperMS: 20},
	}
	path := filepath.Join(t.TempDir(), "schema-v6.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 6 || !got.Metrics.P95IID95Approximation {
		t.Fatalf("schema-v6 report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaSevenHTTPTrace(t *testing.T) {
	r := report.Result{SchemaVersion: 7, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30, ObservationSeconds: 30, Config: report.RunConfig{Users: 1}, Steps: []report.StepResult{{Name: "page", HTTPTrace: report.HTTPTrace{RequestAttemptCount: 1, CompleteCount: 1, RateWaitCount: 1}}}}
	path := filepath.Join(t.TempDir(), "schema-v7.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 7 || len(got.Steps) != 1 || got.Steps[0].HTTPTrace.RequestAttemptCount != 1 {
		t.Fatalf("schema-v7 timing report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaEightHTTPDiagnostics(t *testing.T) {
	r := report.Result{SchemaVersion: 8, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30, ObservationSeconds: 30, Config: report.RunConfig{Users: 1}, Metrics: report.MetricSet{HTTP: report.HTTPDiagnostics{StatusCodes: map[int]int{302: 1}, Status3xx: 1, RedirectResponses: 1}}}
	path := filepath.Join(t.TempDir(), "schema-v8.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 8 || got.Metrics.HTTP.RedirectResponses != 1 {
		t.Fatalf("schema-v8 HTTP diagnostics report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaNineMixProvenance(t *testing.T) {
	r := report.Result{SchemaVersion: 9, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30, ObservationSeconds: 30, Config: report.RunConfig{Users: 1, ProfileMixBasis: "sanitized_analytics", ProfileMixReference: "weekly-summary-v2"}}
	path := filepath.Join(t.TempDir(), "schema-v9.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.Config.ProfileMixBasis != "sanitized_analytics" || got.Config.ProfileMixReference != "weekly-summary-v2" {
		t.Fatalf("schema-v9 provenance report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaTenSamplerEvidence(t *testing.T) {
	r := report.Result{SchemaVersion: 10, Name: "Synthetic example", Target: "https://app.example.com", StartedAt: "2026-10-01T12:00:00Z", ElapsedSeconds: 30, ObservationSeconds: 30, Config: report.RunConfig{Users: 1}, Generator: &generatorhealth.Summary{Samples: 5, SamplingIntervalMS: 250, MaxSampleGapMS: 251, SampleCollectionTotalMS: 1.2, SampleCollectionMaxMS: 0.4}}
	path := filepath.Join(t.TempDir(), "schema-v10.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 10 || got.Generator.MaxSampleGapMS != 251 {
		t.Fatalf("schema-v10 sampler report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunRequiresValidFingerprintForSchemaEleven(t *testing.T) {
	r := synthetic()
	r.ObservationSeconds = 30
	r.Config.WorkloadFingerprint = "invalid"
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-v11-invalid-fingerprint.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRun(path); err == nil {
		t.Fatal("schema-v11 report without a valid workload fingerprint was accepted")
	}
}

func TestReadRunAcceptsSchemaElevenWorkloadFingerprint(t *testing.T) {
	r := synthetic()
	r.ObservationSeconds = 30
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "schema-v11-workload-fingerprint.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"response_body"`)) {
		t.Fatal("schema-v11 report must not imply response-body shape was measured")
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 11 || got.Config.WorkloadFingerprint != r.Config.WorkloadFingerprint {
		t.Fatalf("schema-v11 workload fingerprint report not readable: %+v err=%v", got, err)
	}
}

func TestReadRunAcceptsSchemaTwelveResponseBodyMeasurements(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 12
	r.ObservationSeconds = 30
	r.Steps[0].Metrics.ResponseBody = report.ResponseBodyDiagnostics{
		CompleteObservations: 4,
		MinBytes:             0,
		MaxBytes:             2048,
		TotalBytes:           4096,
	}
	path := filepath.Join(t.TempDir(), "schema-v12-response-body.json")
	if err := report.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRun(path)
	if err != nil || got.SchemaVersion != 12 || got.Steps[0].Metrics.ResponseBody.TotalBytes != 4096 {
		t.Fatalf("schema-v12 response-body report not readable: %+v err=%v", got, err)
	}
}

func TestComparisonRequiresMatchingMixProvenance(t *testing.T) {
	old, current := synthetic(), synthetic()
	old.Config.WorkloadRevision, current.Config.WorkloadRevision = "rev-1", "rev-1"
	old.Config.ProfileMixBasis, current.Config.ProfileMixBasis = "sanitized_analytics", "business_estimate"
	old.Config.ProfileMixReference, current.Config.ProfileMixReference = "usage-v1", "planning-v1"
	var out strings.Builder
	writeComparison(&out, old, current)
	if !strings.Contains(out.String(), "NOT a controlled comparison") {
		t.Fatalf("comparison did not reject changed provenance: %s", out.String())
	}
}
func TestNoPrivateTargetInFixture(t *testing.T) {
	r := synthetic()
	if !strings.Contains(r.Target, "example.com") {
		t.Fatal("fixture must use reserved documentation domain")
	}
}

func TestGeneratorHealthReportedWithoutAssumingServerBottleneck(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 10
	r.Generator = &generatorhealth.Summary{
		Samples: 6, ObservedSeconds: 2, SamplingIntervalMS: 250, MaxSampleGapMS: 251, SampleCollectionTotalMS: 2.4, SampleCollectionMaxMS: 0.5, PeakHeapAllocBytes: 32 << 20,
		PeakGoroutines: 21, GCCycles: 1, GoCPUValid: true,
		GoCPUUtilizationRatio: 0.96, ProcessSamples: 6, ProcessCPUSamples: 5, ProcessCPUValid: true,
		PeakProcessRSSBytes: 64 << 20, PeakProcessCPUUtilizationRatio: 0.95,
	}
	var output bytes.Buffer
	Write(&output, r, nil, nil)
	for _, phrase := range []string{"Load-generator runtime", "Generator sampler", "maximum sample gap", "Go runtime CPU activity", "Validate generator headroom", "Load-generator OS process", "Peak sampled generator process CPU", "SERVER UPGRADE: NOT JUSTIFIED"} {
		if !strings.Contains(output.String(), phrase) {
			t.Fatalf("missing %q in output: %s", phrase, output.String())
		}
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
