package crosscheck

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

// Verdict values. MATCH and MISMATCH are only issued when both sides carry
// the evidence; otherwise the metric is NOT_ASSESSABLE.
const (
	Match         = "MATCH"
	Mismatch      = "MISMATCH"
	NotAssessable = "NOT_ASSESSABLE"
)

// Options declare the tolerances under which counts are called equal.
type Options struct {
	// ClockTolerance widens the run window on both sides to absorb log
	// timestamp resolution and small clock offsets.
	ClockTolerance time.Duration
	// CountRelTolerance and CountAbsTolerance bound count differences:
	// allowed = max(abs, rel*expected).
	CountRelTolerance float64
	CountAbsTolerance int
	// DurationRelTolerance and DurationAbsMS bound how much the server p95
	// may exceed the client p95 before it is a mismatch.
	DurationRelTolerance float64
	DurationAbsMS        float64
	// PathMaps rewrite log path prefixes before matching (reverse proxies).
	PathMaps []PathMap
}

// PathMap replaces a log path prefix From with To.
type PathMap struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DefaultOptions are the declared default tolerances.
func DefaultOptions() Options {
	return Options{ClockTolerance: 2 * time.Second, CountRelTolerance: 0.01, CountAbsTolerance: 2, DurationRelTolerance: 0.10, DurationAbsMS: 5}
}

// ParsePathMap parses "from=to".
func ParsePathMap(s string) (PathMap, error) {
	from, to, ok := strings.Cut(s, "=")
	if !ok || !strings.HasPrefix(from, "/") || !strings.HasPrefix(to, "/") {
		return PathMap{}, fmt.Errorf("path map %q must be /log-prefix=/scenario-prefix", s)
	}
	return PathMap{From: from, To: to}, nil
}

// MetricCheck is one compared metric.
type MetricCheck struct {
	Metric      string  `json:"metric"`
	Damocline   float64 `json:"damocline"`
	Server      float64 `json:"server"`
	AllowedLow  float64 `json:"allowed_low,omitempty"`
	AllowedHigh float64 `json:"allowed_high,omitempty"`
	Verdict     string  `json:"verdict"`
	Explanation string  `json:"explanation"`
}

