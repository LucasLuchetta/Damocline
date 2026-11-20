package config

import (
	"strings"
	"testing"
	"time"
)

func baselineTestScenario() Scenario {
	return Scenario{Name: "b", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 5, P95LimitMS: 500,
		SyntheticDataPrefixEnv: "DAMOCLINE_TEST_PREFIX",
		Steps:                  []Step{{Name: "search", Path: "/search/{{synthetic_id}}"}, {Name: "home", Method: "HEAD", Path: "/home?x=1"}}}
}

func TestNetworkBaselinePlanDefaultsToFirstLiteralGetStep(t *testing.T) {
	t.Setenv("DAMOCLINE_TEST_PREFIX", "synthetic")
	s := baselineTestScenario()
	v, err := Parse(s, Options{Authorized: true, NetworkBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	p := v.NetworkBaseline
	if p == nil || p.Method != "HEAD" || p.Path != "/home" || p.Source != NetworkBaselineSourceStep || p.Requests != NetworkBaselineRequests ||
		p.Interval != 200*time.Millisecond || p.Timeout > NetworkBaselineMaxRequestTime {
		t.Fatalf("unexpected plan: %+v", p)
	}
	if v2, err := Parse(s, Options{Authorized: true}); err != nil || v2.NetworkBaseline != nil {
		t.Fatalf("baseline planned without being requested: %+v err=%v", v2.NetworkBaseline, err)
	}
}

func TestNetworkBaselineRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	for _, path := range []string{"https://other.example.com/", "//other.example.com/", "/a/{{x}}", "/../admin", "relative", "/a#b"} {
		s := baselineTestScenario()
		s.SyntheticDataPrefixEnv = ""
		s.Steps = []Step{{Name: "home", Path: "/"}}
		s.BaselinePath = path
		if _, err := Parse(s, Options{Authorized: true, NetworkBaseline: true}); err == nil {
			t.Fatalf("unsafe baseline_path %q accepted", path)
		}
	}
	s := Scenario{Name: "w", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 5, P95LimitMS: 500,
		Steps: []Step{{Name: "write", Method: "POST", Path: "/items"}}}
	if _, err := Parse(s, Options{Authorized: true, AllowWrites: true, NetworkBaseline: true}); err == nil || !strings.Contains(err.Error(), "network baseline requires") {
		t.Fatalf("write-only scenario did not require an explicit baseline choice: %v", err)
	}
	s.BaselinePath = "/health"
	v, err := Parse(s, Options{Authorized: true, AllowWrites: true, NetworkBaseline: true})
	if err != nil || v.NetworkBaseline.Method != "GET" || v.NetworkBaseline.URL != "https://app.example.com/health" {
		t.Fatalf("explicit baseline_path not used: %+v err=%v", v.NetworkBaseline, err)
	}
	s.BaselinePath = ""
	s.NetworkBaselineSkipReason = "bad reason!"
	if _, err := Parse(s, Options{Authorized: true, AllowWrites: true, NetworkBaseline: true}); err == nil {
		t.Fatal("unsafe skip reason accepted")
	}
	s.NetworkBaselineSkipReason = "write-only-lab"
	v, err = Parse(s, Options{Authorized: true, AllowWrites: true, NetworkBaseline: true})
	if err != nil || v.NetworkBaseline.SkipReason != "write-only-lab" {
		t.Fatalf("explicit skip not planned: %+v err=%v", v.NetworkBaseline, err)
	}
}
