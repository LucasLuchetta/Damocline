package crosscheck

import (
	"strings"
	"testing"
	"time"
)

func TestCrosscheckNoServerEvidenceIsNotAMatch(t *testing.T) {
	// The default absolute count tolerance is two. A single client
	// response and zero server entries used to be reported as MATCH.
	r, err := Compare(runReport(1, 0, 25), ParseResult{}, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall != NotAssessable || find(t, r, "requests").Verdict != NotAssessable ||
		find(t, r, "status_2xx").Verdict != NotAssessable {
		t.Fatalf("missing independent evidence cannot be a match:\n%s", Markdown(r))
	}
}

func TestCrosscheckExactCountWrongStatusIsMismatch(t *testing.T) {
	// Even though the count tolerance is two, the independent server
	// observed HTTP 503 where Damocline recorded HTTP 200.
	p, err := ParseLog(strings.NewReader(nginxLines(runStart.Add(time.Second), 1, 503, "/", 0.01)), FormatNginx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(runReport(1, 0, 25), p, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall != Mismatch || find(t, r, "requests").Verdict != Match ||
		find(t, r, "status_2xx").Verdict != Mismatch ||
		find(t, r, "status_5xx").Verdict != Mismatch {
		t.Fatalf("a contradictory HTTP status must not be tolerated:\n%s", Markdown(r))
	}
}

func TestCrosscheckIncompleteStatusReconciliationIsNotAssessable(t *testing.T) {
	p, err := ParseLog(strings.NewReader(nginxLines(runStart, 2, 200, "/", 0.01)), FormatNginx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(runReport(3, 0, 25), p, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if find(t, r, "requests").Verdict != Match ||
		find(t, r, "status_2xx").Verdict != NotAssessable || r.Overall != NotAssessable {
		t.Fatalf("small count tolerance cannot approve incomplete status evidence:\n%s", Markdown(r))
	}
}

func TestCrosscheckOneFullyReconciledRequestStillMatches(t *testing.T) {
	p, err := ParseLog(strings.NewReader(nginxLines(runStart, 1, 200, "/", 0.01)), FormatNginx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(runReport(1, 0, 25), p, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall != Match {
		t.Fatalf("exact independent evidence should still match:\n%s", Markdown(r))
	}
}

func TestCrosscheckMissingOneOfMultipleStepsIsNotAssessable(t *testing.T) {
	run := runReport(1, 0, 25)
	missing := run.Steps[0]
	missing.Name = "missing"
	missing.Path = "/missing"
	run.Steps = append(run.Steps, missing)
	p, err := ParseLog(strings.NewReader(nginxLines(runStart, 1, 200, "/", 0.01)), FormatNginx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(run, p, FormatNginx, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Steps) != 2 || r.Overall != NotAssessable ||
		r.Steps[1].Checks[0].Verdict != NotAssessable {
		t.Fatalf("an unobserved step cannot inherit another step's MATCH:\n%s", Markdown(r))
	}
}