// StepCheck groups metrics for one method and path pattern.
type StepCheck struct {
	Steps   []string      `json:"steps"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Checks  []MetricCheck `json:"checks"`
	Entries int           `json:"server_entries"`
}

// ClockSkew records whether log timestamps appear offset from the run window.
type ClockSkew struct {
	Suspected              bool    `json:"suspected"`
	EstimatedOffsetSeconds float64 `json:"estimated_offset_seconds"`
	Explanation            string  `json:"explanation"`
}

// Result is the cross-check outcome.
type Result struct {
	RunName           string      `json:"run_name"`
	WindowStart       string      `json:"window_start"`
	WindowEnd         string      `json:"window_end"`
	ClockToleranceSec float64     `json:"clock_tolerance_seconds"`
	LogFormat         string      `json:"log_format"`
	LogLines          int         `json:"log_lines"`
	UnparsedLines     int         `json:"unparsed_lines"`
	EntriesInWindow   int         `json:"entries_in_window"`
	UnattributedInWin int         `json:"unattributed_entries_in_window"`
	PathMaps          []PathMap   `json:"path_maps,omitempty"`
	ClockSkew         ClockSkew   `json:"clock_skew"`
	Steps             []StepCheck `json:"steps"`
	Overall           string      `json:"overall"`
	Limitations       []string    `json:"limitations"`
}

type matcher struct {
	key    string
	method string
	path   string
	re     *regexp.Regexp
	steps  []report.StepResult
	hits   []Entry
}

var placeholder = regexp.MustCompile(`\$\{[^}]*\}`)

func compilePath(p string) *regexp.Regexp {
	p, _, _ = strings.Cut(p, "?")
	parts := placeholder.Split(p, -1)
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, "[^/]+") + "$")
}

func (o Options) mapPath(p string) string {
	p, _, _ = strings.Cut(p, "?")
	for _, m := range o.PathMaps {
		if strings.HasPrefix(p, m.From) {
			return strings.Replace(p, m.From, m.To, 1)
		}
	}
	return p
}

// Compare checks a Damocline run against parsed server log entries.
func Compare(run report.Result, log ParseResult, format string, o Options) (Result, error) {
	start, err := time.Parse(time.RFC3339Nano, run.StartedAt)
	if err != nil {
		return Result{}, fmt.Errorf("run report has no valid started_at: %w", err)
	}
	if run.ElapsedSeconds <= 0 {
		return Result{}, fmt.Errorf("run report has no positive elapsed_seconds")
	}
	end := start.Add(time.Duration(run.ElapsedSeconds * float64(time.Second)))
	lo, hi := start.Add(-o.ClockTolerance), end.Add(o.ClockTolerance)

	var ms []*matcher
	index := map[string]*matcher{}
	for _, s := range run.Steps {
		method := strings.ToUpper(s.Method)
		key := method + " " + s.Path
		m, ok := index[key]
		if !ok {
			m = &matcher{key: key, method: method, path: s.Path, re: compilePath(s.Path)}
			index[key] = m
			ms = append(ms, m)
		}
		m.steps = append(m.steps, s)
	}
	res := Result{RunName: run.Name, WindowStart: start.UTC().Format(time.RFC3339Nano), WindowEnd: end.UTC().Format(time.RFC3339Nano),
		ClockToleranceSec: o.ClockTolerance.Seconds(), LogFormat: format, LogLines: log.Lines, UnparsedLines: log.Unparsed, PathMaps: o.PathMaps}

	// Matching entries near the window (±10 min) drive skew detection.
	var nearby []time.Time
	for _, e := range log.Entries {
		path := o.mapPath(e.Path)
		var hit *matcher
		for _, m := range ms {
			if m.method == e.Method && m.re.MatchString(path) {
				hit = m
				break
			}
		}
		inWin := !e.Time.Before(lo) && !e.Time.After(hi)
		if hit != nil && e.Time.After(start.Add(-10*time.Minute)) && e.Time.Before(end.Add(10*time.Minute)) {
			nearby = append(nearby, e.Time)
		}
		if !inWin {
			continue
		}
		res.EntriesInWindow++
		if hit == nil {
			res.UnattributedInWin++
			continue
		}
		hit.hits = append(hit.hits, e)
	}

	inWindow, expected := 0, 0
	for _, m := range ms {
		inWindow += len(m.hits)
		for _, s := range m.steps {
			expected += responses(s.Metrics.HTTP)
		}
	}
	res.ClockSkew = detectSkew(nearby, start, end, inWindow, expected, o.ClockTolerance)

	overall := Match
	for _, m := range ms {
		sc := checkStep(m, o)
		for _, c := range sc.Checks {
			switch c.Verdict {
			case Mismatch:
				overall = Mismatch
			case NotAssessable:
				// Missing server evidence must prevent a positive count or
				// status verdict. Logs without durations can still reconcile
				// counts and status classes independently.
				if c.Metric != "p95_ms" && overall == Match {
					overall = NotAssessable
				}
			}
		}
		res.Steps = append(res.Steps, sc)
	}
	if res.ClockSkew.Suspected && overall == Match {
		overall = NotAssessable
	}
	if len(ms) == 0 {
		overall = NotAssessable
	}
	res.Overall = overall
	res.Limitations = []string{
		"Damocline requests that failed before a response (timeouts, connection or transport failures, cancellations) may or may not reach the server log; they widen the upper count bound only.",
		"Server duration is the time the logging server measured (for example nginx $request_time); it excludes client-side network and queueing time, so it is expected to be at most the client latency.",
		"Requests logged by other clients in the same window are reported as unattributed and are not subtracted from Damocline's counters.",
	}
	if res.UnattributedInWin > 0 {
		res.Limitations = append(res.Limitations, fmt.Sprintf("%d in-window log entries did not match any scenario step: the server carried other traffic during the run, which can affect capacity conclusions.", res.UnattributedInWin))
	}
	if log.Unparsed > 0 {
		res.Limitations = append(res.Limitations, fmt.Sprintf("%d log lines could not be parsed in %s format and were excluded.", log.Unparsed, format))
	}
	return res, nil
}

func responses(h report.HTTPDiagnostics) int {
	return h.Status1xx + h.Status2xx + h.Status3xx + h.Status4xx + h.Status5xx
}

func detectSkew(nearby []time.Time, start, end time.Time, inWindow, expected int, tol time.Duration) ClockSkew {
	if len(nearby) == 0 {
		return ClockSkew{Explanation: "No matching log entries within 10 minutes of the run window; skew cannot be estimated."}
	}
	sort.Slice(nearby, func(i, j int) bool { return nearby[i].Before(nearby[j]) })
	offset := nearby[0].Sub(start).Seconds()
	outside := len(nearby) - inWindow
	if expected > 0 && outside > 0 && float64(outside) >= 0.05*float64(expected) {
		return ClockSkew{Suspected: true, EstimatedOffsetSeconds: round(offset),
			Explanation: fmt.Sprintf("%d matching entries fall outside the run window widened by %s (first matching entry at %+.1fs from start). The log clock or time zone may be offset from the generator clock; rerun with a larger --clock-tolerance only if the offset is understood.", outside, tol, offset)}
	}
	return ClockSkew{EstimatedOffsetSeconds: round(offset), Explanation: "Matching entries fall inside the run window within the declared tolerance."}
}

func (o Options) allowed(expected int) float64 {
	return math.Max(float64(o.CountAbsTolerance), o.CountRelTolerance*float64(expected))
}

func checkStep(m *matcher, o Options) StepCheck {
	sc := StepCheck{Method: m.method, Path: m.path, Entries: len(m.hits)}
	var h report.HTTPDiagnostics
	unresolved := 0
	for _, s := range m.steps {
		sc.Steps = append(sc.Steps, s.Name)
		d := s.Metrics.HTTP
		h.Status1xx += d.Status1xx
		h.Status2xx += d.Status2xx
		h.Status3xx += d.Status3xx
		h.Status4xx += d.Status4xx
		h.Status5xx += d.Status5xx
		if u := s.Metrics.Total - responses(d); u > 0 {
			unresolved += u
		}
	}
	server := [6]int{}
	var durations []float64
	for _, e := range m.hits {
		if c := e.Status / 100; c >= 1 && c <= 5 {
			server[c]++
		}
		if e.DurationMS >= 0 {
			durations = append(durations, e.DurationMS)
		}
	}
	total := h.Status1xx + h.Status2xx + h.Status3xx + h.Status4xx + h.Status5xx
	count := func(metric string, dam, srv int) MetricCheck {
		tol := o.allowed(dam)
		c := MetricCheck{Metric: metric, Damocline: float64(dam), Server: float64(srv), AllowedLow: math.Max(0, float64(dam)-tol), AllowedHigh: float64(dam+unresolved) + tol}
		switch {
		case float64(srv) < c.AllowedLow:
			c.Verdict = Mismatch
			c.Explanation = fmt.Sprintf("Server logged %d fewer than Damocline's lower bound: Damocline may count responses the server never logged (wrong path map, other virtual host, cache or CDN answering, or log sampling).", int(c.AllowedLow)-srv)
		case float64(srv) > c.AllowedHigh:
			c.Verdict = Mismatch
			c.Explanation = fmt.Sprintf("Server logged %d more than Damocline's upper bound: other clients, retries, redirects followed by the generator, or a too-wide window may be inflating the server count.", srv-int(c.AllowedHigh))
		case len(m.hits) == 0:
			c.Verdict = NotAssessable
			c.Explanation = "No matching server log entries. An absolute count tolerance cannot substitute for independent evidence."
		case metric != "requests" && dam != srv:
			// The count tolerance permits a small difference in the number
			// of logged requests, not a contradictory HTTP status class.
			// Exact, fully observed totals make a class difference a mismatch;
			// otherwise the missing/unresolved work prevents verification.
			if unresolved == 0 && len(m.hits) == total {
				c.Verdict = Mismatch
				c.Explanation = fmt.Sprintf("Client and server both accounted for %d requests, but the HTTP status class differs (%d vs %d); count tolerance does not apply.", total, dam, srv)
			} else {
				c.Verdict = NotAssessable
				c.Explanation = fmt.Sprintf("HTTP status class differs (%d vs %d) within count tolerance, with incomplete request reconciliation; status evidence is not verified.", dam, srv)
			}
		default:
			c.Verdict = Match
			c.Explanation = fmt.Sprintf("Within declared tolerance (%.0f, plus %d Damocline requests without a response).", tol, unresolved)
		}
		return c
	}
	sc.Checks = append(sc.Checks, count("requests", total, len(m.hits)))
	sc.Checks = append(sc.Checks, count("status_2xx", h.Status2xx, server[2]))
	sc.Checks = append(sc.Checks, count("status_3xx", h.Status3xx, server[3]))
	sc.Checks = append(sc.Checks, count("status_4xx", h.Status4xx, server[4]))
	sc.Checks = append(sc.Checks, count("status_5xx", h.Status5xx, server[5]))
	sc.Checks = append(sc.Checks, durationCheck(m, durations, o))
	return sc
}

func durationCheck(m *matcher, durations []float64, o Options) MetricCheck {
	c := MetricCheck{Metric: "p95_ms", Verdict: NotAssessable}
	switch {
	case len(m.steps) != 1:
		c.Explanation = "Several steps share this method and path; per-step client p95 values cannot be pooled."
		return c
	case len(m.hits) == 0 || len(durations) < len(m.hits)/2+1:
		c.Explanation = "The log does not carry a server-side duration for most matching entries."
		return c
	case m.steps[0].Metrics.Total == 0:
		c.Explanation = "Damocline recorded no requests for this step."
		return c
	}
	sort.Float64s(durations)
	c.Server = round(durations[int(math.Ceil(0.95*float64(len(durations))))-1])
	c.Damocline = m.steps[0].Metrics.P95MS
	c.AllowedHigh = round(c.Damocline*(1+o.DurationRelTolerance) + o.DurationAbsMS)
	if c.Server > c.AllowedHigh {
		c.Verdict = Mismatch
		c.Explanation = "Server-side p95 exceeds Damocline's client p95 beyond tolerance. A client cannot observe less time than the server spent; check the log duration unit, clock window and path map, or Damocline's timing."
		return c
	}
	c.Verdict = Match
	c.Explanation = fmt.Sprintf("Server p95 is not above client p95; the %.1f ms gap is network, TLS, client queueing and proxy time (expected, not a defect).", round(c.Damocline-c.Server))
	return c
}

func round(v float64) float64 { return math.Round(v*1000) / 1000 }

// Markdown renders a human-readable report.
func Markdown(r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Damocline access-log cross-check\n\n")
	fmt.Fprintf(&b, "- Run: %s\n- Window: %s to %s (±%.1fs)\n- Log: %s, %d lines, %d unparsed, %d entries in window, %d unattributed\n- Overall: **%s**\n\n",
		r.RunName, r.WindowStart, r.WindowEnd, r.ClockToleranceSec, r.LogFormat, r.LogLines, r.UnparsedLines, r.EntriesInWindow, r.UnattributedInWin, r.Overall)
	skew := "not suspected"
	if r.ClockSkew.Suspected {
		skew = "**SUSPECTED**"
	}
	fmt.Fprintf(&b, "## Clock skew\n\n%s (estimated offset %+.1fs). %s\n\n", skew, r.ClockSkew.EstimatedOffsetSeconds, r.ClockSkew.Explanation)
	for _, s := range r.Steps {
		fmt.Fprintf(&b, "## %s %s (%s)\n\n| Metric | Damocline | Server | Allowed | Verdict | Explanation |\n|---|---:|---:|---|---|---|\n", s.Method, s.Path, strings.Join(s.Steps, ", "))
		for _, c := range s.Checks {
			allowed := "-"
			if c.Verdict != NotAssessable {
				allowed = fmt.Sprintf("%g..%g", c.AllowedLow, c.AllowedHigh)
			}
			fmt.Fprintf(&b, "| %s | %g | %g | %s | %s | %s |\n", c.Metric, c.Damocline, c.Server, allowed, c.Verdict, c.Explanation)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Limitations\n\n")
	for _, l := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}
