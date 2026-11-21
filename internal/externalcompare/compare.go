// Package externalcompare compares a Damocline run report with a k6 summary
// produced for an equivalent closed-loop scenario. It never runs k6.
package externalcompare

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

// Classification of each compared metric.
const (
	Consistent    = "CONSISTENT"
	Explainable   = "EXPLAINABLE_DIFFERENCE"
	RedFlag       = "RED_FLAG"
	NotComparable = "NOT_COMPARABLE"
)

// K6Summary holds the k6 values used for the comparison.
type K6Summary struct {
	HTTPReqs     float64
	HTTPReqRate  float64
	FailedRate   float64
	HasFailed    bool
	P95MS        float64
	HasP95       bool
	VUsMax       float64
	DurationSecs float64
}

// ParseK6Summary accepts both `k6 run --summary-export` output and the
// handleSummary(data) JSON shape (metrics.<name>.values).
func ParseK6Summary(data []byte) (K6Summary, error) {
	var raw struct {
		Metrics map[string]map[string]any `json:"metrics"`
		State   struct {
			TestRunDurationMS float64 `json:"testRunDurationMs"`
		} `json:"state"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return K6Summary{}, fmt.Errorf("decode k6 summary: %w", err)
	}
	if len(raw.Metrics) == 0 {
		return K6Summary{}, fmt.Errorf("k6 summary has no metrics")
	}
	get := func(metric, field string) (float64, bool) {
		m, ok := raw.Metrics[metric]
		if !ok {
			return 0, false
		}
		if vals, ok := m["values"].(map[string]any); ok {
			m = vals
		}
		v, ok := m[field].(float64)
		return v, ok
	}
	var s K6Summary
	var ok bool
	if s.HTTPReqs, ok = get("http_reqs", "count"); !ok {
		return K6Summary{}, fmt.Errorf("k6 summary lacks http_reqs.count")
	}
	s.HTTPReqRate, _ = get("http_reqs", "rate")
	if s.FailedRate, s.HasFailed = get("http_req_failed", "value"); !s.HasFailed {
		s.FailedRate, s.HasFailed = get("http_req_failed", "rate")
	}
	s.P95MS, s.HasP95 = get("http_req_duration", "p(95)")
	if v, ok := get("vus_max", "max"); ok {
		s.VUsMax = v
	} else {
		s.VUsMax, _ = get("vus_max", "value")
	}
	s.DurationSecs = raw.State.TestRunDurationMS / 1000
	return s, nil
}

// Options declare comparison tolerances.
type Options struct {
	ThroughputRelTolerance float64
	ErrorRateAbsTolerance  float64
	P95RelTolerance        float64
}

// DefaultOptions returns the declared default tolerances.
func DefaultOptions() Options {
	return Options{ThroughputRelTolerance: 0.10, ErrorRateAbsTolerance: 0.01, P95RelTolerance: 0.15}
}

// Check is one compared metric.
type Check struct {
	Metric         string  `json:"metric"`
	Damocline      float64 `json:"damocline"`
	K6             float64 `json:"k6"`
	Classification string  `json:"classification"`
	Explanation    string  `json:"explanation"`
}

// Result is the comparison outcome.
type Result struct {
	RunName          string   `json:"run_name"`
	DamoclineOutcome string   `json:"damocline_outcome"`
	K6WouldPass      *bool    `json:"k6_would_pass,omitempty"`
	Checks           []Check  `json:"checks"`
	Overall          string   `json:"overall"`
	Notes            []string `json:"notes"`
}

func relDiff(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	return math.Abs(a-b) / math.Max(math.Abs(a), math.Abs(b))
}

func r3(v float64) float64 { return math.Round(v*1000) / 1000 }

// Compare classifies differences between a Damocline run and a k6 summary.
func Compare(run report.Result, k K6Summary, o Options) Result {
	res := Result{RunName: run.Name, DamoclineOutcome: run.Outcome}
	add := func(c Check) { c.Damocline, c.K6 = r3(c.Damocline), r3(c.K6); res.Checks = append(res.Checks, c) }

	// Offered load: closed-loop VUs must match or nothing else is comparable.
	offered := Check{Metric: "offered_load_vus", Damocline: float64(run.Config.Users), K6: k.VUsMax}
	comparable := true
	switch {
	case run.Config.Mode != "" && run.Config.Mode != "closed":
		offered.Classification, offered.Explanation = NotComparable, fmt.Sprintf("Damocline mode %q is not closed-loop; the kit compares closed-loop VUs only.", run.Config.Mode)
		comparable = false
	case k.VUsMax == 0:
		offered.Classification, offered.Explanation = NotComparable, "k6 summary has no vus_max."
		comparable = false
	case int(k.VUsMax) != run.Config.Users:
		offered.Classification, offered.Explanation = NotComparable, "Different VU counts: the two runs did not offer the same closed-loop load."
		comparable = false
	default:
		offered.Classification, offered.Explanation = Consistent, "Same closed-loop VU count. VUs are concurrent actors, not requests per second."
	}
	add(offered)

	window := run.ObservationSeconds
	if window <= 0 {
		window = run.ElapsedSeconds
	}
	damRate := 0.0
	if window > 0 {
		damRate = float64(run.Metrics.Total) / window
	}
	tp := Check{Metric: "delivered_requests_per_second", Damocline: damRate, K6: k.HTTPReqRate}
	gap := relDiff(damRate, k.HTTPReqRate)
	genLimited := run.Generator != nil && run.Generator.ProcessCPUValid && run.Generator.PeakProcessCPUUtilizationRatio >= 0.8
	switch {
	case !comparable || k.HTTPReqRate == 0 || damRate == 0:
		tp.Classification, tp.Explanation = NotComparable, "Throughput is only comparable at equal offered load with nonzero rates."
	case gap <= o.ThroughputRelTolerance:
		tp.Classification, tp.Explanation = Consistent, fmt.Sprintf("Within %.0f%%.", o.ThroughputRelTolerance*100)
	case k.HTTPReqRate > damRate:
		tp.Classification = RedFlag
		tp.Explanation = fmt.Sprintf("k6 delivered %.0f%% more requests per second at the same VUs: Damocline's generator, connection policy or max_rps (%d) may be limiting the offered load. Check generator health before trusting capacity.", gap*100, run.Config.MaxRPS)
		if genLimited {
			tp.Explanation += " Damocline's generator process CPU peaked at or above 80%, which supports a generator limit."
		}
	default:
		tp.Classification = Explainable
		tp.Explanation = fmt.Sprintf("Damocline delivered %.0f%% more than k6: check the k6 host's CPU and k6's own warnings; a closed loop slows when its generator is the bottleneck.", gap*100)
	}
	add(tp)

	errCheck := Check{Metric: "error_rate", Damocline: run.ErrorsRate, K6: k.FailedRate}
	switch {
	case !k.HasFailed:
		errCheck.Classification, errCheck.Explanation = NotComparable, "k6 summary lacks http_req_failed."
	case math.Abs(run.ErrorsRate-k.FailedRate) <= o.ErrorRateAbsTolerance:
		errCheck.Classification, errCheck.Explanation = Consistent, fmt.Sprintf("Within %.3f absolute.", o.ErrorRateAbsTolerance)
	default:
		errCheck.Classification = Explainable
		errCheck.Explanation = "Error rates differ. k6 http_req_failed uses the generated script's expected-status rule, while Damocline also counts body assertions and transport failures; compare check failures in the k6 output."
		if k.FailedRate > run.ErrorsRate && run.Outcome == "PASS" && k.FailedRate > run.Config.MaxErrorRate {
			errCheck.Classification = RedFlag
			errCheck.Explanation = "k6 observed an error rate above max_error_rate at a level Damocline approved."
		}
	}
	add(errCheck)

	p95 := Check{Metric: "p95_ms", Damocline: run.Metrics.P95MS, K6: k.P95MS}
	switch {
	case !k.HasP95:
		p95.Classification, p95.Explanation = NotComparable, "k6 summary lacks http_req_duration p(95)."
	case relDiff(run.Metrics.P95MS, k.P95MS) <= o.P95RelTolerance:
		p95.Classification, p95.Explanation = Consistent, fmt.Sprintf("Within %.0f%%.", o.P95RelTolerance*100)
	case run.Metrics.P95MS > k.P95MS:
		p95.Classification = Explainable
		p95.Explanation = "Damocline p95 is higher. k6 http_req_duration excludes blocked, DNS, connect and TLS time, while Damocline measures the whole request; estimator differences also apply."
	default:
		p95.Classification = Explainable
		p95.Explanation = "k6 p95 is higher than Damocline's. Damocline's definition includes more phases, so investigate the k6 generator host and pooled-percentile composition."
		if run.Outcome == "PASS" && run.Config.P95LimitMS > 0 && k.P95MS > run.Config.P95LimitMS {
			p95.Classification = RedFlag
			p95.Explanation = fmt.Sprintf("k6 p95 exceeds the p95 limit (%.0f ms) at a level Damocline approved, although k6 measures fewer phases.", run.Config.P95LimitMS)
		}
	}
	add(p95)

	if k.HasFailed && k.HasP95 {
		pass := k.FailedRate <= run.Config.MaxErrorRate && (run.Config.P95LimitMS <= 0 || k.P95MS <= run.Config.P95LimitMS)
		res.K6WouldPass = &pass
	}
	res.Overall = Consistent
	for _, c := range res.Checks {
		switch c.Classification {
		case RedFlag:
			res.Overall = RedFlag
		case Explainable:
			if res.Overall == Consistent {
				res.Overall = Explainable
			}
		case NotComparable:
			if res.Overall != RedFlag {
				res.Overall = NotComparable
			}
		}
	}
	res.Notes = []string{
		"Run both generators against the same target state, with the generator on a separate monitored machine, and compare one level at a time.",
		"Differences labelled EXPLAINABLE_DIFFERENCE are expected from definitions; RED_FLAG means a Damocline conclusion is contradicted and must not be reported until explained.",
	}
	return res
}

// Markdown renders the comparison.
func Markdown(r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Damocline vs k6 comparison\n\n- Run: %s\n- Damocline outcome: %s\n", r.RunName, r.DamoclineOutcome)
	if r.K6WouldPass != nil {
		fmt.Fprintf(&b, "- Same pass criteria applied to k6: %t\n", *r.K6WouldPass)
	}
	fmt.Fprintf(&b, "- Overall: **%s**\n\n| Metric | Damocline | k6 | Classification | Explanation |\n|---|---:|---:|---|---|\n", r.Overall)
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "| %s | %g | %g | %s | %s |\n", c.Metric, c.Damocline, c.K6, c.Classification, c.Explanation)
	}
	b.WriteString("\n## Notes\n\n")
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	return b.String()
}
