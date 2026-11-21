package externalcompare

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

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
	s := summary(t, `{"metrics":{"http_reqs":{"values":{"count":1000,"rate":10.2}},"http_req_failed":{"values":{"rate":0.001}},"http_req_duration":{"values":{"p(95)":190}},"vus_max":{"values":{"value":10,"max":10}}}}`)
	r := Compare(run(1000, 200, 0, "PASS"), s, DefaultOptions())
	if r.Overall != Consistent || r.K6WouldPass == nil || !*r.K6WouldPass {
		t.Fatalf("%s", Markdown(r))
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
	if r.Overall != Explainable {
		t.Fatalf("%s", Markdown(r))
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
