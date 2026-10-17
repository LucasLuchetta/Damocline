package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadEnforcesExactScenarioByteLimit(t *testing.T) {
	encoded, err := json.Marshal(good())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	valid := make([]byte, maxScenarioBytes)
	copy(valid, encoded)
	for i := len(encoded); i < len(valid); i++ {
		valid[i] = ' '
	}
	if err := os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("scenario exactly at the input limit was rejected: %v", err)
	}
	if err := os.WriteFile(path, append(valid, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("oversized scenario was not rejected clearly: %v", err)
	}
}

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

func TestProfileMixProvenanceDefaultsAndValidates(t *testing.T) {
	s := good()
	v, err := Parse(s, Options{Authorized: true})
	if err != nil || v.Scenario.ProfileMixBasis != "assumption" {
		t.Fatalf("default basis=%q err=%v", v.Scenario.ProfileMixBasis, err)
	}
	tests := []struct {
		basis, reference string
		valid            bool
	}{
		{"sanitized_analytics", "weekly-summary-v2", true},
		{"business_estimate", "planning-review", true},
		{"unknown", "source-1", false},
		{"sanitized_analytics", "", false},
		{"assumption", "source-1", false},
		{"sanitized_analytics", "https://private.example", false},
		{"sanitized_analytics", "source with spaces", false},
	}
	for _, tt := range tests {
		t.Run(tt.basis+"/"+tt.reference, func(t *testing.T) {
			s := good()
			s.ProfileMixBasis, s.ProfileMixReference = tt.basis, tt.reference
			_, err := Parse(s, Options{Authorized: true})
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
		})
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

func TestRejectInvalidDynamicWorkflowReferences(t *testing.T) {
	tests := []struct {
		name  string
		steps []Step
	}{
		{
			name:  "forward reference",
			steps: []Step{{Name: "record", Path: "/records/{{id}}"}},
		},
		{
			name:  "query reference",
			steps: []Step{{Name: "session", Path: "/session", Extract: map[string]Extract{"id": {JSONPointer: "/id"}}}, {Name: "record", Path: "/records?id={{id}}"}},
		},
		{
			name:  "invalid extractor",
			steps: []Step{{Name: "session", Path: "/session", Extract: map[string]Extract{"id": {JSONPointer: "/id", Header: "X-ID"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := good()
			s.Steps = tt.steps
			if _, err := Parse(s, Options{Authorized: true}); err == nil {
				t.Fatal("invalid dynamic workflow was accepted")
			}
		})
	}
}

func TestWindowDurationValidation(t *testing.T) {
	s := good()
	s.WindowDuration = "500ms"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("sub-second window was accepted")
	}
	s.WindowDuration = "2s"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("window longer than scenario duration was accepted")
	}
}

func TestSyntheticDataPrefixEnablesOnlyTheSyntheticIDTemplate(t *testing.T) {
	t.Setenv("DAMOCLINE_TEST_DATA_PREFIX", "damocline-test")
	s := good()
	s.SyntheticDataPrefixEnv = "DAMOCLINE_TEST_DATA_PREFIX"
	s.Steps = []Step{{Name: "create", Method: "POST", Path: "/records/{{synthetic_id}}"}}
	v, err := Parse(s, Options{Authorized: true, AllowWrites: true})
	if err != nil || v.SyntheticDataPrefix != "damocline-test" {
		t.Fatalf("synthetic prefix was not prepared: %+v err=%v", v, err)
	}
	s.SyntheticDataPrefixEnv = "bad-name"
	if _, err := Parse(s, Options{Authorized: true, AllowWrites: true}); err == nil {
		t.Fatal("invalid synthetic prefix environment name was accepted")
	}
}

func TestWorkloadRevisionAndEnvironmentLabelsAreValidated(t *testing.T) {
	s := good()
	s.WorkloadRevision = "checkout-v3.2"
	s.Environment = "staging-small"
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatalf("safe reproducibility labels rejected: %v", err)
	}
	s.Environment = "production cluster"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("unsafe environment label accepted")
	}
}
