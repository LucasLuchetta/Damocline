package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func good() Scenario {
	return Scenario{Name: "smoke", BaseURL: "http://127.0.0.1:9876", Users: 2, Duration: "1s", MaxRPS: 5, P95LimitMS: 1500, MaxErrorRate: .01, Steps: []Step{{Name: "home", Path: "/"}}}
}
func TestParseRequiresAuthorization(t *testing.T) {
	_, err := Parse(good(), Options{})
	if err == nil || !strings.Contains(err.Error(), "--authorized") {
		t.Fatalf("unexpected %v", err)
	}
}
func TestValid(t *testing.T) {
	v, err := Parse(good(), Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Steps[0].Method != "GET" {
		t.Fatalf("method = %s", v.Steps[0].Method)
	}
}
func TestHeadersUseEnv(t *testing.T) {
	t.Setenv("TEST_DAMOCLINE_COOKIE", "session=fake")
	s := good()
	s.Steps[0].Headers = map[string]string{"Cookie": "${TEST_DAMOCLINE_COOKIE}"}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Steps[0].ResolvedHeaders["Cookie"] != "session=fake" {
		t.Fatal("unresolved cookie")
	}
}
func TestRejectLiteralCookie(t *testing.T) {
	s := good()
	s.Steps[0].Headers = map[string]string{"Cookie": "session=private"}
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed literal cookie")
	}
}
func TestBlockWrites(t *testing.T) {
	s := good()
	s.Steps[0].Method = "POST"
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed writes")
	}
}
func TestBlockExternalHTTP(t *testing.T) {
	s := good()
	s.BaseURL = "http://example.com"
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed external plaintext HTTP")
	}
}
func TestRejectPathTraversal(t *testing.T) {
	s := good()
	s.Steps[0].Path = "/../private"
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed traversal")
	}
}
func TestRejectHighRPS(t *testing.T) {
	s := good()
	s.MaxRPS = 101
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed unbounded load")
	}
}
func TestMissingEnv(t *testing.T) {
	key := "UNLIKELY_DAMOCLINE_UNSET_TEST_98765"
	os.Unsetenv(key)
	s := good()
	s.Steps[0].Headers = map[string]string{"Cookie": "${" + key + "}"}
	_, err := Parse(s, Options{Authorized: true})
	if err == nil {
		t.Fatal("allowed missing environment variable")
	}
}

func TestSeparateSessionsRequiredAndUnique(t *testing.T) {
	t.Setenv("DAMOCLINE_TEST_VU_1", "session=alpha")
	t.Setenv("DAMOCLINE_TEST_VU_2", "session=beta")
	s := good()
	s.PerVUCookieEnvPrefix = "DAMOCLINE_TEST_VU_"
	v, err := Parse(s, Options{Authorized: true})
	if err != nil || len(v.Cookies) != 2 || v.Cookies[0] == v.Cookies[1] {
		t.Fatalf("cookies=%v err=%v", len(v.Cookies), err)
	}
	t.Setenv("DAMOCLINE_TEST_VU_2", "session=alpha")
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("duplicate cookie accepted")
	}
}
func TestArrivalValidation(t *testing.T) {
	s := good()
	s.Mode = "arrival"
	s.ArrivalRate = 10
	s.MaxRPS = 5
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("accepted unschedulable workload")
	}
	s.MaxRPS = 20
	s.RampUp = "1s"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("accepted arrival ramp")
	}
	s.RampUp = ""
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatal(err)
	}
}

func TestArrivalRateBoundMatchesGlobalRPSCeiling(t *testing.T) {
	s := good()
	s.Mode = "arrival"
	s.MaxRPS = 100
	s.ArrivalRate = 100
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatalf("100 arrivals/s should be allowed within max_rps: %v", err)
	}
	s.ArrivalRate = 101
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("accepted arrival rate above the bounded 100 RPS ceiling")
	}
}

func TestWeightedProfilesArePrepared(t *testing.T) {
	s := good()
	s.Users = 4
	s.Steps = nil
	s.Profiles = []Profile{
		{Name: "browse", Weight: 3, ThinkTime: "20ms", Steps: []Step{{Name: "dashboard", Path: "/dashboard"}}},
		{Name: "edit", Weight: 1, Steps: []Step{{Name: "save", Method: "POST", Path: "/save"}}},
	}
	v, err := Parse(s, Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Profiles) != 2 || len(v.Steps) != 2 || v.Profiles[0].ThinkTime != 20*time.Millisecond || v.Steps[1].Profile != "edit" {
		t.Fatalf("profiles were not prepared: %+v", v)
	}
}

func TestRejectInvalidWeightedProfiles(t *testing.T) {
	tests := []struct {
		name     string
		users    int
		steps    []Step
		profiles []Profile
	}{
		{
			name:  "steps and profiles",
			users: 2,
			steps: []Step{{Name: "legacy", Path: "/"}},
			profiles: []Profile{
				{Name: "browse", Weight: 1, Steps: []Step{{Name: "browse", Path: "/"}}},
				{Name: "search", Weight: 1, Steps: []Step{{Name: "search", Path: "/search"}}},
			},
		},
		{
			name:  "too few users",
			users: 1,
			profiles: []Profile{
				{Name: "browse", Weight: 1, Steps: []Step{{Name: "browse", Path: "/"}}},
				{Name: "search", Weight: 1, Steps: []Step{{Name: "search", Path: "/search"}}},
			},
		},
		{
			name:  "duplicate step names",
			users: 2,
			profiles: []Profile{
				{Name: "browse", Weight: 1, Steps: []Step{{Name: "page", Path: "/"}}},
				{Name: "search", Weight: 1, Steps: []Step{{Name: "page", Path: "/search"}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := good()
			s.Users = tt.users
			s.Steps = tt.steps
			s.Profiles = tt.profiles
			if _, err := Parse(s, Options{Authorized: true}); err == nil {
				t.Fatal("invalid profiles were accepted")
			}
		})
	}
}
