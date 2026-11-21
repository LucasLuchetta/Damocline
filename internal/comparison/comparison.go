// Package comparison compares two saved closed-loop capacity campaigns of the
// same scenario under the same load. It only reads saved evidence and never
// sends network requests.
package comparison

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
)

// SchemaVersion is the version of the comparison JSON artifact.
const SchemaVersion = 1

// Verdicts for one metric delta.
const (
	VerdictImproved     = "IMPROVED"
	VerdictRegressed    = "REGRESSED"
	VerdictInconclusive = "INCONCLUSIVE"
	VerdictUnchanged    = "UNCHANGED"
)

// Comparability states for the whole comparison.
const (
	Comparable    = "COMPARABLE"
	NotComparable = "NOT_COMPARABLE"
)

// MinRepetitions is the minimum number of usable observation phases per side
// required before a difference can be called anything but INCONCLUSIVE.
const MinRepetitions = 2

// Mismatch records one setting that differs between the two campaigns.
type Mismatch struct {
	Field    string `json:"field"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Blocking bool   `json:"blocking"`
}

// Spread is the descriptive range of one metric across repetitions.
type Spread struct {
	Runs   int       `json:"runs"`
	Values []float64 `json:"values"`
	Min    float64   `json:"min"`
	Median float64   `json:"median"`
	Max    float64   `json:"max"`
}

// MetricDelta compares one metric between the two campaigns.
type MetricDelta struct {
	Scope          string   `json:"scope"`
	Metric         string   `json:"metric"`
	Unit           string   `json:"unit"`
	HigherIsBetter bool     `json:"higher_is_better"`
	Before         Spread   `json:"before"`
	After          Spread   `json:"after"`
	MedianDelta    float64  `json:"median_delta"`
	MedianDeltaPct *float64 `json:"median_delta_percent,omitempty"`
	Verdict        string   `json:"verdict"`
	VerdictReason  string   `json:"verdict_reason"`
}

// Result is the comparison artifact.
type Result struct {
	SchemaVersion int           `json:"schema_version"`
	BeforeSource  string        `json:"before_source"`
	AfterSource   string        `json:"after_source"`
	Scenario      string        `json:"scenario"`
	Comparability string        `json:"comparability"`
	Mismatches    []Mismatch    `json:"mismatches"`
	Method        string        `json:"method"`
	Deltas        []MetricDelta `json:"deltas"`
	Facts         []string      `json:"measured_facts"`
	Hypotheses    []string      `json:"hypotheses"`
	Limitations   []string      `json:"limitations"`
}

const method = "Per-repetition observation-phase values are compared as ranges. " +
	"A difference is IMPROVED or REGRESSED only when the before and after ranges do not overlap " +
	"and each side has at least 2 usable repetitions; otherwise it is INCONCLUSIVE because the " +
	"difference is within run-to-run spread. This is a descriptive screen, not a significance test."

// Compare builds a comparison of two campaigns. Sources are labels only.
func Compare(before, after campaign.Result, beforeSource, afterSource string) Result {
	r := Result{
		SchemaVersion: SchemaVersion, BeforeSource: beforeSource, AfterSource: afterSource,
		Scenario: after.Scenario, Method: method, Mismatches: comparabilityMismatches(before, after),
		Deltas: []MetricDelta{}, Facts: []string{}, Hypotheses: []string{},
	}
	r.Comparability = Comparable
	for _, m := range r.Mismatches {
		if m.Blocking {
			r.Comparability = NotComparable
		}
	}
	r.Limitations = []string{
		"Only observation phases of non-aborted trials are used; warmup, spike, recovery and soak phases are not compared.",
		"Ranges describe observed spread across a small number of repetitions and do not bound the true difference.",
		"A comparison cannot attribute a change to a specific code change; environment drift between runs is not excluded.",
	}
	if r.Comparability == NotComparable {
		r.Facts = append(r.Facts, "The campaigns differ in at least one blocking setting, so no metric deltas are reported.")
		r.Hypotheses = append(r.Hypotheses, "No hypothesis is proposed for non-comparable campaigns.")
		return r
	}
	b, a := observations(before), observations(after)
	overall := func(f func(obs) float64) ([]float64, []float64) { return b.values(f), a.values(f) }
	bp, ap := overall(func(o obs) float64 { return o.p95 })
	be, ae := overall(func(o obs) float64 { return o.errRate })
	bt, at := overall(func(o obs) float64 { return o.rps })
	r.Deltas = append(r.Deltas,
		delta("overall", "request_p95", "ms", false, bp, ap),
		delta("overall", "request_error_rate", "ratio", false, be, ae),
		delta("overall", "throughput", "requests/s", true, bt, at),
	)
	for _, step := range stepNames(b, a) {
		get := func(set obsSet, f func(stepObs) float64) []float64 {
			var out []float64
			for _, o := range set {
				if s, ok := o.steps[step]; ok {
					out = append(out, f(s))
				}
			}
			return out
		}
		p95 := func(s stepObs) float64 { return s.p95 }
		er := func(s stepObs) float64 { return s.errRate }
		rps := func(s stepObs) float64 { return s.rps }
		r.Deltas = append(r.Deltas,
			delta("step:"+step, "p95", "ms", false, get(b, p95), get(a, p95)),
			delta("step:"+step, "error_rate", "ratio", false, get(b, er), get(a, er)),
			delta("step:"+step, "throughput", "requests/s", true, get(b, rps), get(a, rps)),
		)
	}
	r.Facts = append(r.Facts,
		fmt.Sprintf("Before: %d usable observation repetitions, campaign outcome %s.", len(b), before.Outcome),
		fmt.Sprintf("After: %d usable observation repetitions, campaign outcome %s.", len(a), after.Outcome),
	)
	for _, d := range r.Deltas {
		if d.Verdict == VerdictImproved || d.Verdict == VerdictRegressed {
			r.Facts = append(r.Facts, fmt.Sprintf("%s %s: %s (median %s -> %s %s; ranges do not overlap).", d.Scope, d.Metric, d.Verdict, num(d.Before.Median), num(d.After.Median), d.Unit))
		}
	}
	r.Hypotheses = hypotheses(r.Deltas)
	return r
}

func comparabilityMismatches(b, a campaign.Result) []Mismatch {
	out := []Mismatch{}
	add := func(field string, bv, av any, blocking bool) {
		bs, as := str(bv), str(av)
		if bs != as {
			out = append(out, Mismatch{Field: field, Before: bs, After: as, Blocking: blocking})
		}
	}
	add("scenario", b.Scenario, a.Scenario, true)
	if b.WorkloadSource != nil && a.WorkloadSource != nil && b.WorkloadSource.SemanticFingerprint != "" && a.WorkloadSource.SemanticFingerprint != "" {
		add("workload_source.semantic_fingerprint", b.WorkloadSource.SemanticFingerprint, a.WorkloadSource.SemanticFingerprint, true)
	}
	add("base_users", b.BaseUsers, a.BaseUsers, true)
	add("spike_users", b.SpikeUsers, a.SpikeUsers, true)
	add("warmup", b.Warmup, a.Warmup, true)
	add("stabilization", b.Stabilization, a.Stabilization, true)
	add("observation", b.Observation, a.Observation, true)
	add("spike_duration", b.SpikeDuration, a.SpikeDuration, true)
	add("recovery", b.Recovery, a.Recovery, true)
	add("load_envelope", b.LoadEnvelope, a.LoadEnvelope, true)
	add("objectives", b.Objectives, a.Objectives, true)
	add("stability_policy", b.StabilityPolicy, a.StabilityPolicy, true)
	add("pass_criteria", passCriteria(b), passCriteria(a), true)
	add("target", b.Target, a.Target, false)
	add("declared_environment", b.DeclaredEnvironment, a.DeclaredEnvironment, false)
	add("repeats", b.Repeats, a.Repeats, false)
	add("soak", b.Soak, a.Soak, false)
	return out
}

type criteria struct {
	JourneyP95LimitMS float64 `json:"journey_p95_limit_ms"`
	MaxErrorRate      float64 `json:"max_error_rate"`
	MinSamplesPerStep int     `json:"min_samples_per_step"`
}

// passCriteria returns the run-level pass criteria of the first recorded phase.
func passCriteria(r campaign.Result) *criteria {
	for _, t := range r.Trials {
		for _, p := range t.Phases {
			c := p.Run.Config
			return &criteria{c.JourneyLatencyLimitMS(), c.MaxErrorRate, c.MinSamplesPerStep}
		}
	}
	return nil
}

func str(v any) string {
	if v == nil {
		return "none"
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return "none"
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return "none"
		}
		return x
	case int:
		return strconv.Itoa(x)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

type stepObs struct{ p95, errRate, rps float64 }

type obs struct {
	p95, errRate, rps float64
	steps             map[string]stepObs
}

type obsSet []obs

func (s obsSet) values(f func(obs) float64) []float64 {
	out := make([]float64, 0, len(s))
	for _, o := range s {
		out = append(out, f(o))
	}
	return out
}

func observations(r campaign.Result) obsSet {
	var out obsSet
	for _, t := range r.Trials {
		if t.Outcome == "ABORTED" || t.Outcome == "IN_PROGRESS" {
			continue
		}
		for _, p := range t.Phases {
			if p.Name != "observation" || p.Run.Metrics.Total <= 0 {
				continue
			}
			secs := p.Run.ObservationSeconds
			if secs <= 0 {
				secs = p.Run.ElapsedSeconds
			}
			if secs <= 0 || math.IsNaN(secs) || math.IsInf(secs, 0) {
				continue
			}
			m := p.Run.Metrics
			o := obs{p95: m.P95MS, errRate: ratio(m.Failures, m.Total), rps: float64(m.Total) / secs, steps: map[string]stepObs{}}
			for _, s := range p.Run.Steps {
				if s.Metrics.Total <= 0 {
					continue
				}
				key := s.Name
				if s.Profile != "" {
					key = s.Profile + "/" + s.Name
				}
				o.steps[key] = stepObs{p95: s.Metrics.P95MS, errRate: ratio(s.Metrics.Failures, s.Metrics.Total), rps: float64(s.Metrics.Total) / secs}
			}
			out = append(out, o)
		}
	}
	return out
}

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func stepNames(b, a obsSet) []string {
	seen := map[string]bool{}
	for _, set := range []obsSet{b, a} {
		for _, o := range set {
			for k := range o.steps {
				seen[k] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func spread(values []float64) Spread {
	s := Spread{Runs: len(values), Values: append([]float64{}, values...)}
	if len(values) == 0 {
		return s
	}
	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)
	s.Min, s.Max = sorted[0], sorted[len(sorted)-1]
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		s.Median = sorted[mid]
	} else {
		s.Median = (sorted[mid-1] + sorted[mid]) / 2
	}
	return s
}

func delta(scope, metric, unit string, higherIsBetter bool, before, after []float64) MetricDelta {
	d := MetricDelta{Scope: scope, Metric: metric, Unit: unit, HigherIsBetter: higherIsBetter, Before: spread(before), After: spread(after)}
	d.MedianDelta = d.After.Median - d.Before.Median
	if d.Before.Median != 0 {
		pct := d.MedianDelta / d.Before.Median * 100
		d.MedianDeltaPct = &pct
	}
	switch {
	case d.Before.Runs < MinRepetitions || d.After.Runs < MinRepetitions:
		d.Verdict, d.VerdictReason = VerdictInconclusive, fmt.Sprintf("fewer than %d usable repetitions on one side", MinRepetitions)
	case d.Before.Min == d.Before.Max && d.After.Min == d.After.Max && d.Before.Min == d.After.Min:
		d.Verdict, d.VerdictReason = VerdictUnchanged, "identical values in every repetition"
	case d.After.Min > d.Before.Max || d.After.Max < d.Before.Min:
		if (d.After.Min > d.Before.Max) == higherIsBetter {
			d.Verdict = VerdictImproved
		} else {
			d.Verdict = VerdictRegressed
		}
		d.VerdictReason = "before and after ranges do not overlap"
	default:
		d.Verdict, d.VerdictReason = VerdictInconclusive, "difference is within run-to-run spread (ranges overlap)"
	}
	return d
}

func hypotheses(deltas []MetricDelta) []string {
	out := []string{}
	var p95Reg, rpsReg, errReg bool
	for _, d := range deltas {
		if d.Scope != "overall" || d.Verdict != VerdictRegressed {
			continue
		}
		switch d.Metric {
		case "request_p95":
			p95Reg = true
		case "throughput":
			rpsReg = true
		case "request_error_rate":
			errReg = true
		}
	}
	if p95Reg && rpsReg {
		out = append(out, "Hypothesis: higher latency with lower throughput under the same closed-loop load is consistent with added per-request work or a newly contended resource; this comparison does not prove it.")
	}
	if errReg {
		out = append(out, "Hypothesis: a higher error rate may come from a functional change, a saturated dependency, or environment drift; inspect failure reasons in both campaigns.")
	}
	for _, d := range deltas {
		if strings.HasPrefix(d.Scope, "step:") && d.Metric == "p95" && d.Verdict == VerdictRegressed {
			out = append(out, fmt.Sprintf("Hypothesis: the %s latency regression points at the code path behind that step; correlate with application and host evidence before attributing a root cause.", strings.TrimPrefix(d.Scope, "step:")))
		}
	}
	if len(out) == 0 {
		out = append(out, "No hypothesis is proposed: no regression was separated from run-to-run spread.")
	}
	return out
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// Markdown renders the comparison as Markdown.
func Markdown(r Result) string {
	var b strings.Builder
	b.WriteString("# Damocline before/after comparison\n\n")
	fmt.Fprintf(&b, "- Scenario: %s\n- Before: %s\n- After: %s\n- Comparability: **%s**\n\n", cell(r.Scenario), cell(r.BeforeSource), cell(r.AfterSource), r.Comparability)
	if len(r.Mismatches) > 0 {
		b.WriteString("## Setting differences\n\n| Field | Before | After | Blocking |\n|---|---|---|---|\n")
		for _, m := range r.Mismatches {
			fmt.Fprintf(&b, "| %s | %s | %s | %t |\n", m.Field, cell(m.Before), cell(m.After), m.Blocking)
		}
		b.WriteString("\n")
	}
	if len(r.Deltas) > 0 {
		b.WriteString("## Metric deltas (observation phases)\n\n| Scope | Metric | Before median [min, max] (n) | After median [min, max] (n) | Median delta | Verdict |\n|---|---|---|---|---|---|\n")
		for _, d := range r.Deltas {
			pct := ""
			if d.MedianDeltaPct != nil {
				pct = fmt.Sprintf(" (%+.1f%%)", *d.MedianDeltaPct)
			}
			fmt.Fprintf(&b, "| %s | %s (%s) | %s | %s | %+.3f%s | %s |\n", cell(d.Scope), d.Metric, d.Unit, sp(d.Before), sp(d.After), d.MedianDelta, pct, d.Verdict)
		}
		b.WriteString("\n")
	}
	list := func(title string, items []string) {
		fmt.Fprintf(&b, "## %s\n\n", title)
		for _, s := range items {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	list("Measured facts", r.Facts)
	list("Hypotheses (not measured)", r.Hypotheses)
	fmt.Fprintf(&b, "## Method\n\n%s\n\n", r.Method)
	list("Limitations", r.Limitations)
	return b.String()
}

func sp(s Spread) string {
	if s.Runs == 0 {
		return "none (0)"
	}
	return fmt.Sprintf("%s [%s, %s] (%d)", num(s.Median), num(s.Min), num(s.Max), s.Runs)
}

func cell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s)
}
