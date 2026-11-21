package externalcompare

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const testFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func run(total int, p95, errRate float64, outcome string) report.Result {
	return report.Result{Name: "synthetic", Outcome: outcome, ObservationSeconds: 100, ErrorsRate: errRate,
		Config:  report.RunConfig{Users: 10, Mode: "closed", MaxErrorRate: 0.01, P95LimitMS: 500, MaxRPS: 50},
		Metrics: report.MetricSet{Total: total, P95MS: p95}}
}

func summary(t *testing.T, js string) K6Summary {
	t.Helper()
	s, err := ParseK6Summary([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConsistentHandleSummaryShape(t *testing.T) {
	const js = `{"metrics":{"http_reqs":{"values":{"count":1000,"rate":10.2}},"http_req_failed":{"values":{"rate":0}},"http_req_duration":{"values":{"p(95)":190}},"vus_max":{"values":{"value":10,"max":10}},"checks":{"values":{"passes":1000,"fails":0}}},"root_group":{"checks":[{"name":%q,"passes":1000,"fails":0}]}}`
	s := summary(t, fmt.Sprintf(js, checkName(testFingerprint, 0)))
	observed := run(1000, 200, 0, "PASS")
	observed.Config.WorkloadFingerprint = testFingerprint
	observed.Steps = []report.StepResult{{Name: "read"}}
	r := Compare(observed, s, DefaultOptions())
	if r.Overall != Consistent || r.K6WouldPass == nil || !*r.K6WouldPass {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestBusinessChecksQualifyExternalPass(t *testing.T) {
	const prefix = `{"metrics":{"http_reqs":{"count":1000,"rate":10},"http_req_failed":{"value":0},"http_req_duration":{"p(95)":200},"vus_max":{"max":10}`
	name := checkName(testFingerprint, 0)
	checks := func(passed, failed int, check string) string {
		return fmt.Sprintf(
			prefix+`,"checks":{"passes":%d,"fails":%d}},"root_group":{"checks":{%q:{"passes":%d,"fails":%d}}}}`,
			passed, failed, check, passed, failed,
		)
	}
	tests := []struct {
		name, summary, overall string
		failed                 bool
		extraStep              bool
	}{
		{name: "wrong content", summary: checks(999, 1, name), overall: RedFlag, failed: true},
		{name: "wrong content in legacy checks", summary: checks(0, 1000, "read success"), overall: Explainable, failed: true},
		{name: "checks missing", summary: prefix + `}}`, overall: NotComparable},
		{name: "checks incomplete", summary: checks(999, 0, name), overall: NotComparable},
		{name: "step omitted", summary: checks(1000, 0, name), overall: NotComparable, extraStep: true},
		{name: "different workload", summary: checks(1000, 0, checkName("sha256:other", 0)), overall: NotComparable},
		{name: "only aggregate checks", summary: prefix + `,"checks":{"passes":1000,"fails":0}}}`, overall: NotComparable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observed := run(1000, 200, 0, "PASS")
			observed.Config.WorkloadFingerprint = testFingerprint
			observed.Steps = []report.StepResult{{Name: "read"}}
			if test.extraStep {
				observed.Steps = append(observed.Steps, report.StepResult{Name: "search"})
			}
			r := Compare(observed, summary(t, test.summary), DefaultOptions())
			if r.Overall != test.overall {
				t.Fatal(Markdown(r))
			}
			if test.failed && (r.K6WouldPass == nil || *r.K6WouldPass) {
				t.Fatal("a failed business check must veto passing")
			}
			if !test.failed && r.K6WouldPass != nil {
				t.Fatal("incomplete or unverified business checks cannot establish a verdict")
			}
		})
	}
}

func TestRedFlagApprovedButK6Fails(t *testing.T) {
	s := summary(t, `{"metrics":{"http_reqs":{"count":1000,"rate":10},"http_req_failed":{"value":0.08},"http_req_duration":{"p(95)":900},"vus_max":{"max":10}}}`)
	r := Compare(run(1000, 200, 0, "PASS"), s, DefaultOptions())
	if r.Overall != RedFlag || *r.K6WouldPass {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestThroughputGapIsRedFlag(t *testing.T) {
	s := summary(t, `{"metrics":{"http_reqs":{"count":2000,"rate":20},"http_req_failed":{"value":0},"http_req_duration":{"p(95)":200},"vus_max":{"max":10}}}`)
	r := Compare(run(1000, 200, 0, "PASS"), s, DefaultOptions())
	if r.Checks[1].Classification != RedFlag {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestLatencyDefinitionExplainable(t *testing.T) {
	s := summary(t, `{"metrics":{"http_reqs":{"count":1000,"rate":10},"http_req_failed":{"value":0},"http_req_duration":{"p(95)":100},"vus_max":{"max":10}}}`)
	r := Compare(run(1000, 200, 0, "PASS"), s, DefaultOptions())
	if r.Overall != NotComparable || r.Checks[3].Classification != Explainable {
		t.Fatalf("%s", Markdown(r))
	}
}

func TestK6ScriptLabelsChecksWithWorkloadFingerprint(t *testing.T) {
	scenario := config.Scenario{
		Name: "synthetic", BaseURL: "http://127.0.0.1", Mode: "closed",
		Users: 1, Duration: "10s", RampUp: "0s", Timeout: "2s", MaxRPS: 5, P95LimitMS: 500,
		Steps: []config.Step{{Name: "read", Path: "/work", ExpectedStatus: []int{200}, MustContain: "marker"}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	js, err := K6Script(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, checkName(validated.WorkloadFingerprint, 0)) ||
		!strings.Contains(js, "http.expectedStatuses(...[200])") {
		t.Fatalf("missing generated success contract in %s", js)
	}
	scenario.RampUp = "1s"
	js, err = K6Script(scenario)
	if err != nil || strings.Contains(js, "damocline:v1:") {
		t.Fatalf("the converter cannot claim equivalence for a ramped workload: %v", err)
	}
}

func TestDifferentVUsNotComparable(t *testing.T) {
	s := summary(t, `{"metrics":{"http_reqs":{"count":1000,"rate":10},"vus_max":{"max":5}}}`)
	r := Compare(run(1000, 200, 0, "PASS"), s, DefaultOptions())
	if r.Overall != NotComparable {
		t.Fatalf("%s", Markdown(r))
	}
	if _, err := ParseK6Summary([]byte(`{"metrics":{}}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestK6Script(t *testing.T) {
	s := config.Scenario{Users: 4, Duration: "30s", ThinkTime: "1s", Timeout: "3s", Steps: []config.Step{
		{Name: "home", Method: "GET", Path: "/", MustContain: "ok", Headers: map[string]string{"Authorization": "Bearer secret-value", "X-Tenant": "${TENANT}"}},
		{Name: "api", Method: "GET", Path: "/api", ExpectedStatus: []int{200, 304}},
	}}
	js, err := K6Script(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vus: 4", `duration: "30s"`, `env("K6_H_0_AUTHORIZATION")`, `env("TENANT")`, `[200,304].includes(res.status)`, `res.body.includes("ok")`, `timeout: "3s"`} {
		if !strings.Contains(js, want) {
			t.Fatalf("missing %q in\n%s", want, js)
		}
	}
	if strings.Contains(js, "secret-value") {
		t.Fatal("header value leaked into script")
	}
	if strings.Count(js, "sleep(1)") != 1 {
		t.Fatalf("think time should only separate steps\n%s", js)
	}
	s.Mode = "arrival"
	if _, err := K6Script(s); err == nil {
		t.Fatal("arrival mode must be rejected")
	}
}
