package crosscheck

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

var runStart = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

func runReport(ok2xx, n5xx int, p95 float64) report.Result {
	m := report.MetricSet{Total: ok2xx + n5xx, Success: ok2xx, Failures: n5xx, P95MS: p95}
	m.HTTP.Status2xx, m.HTTP.Status5xx = ok2xx, n5xx
	return report.Result{SchemaVersion: 50, Name: "synthetic", StartedAt: runStart.Format(time.RFC3339Nano), ElapsedSeconds: 60,
		Steps: []report.StepResult{{Name: "home", Method: "GET", Path: "/", Metrics: m}}}
}

func nginxLines(start time.Time, n int, status int, path string, sec float64) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		t := start.Add(time.Duration(i%55) * time.Second).In(time.FixedZone("", -3*3600))
		fmt.Fprintf(&b, "127.0.0.1 - - [%s] \"GET %s HTTP/1.1\" %d 12 \"-\" \"Damocline\" %.3f\n", t.Format(clfTime), path, status, sec)
	}
	return b.String()
}

func find(t *testing.T, r Result, metric string) MetricCheck {
	t.Helper()
	for _, c := range r.Steps[0].Checks {
		if c.Metric == metric {
			return c
		}
	}
	t.Fatalf("metric %s missing", metric)
	return MetricCheck{}
}

func TestNginxLogMatches(t *testing.T) {
	log := nginxLines(runStart.Add(time.Second), 100, 200, "/?q=1", 0.010) +
		nginxLines(runStart.Add(time.Second), 3, 503, "/", 0.002) + "garbage\n" +
		nginxLines(runStart, 5, 200, "/other", 0.001)
	p, err := ParseLog(strings.NewReader(log), FormatNginx)
	if err != nil || p.Unparsed != 1 {
		t.Fatalf("parse: %v %d", err, p.Unparsed)
	}
	r, err := Compare(runReport(100, 3, 25), p, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall != Match || r.ClockSkew.Suspected || r.UnattributedInWin != 5 {
		t.Fatalf("unexpected: %s skew=%v unattr=%d\n%s", r.Overall, r.ClockSkew.Suspected, r.UnattributedInWin, Markdown(r))
	}
	if c := find(t, r, "p95_ms"); c.Verdict != Match || c.Server != 10 {
		t.Fatalf("p95 %+v", c)
	}
}

func TestCountMismatchAndServerSlowerThanClient(t *testing.T) {
	p, _ := ParseLog(strings.NewReader(nginxLines(runStart, 80, 200, "/", 0.5)), FormatNginx)
	r, _ := Compare(runReport(100, 0, 25), p, FormatNginx, DefaultOptions())
	if r.Overall != Mismatch || find(t, r, "requests").Verdict != Mismatch || find(t, r, "p95_ms").Verdict != Mismatch {
		t.Fatalf("expected mismatches\n%s", Markdown(r))
	}
}

func TestClockSkewFlagged(t *testing.T) {
	p, _ := ParseLog(strings.NewReader(nginxLines(runStart.Add(3*time.Minute), 100, 200, "/", 0.01)), FormatNginx)
	r, _ := Compare(runReport(100, 0, 25), p, FormatNginx, DefaultOptions())
	if !r.ClockSkew.Suspected || r.Overall != Mismatch || r.ClockSkew.EstimatedOffsetSeconds < 170 {
		t.Fatalf("skew not flagged: %+v %s", r.ClockSkew, r.Overall)
	}
}

func TestCombinedHasNoDurationAndPathMap(t *testing.T) {
	log := strings.ReplaceAll(nginxLines(runStart, 10, 200, "/app/", 0.01), " 0.010\n", "\n")
	p, _ := ParseLog(strings.NewReader(log), FormatCombined)
	o := DefaultOptions()
	pm, err := ParsePathMap("/app/=/")
	if err != nil {
		t.Fatal(err)
	}
	o.PathMaps = []PathMap{pm}
	r, _ := Compare(runReport(10, 0, 25), p, FormatCombined, o)
	if find(t, r, "requests").Verdict != Match || find(t, r, "p95_ms").Verdict != NotAssessable {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestJSONLogAndPlaceholders(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, `{"time":%q,"method":"get","uri":"/items/%d","status":200,"duration_ms":4}`+"\n", runStart.Add(time.Duration(i)*time.Second).Format(time.RFC3339), i)
	}
	p, err := ParseLog(strings.NewReader(b.String()), FormatJSON)
	if err != nil || len(p.Entries) != 20 {
		t.Fatalf("%v %d", err, len(p.Entries))
	}
	run := runReport(20, 0, 9)
	run.Steps[0].Path = "/items/${data.id}"
	r, _ := Compare(run, p, FormatJSON, DefaultOptions())
	if r.Overall != Match {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestUnknownFormatAndBadRun(t *testing.T) {
	if _, err := ParseLog(strings.NewReader(""), "iis"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Compare(report.Result{}, ParseResult{}, FormatJSON, DefaultOptions()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParsePathMap("noequals"); err == nil {
		t.Fatal("expected error")
	}
}
