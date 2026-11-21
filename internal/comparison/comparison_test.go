package comparison

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

type trialSpec struct {
	p95, stepP95 float64
	total, fails int
}

func fixture(trials ...trialSpec) campaign.Result {
	r := campaign.Result{Scenario: "synthetic-checkout", BaseUsers: 10, Target: "http://127.0.0.1:8080", Observation: "30s", Warmup: "5s", Recovery: "10s", Repeats: len(trials), Outcome: "PASS"}
	for i, s := range trials {
		run := report.Result{ObservationSeconds: 10, Config: report.RunConfig{MaxErrorRate: 0.01}}
		run.Metrics = report.MetricSet{Total: s.total, Failures: s.fails, P95MS: s.p95}
		run.Steps = []report.StepResult{{Name: "login", Metrics: report.MetricSet{Total: s.total, Failures: s.fails, P95MS: s.stepP95}}}
		r.Trials = append(r.Trials, campaign.Trial{Number: i + 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "warmup", Run: run}, {Name: "observation", Run: run}}})
	}
	return r
}

func find(t *testing.T, r Result, scope, metric string) MetricDelta {
	t.Helper()
	for _, d := range r.Deltas {
		if d.Scope == scope && d.Metric == metric {
			return d
		}
	}
	t.Fatalf("missing delta %s %s", scope, metric)
	return MetricDelta{}
}

func TestRegressionSeparatedFromSpread(t *testing.T) {
	before := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{110, 85, 1010, 0}, trialSpec{105, 82, 990, 0})
	after := fixture(trialSpec{200, 180, 800, 0}, trialSpec{210, 190, 790, 0}, trialSpec{205, 185, 810, 0})
	r := Compare(before, after, "before", "after")
	if r.Comparability != Comparable {
		t.Fatalf("comparability = %s %+v", r.Comparability, r.Mismatches)
	}
	if d := find(t, r, "overall", "request_p95"); d.Verdict != VerdictRegressed || d.Before.Median != 105 || d.After.Median != 205 {
		t.Fatalf("p95 delta = %+v", d)
	}
	if d := find(t, r, "overall", "throughput"); d.Verdict != VerdictRegressed {
		t.Fatalf("throughput = %+v", d)
	}
	if d := find(t, r, "step:login", "p95"); d.Verdict != VerdictRegressed {
		t.Fatalf("step p95 = %+v", d)
	}
	if d := find(t, r, "overall", "request_error_rate"); d.Verdict != VerdictUnchanged {
		t.Fatalf("error rate = %+v", d)
	}
	if !strings.Contains(strings.Join(r.Hypotheses, " "), "login") {
		t.Fatalf("hypotheses = %v", r.Hypotheses)
	}
	for _, h := range r.Hypotheses {
		if !strings.HasPrefix(h, "Hypothesis:") {
			t.Fatalf("hypothesis not labelled: %q", h)
		}
	}
}

func TestImprovementAndOverlapIsInconclusive(t *testing.T) {
	before := fixture(trialSpec{200, 100, 1000, 0}, trialSpec{220, 120, 1000, 0})
	after := fixture(trialSpec{100, 110, 1000, 0}, trialSpec{120, 115, 1000, 0})
	r := Compare(before, after, "b", "a")
	if d := find(t, r, "overall", "request_p95"); d.Verdict != VerdictImproved {
		t.Fatalf("p95 = %+v", d)
	}
	if d := find(t, r, "step:login", "p95"); d.Verdict != VerdictInconclusive || !strings.Contains(d.VerdictReason, "spread") {
		t.Fatalf("step p95 = %+v", d)
	}
}

func TestSingleRepetitionIsInconclusive(t *testing.T) {
	r := Compare(fixture(trialSpec{100, 80, 1000, 0}), fixture(trialSpec{500, 400, 100, 50}), "b", "a")
	for _, d := range r.Deltas {
		if d.Verdict != VerdictInconclusive {
			t.Fatalf("%s %s = %s", d.Scope, d.Metric, d.Verdict)
		}
	}
}

func TestAbortedTrialsAreIgnored(t *testing.T) {
	before := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0}, trialSpec{900, 900, 10, 9})
	before.Trials[2].Outcome = "ABORTED"
	r := Compare(before, before, "b", "a")
	if d := find(t, r, "overall", "request_p95"); d.Before.Runs != 2 || d.Verdict != VerdictUnchanged {
		t.Fatalf("p95 = %+v", d)
	}
}

func TestBlockingMismatchesRefuseDeltas(t *testing.T) {
	base := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0})
	cases := map[string]func(*campaign.Result){
		"scenario":         func(r *campaign.Result) { r.Scenario = "other" },
		"base_users":       func(r *campaign.Result) { r.BaseUsers = 20 },
		"observation":      func(r *campaign.Result) { r.Observation = "60s" },
		"load_envelope":    func(r *campaign.Result) { r.LoadEnvelope = &report.Envelope{Mode: report.EnvelopeModeExtended} },
		"pass_criteria":    func(r *campaign.Result) { r.Trials[0].Phases[0].Run.Config.MaxErrorRate = 0.05 },
		"objectives":       func(r *campaign.Result) { r.Objectives = &campaign.ObjectiveContract{Version: 1} },
		"stability_policy": func(r *campaign.Result) { p := campaign.DefaultStabilityPolicy(); r.StabilityPolicy = &p },
	}
	for field, mutate := range cases {
		after := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0})
		mutate(&after)
		r := Compare(base, after, "b", "a")
		if r.Comparability != NotComparable || len(r.Deltas) != 0 {
			t.Fatalf("%s: comparability=%s deltas=%d", field, r.Comparability, len(r.Deltas))
		}
		found := false
		for _, m := range r.Mismatches {
			found = found || (m.Field == field && m.Blocking)
		}
		if !found {
			t.Fatalf("%s: mismatches %+v", field, r.Mismatches)
		}
	}
}

func TestNonBlockingMismatchIsFlagged(t *testing.T) {
	base := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0})
	after := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0}, trialSpec{100, 80, 1000, 0})
	r := Compare(base, after, "b", "a")
	if r.Comparability != Comparable || len(r.Mismatches) != 1 || r.Mismatches[0].Field != "repeats" || r.Mismatches[0].Blocking {
		t.Fatalf("mismatches = %+v", r.Mismatches)
	}
}

func TestOutputsAreDeterministic(t *testing.T) {
	before := fixture(trialSpec{100, 80, 1000, 0}, trialSpec{110, 85, 1010, 1})
	after := fixture(trialSpec{200, 180, 800, 0}, trialSpec{210, 190, 790, 0})
	first, second := Compare(before, after, "b", "a"), Compare(before, after, "b", "a")
	j1, _ := json.Marshal(first)
	j2, _ := json.Marshal(second)
	if string(j1) != string(j2) || Markdown(first) != Markdown(second) {
		t.Fatal("non-deterministic output")
	}
	md := Markdown(first)
	for _, want := range []string{"## Measured facts", "## Hypotheses (not measured)", "REGRESSED", "step:login"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
