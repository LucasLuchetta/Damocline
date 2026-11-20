package report

import (
	"strings"
	"testing"
)

func measuredBaseline(p50, p95, max float64) *NetworkBaseline {
	ratio := 0.0
	if p50 > 0 {
		ratio = float64(int64(max/p50*1000+0.5)) / 1000
	}
	return &NetworkBaseline{Status: NetworkBaselineMeasured, Phase: "before_load", Source: "first_get_step", Method: "GET", Path: "/",
		Planned: 20, Completed: 20, IntervalMS: 100, TimeoutMS: 5000, MinMS: p50 / 2, P50MS: p50, P95MS: p95, MaxMS: max, MaxToP50Ratio: ratio}
}

func TestNetworkBaselineIssueThresholds(t *testing.T) {
	cfg := RunConfig{P95LimitMS: 1000}
	for _, tc := range []struct {
		name string
		b    *NetworkBaseline
		want string
	}{
		{"stable", measuredBaseline(9, 12, 20), ""},
		{"loopback micro jitter", measuredBaseline(0.1, 0.5, 5), ""},
		{"dominant p95", measuredBaseline(400, 600, 700), "exceeds 50% of the 1000 ms request p95 objective"},
		{"unstable dispersion", measuredBaseline(10, 200, 1100), "unstable"},
		{"failed", &NetworkBaseline{Status: NetworkBaselineMeasured, Planned: 20, Failures: 20}, "failed entirely"},
		{"partial", &NetworkBaseline{Status: NetworkBaselineMeasured, Planned: 20, Completed: 19, Failures: 1, P50MS: 5, P95MS: 6, MaxMS: 7}, "incomplete"},
		{"skipped", &NetworkBaseline{Status: NetworkBaselineSkipped, SkipReason: "same-host-generator"}, ""},
	} {
		got := NetworkBaselineIssue(Result{Config: cfg, NetworkBaseline: tc.b})
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Fatalf("%s: issue=%q want %q", tc.name, got, tc.want)
		}
		if tc.want != "" && !strings.Contains(got, "network path may dominate") {
			t.Fatalf("%s: missing network caveat: %q", tc.name, got)
		}
	}
}

func TestValidNetworkBaselineGatesSchemaAndShape(t *testing.T) {
	ok := Result{SchemaVersion: NetworkBaselineSchemaVersion, NetworkBaseline: measuredBaseline(9, 12, 20)}
	if !ValidNetworkBaseline(ok) {
		t.Fatal("valid baseline rejected")
	}
	old := ok
	old.SchemaVersion = NetworkBaselineSchemaVersion - 1
	if ValidNetworkBaseline(old) {
		t.Fatal("pre-v37 baseline accepted")
	}
	for name, mutate := range map[string]func(*NetworkBaseline){
		"unordered":     func(b *NetworkBaseline) { b.P95MS = 30 },
		"unreconciled":  func(b *NetworkBaseline) { b.Failures = 1 },
		"write method":  func(b *NetworkBaseline) { b.Method = "POST" },
		"bad ratio":     func(b *NetworkBaseline) { b.MaxToP50Ratio = 99 },
		"unknown state": func(b *NetworkBaseline) { b.Status = "MAYBE" },
	} {
		b := *ok.NetworkBaseline
		mutate(&b)
		if ValidNetworkBaseline(Result{SchemaVersion: NetworkBaselineSchemaVersion, NetworkBaseline: &b}) {
			t.Fatalf("%s baseline accepted", name)
		}
	}
	skipped := Result{SchemaVersion: NetworkBaselineSchemaVersion, NetworkBaseline: &NetworkBaseline{Status: NetworkBaselineSkipped, SkipReason: "explicit-opt-out"}}
	if !ValidNetworkBaseline(skipped) {
		t.Fatal("explicit skip rejected")
	}
	skipped.NetworkBaseline.Planned = 20
	if ValidNetworkBaseline(skipped) {
		t.Fatal("skip with measurements accepted")
	}
}

func TestAssessQualityScreensNetworkBaseline(t *testing.T) {
	base := Result{SchemaVersion: CurrentSchemaVersion, Config: RunConfig{P95LimitMS: 1000}}
	limited := base
	limited.NetworkBaseline = measuredBaseline(900, 2600, 3000)
	if q := AssessQuality(limited); !hasFindingText(q.Findings, "network path may dominate") {
		t.Fatalf("dominant baseline did not limit quality: %+v", q)
	}
	failed := base
	failed.NetworkBaseline = &NetworkBaseline{Status: NetworkBaselineMeasured, Phase: "before_load", Source: "first_get_step", Method: "GET", Path: "/", Planned: 20, Failures: 20, IntervalMS: 100, TimeoutMS: 5000}
	if q := AssessQuality(failed); q.State != QualityInvalid || !hasFindingText(q.Findings, "failed entirely") {
		t.Fatalf("failed baseline did not invalidate quality: %+v", q)
	}
}

func hasFindingText(findings []string, text string) bool {
	for _, f := range findings {
		if strings.Contains(f, text) {
			return true
		}
	}
	return false
}
