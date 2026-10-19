package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

func TestScenarioV1MigratesLegacyStepsAndWorkflows(t *testing.T) {
	legacy := good()
	migrated, err := MigrateScenario(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.ScenarioVersion != CurrentScenarioVersion || len(migrated.Workflows) != 1 || migrated.Workflows[0].Name != "default" || migrated.Workflows[0].Weight != 1 || len(migrated.Workflows[0].Steps) != 1 || len(migrated.Steps) != 0 {
		t.Fatalf("legacy single-workflow scenario did not migrate canonically: %+v", migrated)
	}
	validated, err := Parse(migrated, Options{Authorized: true})
	if err != nil {
		t.Fatalf("migrated scenario failed normal validation: %v", err)
	}
	if len(validated.Profiles) != 1 || validated.Profiles[0].Name != "default" || len(validated.Profiles[0].Steps) != 1 || validated.Profiles[0].Steps[0].Path != "/" {
		t.Fatalf("migrated workflow did not reach the engine model: %+v", validated.Profiles)
	}

	legacyProfiles := good()
	legacyProfiles.Steps = nil
	legacyProfiles.Profiles = []Profile{{Name: "read", Weight: 3, Steps: []Step{{Name: "list", Path: "/items"}}}, {Name: "search", Weight: 1, Steps: []Step{{Name: "query", Path: "/search"}}}}
	migratedProfiles, err := MigrateScenario(legacyProfiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(migratedProfiles.Workflows) != 2 || migratedProfiles.Workflows[0].Weight != 3 || migratedProfiles.Workflows[1].Name != "search" {
		t.Fatalf("legacy weighted profiles did not migrate intact: %+v", migratedProfiles.Workflows)
	}
	if _, err := Parse(migratedProfiles, Options{Authorized: true}); err != nil {
		t.Fatalf("migrated weighted scenario failed normal validation: %v", err)
	}
}

func TestScenarioV1LoaderAndParserRejectAmbiguousOrFutureFormats(t *testing.T) {
	base := good()
	base.ScenarioVersion = CurrentScenarioVersion
	base.Workflows = []Profile{{Name: "only", Weight: 1, Steps: []Step{{Name: "read", Path: "/"}}}}
	base.Steps = nil
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if validated, err := Parse(loaded, Options{Authorized: true}); err != nil || len(validated.Profiles) != 1 {
		t.Fatalf("versioned single workflow failed: profiles=%d err=%v", len(validated.Profiles), err)
	}

	for _, input := range []string{
		`{"scenario_version":2,"workflows":[{"name":"x","weight":1,"steps":[{"name":"a","path":"/"}]}]}`,
		`{"scenario_version":1,"steps":[{"name":"a","path":"/"}],"workflows":[{"name":"x","weight":1,"steps":[{"name":"a","path":"/"}]}]}`,
		`{"workflows":[{"name":"x","weight":1,"steps":[{"name":"a","path":"/"}]}]}`,
	} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("invalid versioned format accepted: %s", input)
		}
	}
}

func TestVersionedWorkflowExampleParses(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "versioned-weighted-workflows.json")
	scenario, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := Parse(scenario, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Profiles) != 2 || validated.Profiles[0].Name != "dashboard reads" || validated.Profiles[1].Weight != 1 {
		t.Fatalf("versioned example did not reach the validated workflow model: %+v", validated.Profiles)
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
	if v.Scenario.ConnectionPolicy != "reuse" {
		t.Fatalf("default connection policy = %q", v.Scenario.ConnectionPolicy)
	}
}

func TestPerProfileCookieContextsResolveDistinctActorSessions(t *testing.T) {
	t.Setenv("DAMO_A_1", "sid=a-one")
	t.Setenv("DAMO_A_2", "sid=a-two")
	t.Setenv("DAMO_B_1", "sid=b-one")
	t.Setenv("DAMO_B_2", "sid=b-two")
	s := good()
	s.Steps = nil
	s.Profiles = []Profile{
		{Name: "read", Weight: 1, PerVUCookieEnvPrefix: "DAMO_A_", Steps: []Step{{Name: "read", Path: "/read"}}},
		{Name: "write", Weight: 1, PerVUCookieEnvPrefix: "DAMO_B_", Steps: []Step{{Name: "write", Path: "/write"}}},
	}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if !v.ProfileAuthContexts || len(v.Profiles[0].Cookies) != 2 || len(v.Profiles[1].Cookies) != 2 || v.Cookies != nil {
		t.Fatalf("profile authentication contexts were not resolved: %+v", v)
	}
	if strings.Contains(v.WorkloadFingerprint, "a-one") || strings.Contains(v.WorkloadFingerprint, "DAMO_A_") {
		t.Fatalf("fingerprint contains credential material or environment names: %s", v.WorkloadFingerprint)
	}
	if got := WorkloadFingerprint(v); got != v.WorkloadFingerprint {
		t.Fatalf("fingerprint is not stable: %q != %q", got, v.WorkloadFingerprint)
	}
	t.Setenv("DAMO_A_1", "sid=changed")
	changedSecret, err := Parse(s, Options{Authorized: true})
	if err != nil || changedSecret.WorkloadFingerprint != v.WorkloadFingerprint {
		t.Fatalf("resolved secret values must not affect workload identity: %v", err)
	}
	withoutContexts := s
	withoutContexts.Profiles = append([]Profile(nil), s.Profiles...)
	withoutContexts.Profiles[0].PerVUCookieEnvPrefix = ""
	withoutContexts.Profiles[1].PerVUCookieEnvPrefix = ""
	withoutAuth, err := Parse(withoutContexts, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if withoutAuth.WorkloadFingerprint == v.WorkloadFingerprint {
		t.Fatal("workflow authentication isolation must change the workload fingerprint")
	}
}

func TestPerProfileCookieContextsRejectAmbiguousConfiguration(t *testing.T) {
	base := good()
	base.Steps = nil
	base.Profiles = []Profile{
		{Name: "first", Weight: 1, PerVUCookieEnvPrefix: "DAMO_A_", Steps: []Step{{Name: "first", Path: "/first"}}},
		{Name: "second", Weight: 1, PerVUCookieEnvPrefix: "DAMO_B_", Steps: []Step{{Name: "second", Path: "/second"}}},
	}
	t.Run("scenario and profile prefixes", func(t *testing.T) {
		s := base
		s.PerVUCookieEnvPrefix = "DAMO_GLOBAL_"
		if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("ambiguous prefixes accepted: %v", err)
		}
	})
	t.Run("shared cookie header", func(t *testing.T) {
		s := base
		s.Profiles = append([]Profile(nil), base.Profiles...)
		s.Profiles[0].Steps = []Step{{Name: "first", Path: "/first", Headers: map[string]string{"Cookie": "${DAMO_COOKIE}"}}}
		t.Setenv("DAMO_COOKIE", "sid=shared")
		if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "Cookie header") {
			t.Fatalf("shared cookie header accepted: %v", err)
		}
	})
	t.Run("duplicate prefixes", func(t *testing.T) {
		s := base
		s.Profiles = append([]Profile(nil), base.Profiles...)
		s.Profiles[1].PerVUCookieEnvPrefix = s.Profiles[0].PerVUCookieEnvPrefix
		if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "distinct") {
			t.Fatalf("duplicate profile prefixes accepted: %v", err)
		}
	})
	t.Run("malformed prefix", func(t *testing.T) {
		s := base
		s.Profiles = append([]Profile(nil), base.Profiles...)
		s.Profiles[0].PerVUCookieEnvPrefix = "invalid-prefix"
		if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "uppercase") {
			t.Fatalf("malformed profile prefix accepted: %v", err)
		}
	})
	t.Run("duplicate sessions across profiles", func(t *testing.T) {
		t.Setenv("DAMO_A_1", "sid=actor-one")
		t.Setenv("DAMO_A_2", "sid=actor-two")
		t.Setenv("DAMO_B_1", "sid=actor-one")
		t.Setenv("DAMO_B_2", "sid=other-two")
		if _, err := Parse(base, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "duplicate session cookie") {
			t.Fatalf("reused cross-workflow credentials accepted: %v", err)
		}
	})
}

func TestOneActorCanExecuteMultipleWeightedProfiles(t *testing.T) {
	s := good()
	s.Users = 1
	s.Steps = nil
	s.Profiles = []Profile{
		{Name: "first", Weight: 1, Steps: []Step{{Name: "first", Path: "/first"}}},
		{Name: "second", Weight: 1, Steps: []Step{{Name: "second", Path: "/second"}}},
	}
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatalf("journey-start selection should not require one actor per workflow: %v", err)
	}
}

func TestIsolatedAuthenticationExampleParsesWithoutPersistingCookieValues(t *testing.T) {
	for _, prefix := range []string{"DAMOCLINE_DASHBOARD_COOKIE_", "DAMOCLINE_SEARCH_COOKIE_"} {
		for actor := 1; actor <= 4; actor++ {
			t.Setenv(prefix+strconv.Itoa(actor), fmt.Sprintf("sid=%s-%d", prefix, actor))
		}
	}
	scenario, err := Load(filepath.Join("..", "..", "examples", "weighted-profiles-isolated-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Parse(scenario, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if !v.ProfileAuthContexts || len(v.Profiles) != 2 || v.Profiles[0].Cookies[0] == v.Profiles[1].Cookies[0] {
		t.Fatalf("example did not produce independent workflow identity contexts: %+v", v.Profiles)
	}
}

func TestThinkTimeRangeValidationAndFingerprint(t *testing.T) {
	base := good()
	base.Steps = []Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}
	base.ThinkTimeRange = &ThinkTimeRange{Min: "10ms", Max: "30ms"}
	validated, err := Parse(base, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if validated.Scenario.WorkloadSeed != 1 || validated.Profiles[0].ThinkTimeMin != 10*time.Millisecond || validated.Profiles[0].ThinkTimeMax != 30*time.Millisecond {
		t.Fatalf("unexpected timing policy: %+v", validated)
	}
	base.WorkloadSeed = 42
	seeded, err := Parse(base, Options{Authorized: true})
	if err != nil || seeded.WorkloadFingerprint == validated.WorkloadFingerprint {
		t.Fatalf("seed must change reproducible workload identity: %v", err)
	}
	base.ThinkTimeRange.Max = "40ms"
	changed, err := Parse(base, Options{Authorized: true})
	if err != nil || changed.WorkloadFingerprint == seeded.WorkloadFingerprint {
		t.Fatalf("range must change workload identity: %v", err)
	}
	for _, tc := range []struct {
		name string
		min  string
		max  string
	}{
		{name: "missing minimum", min: "", max: "10ms"},
		{name: "negative", min: "-1ms", max: "10ms"},
		{name: "reversed", min: "20ms", max: "10ms"},
		{name: "zero maximum", min: "0s", max: "0s"},
		{name: "too long", min: "0s", max: "31s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := good()
			invalid.ThinkTimeRange = &ThinkTimeRange{Min: tc.min, Max: tc.max}
			if _, err := Parse(invalid, Options{Authorized: true}); err == nil {
				t.Fatal("invalid range accepted")
			}
		})
	}
	conflicting := good()
	conflicting.ThinkTime = "10ms"
	conflicting.ThinkTimeRange = &ThinkTimeRange{Min: "10ms", Max: "20ms"}
	if _, err := Parse(conflicting, Options{Authorized: true}); err == nil {
		t.Fatal("fixed and ranged think time accepted together")
	}
	oneStep := good()
	oneStep.ThinkTimeRange = &ThinkTimeRange{Min: "10ms", Max: "20ms"}
	if _, err := Parse(oneStep, Options{Authorized: true}); err == nil {
		t.Fatal("range with no between-step boundary accepted")
	}
}

func TestProfileThinkTimeRangeOverrides(t *testing.T) {
	s := good()
	s.Steps = nil
	s.Users = 2
	s.ThinkTimeRange = &ThinkTimeRange{Min: "1ms", Max: "3ms"}
	s.Profiles = []Profile{
		{Name: "browse", Weight: 1, Steps: []Step{{Name: "browse-page", Path: "/browse"}, {Name: "browse-detail", Path: "/browse/detail"}}},
		{Name: "search", Weight: 1, ThinkTime: "2ms", Steps: []Step{{Name: "search-page", Path: "/search"}}},
	}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Profiles[0].ThinkTimeMin != time.Millisecond || v.Profiles[0].ThinkTimeMax != 3*time.Millisecond || v.Profiles[1].ThinkTimeMax != 0 || v.Profiles[1].ThinkTime != 2*time.Millisecond {
		t.Fatalf("profile override mismatch: %+v", v.Profiles)
	}
	s.Profiles[0].ThinkTime = "1ms"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("unused global range accepted")
	}
}

func TestWorkloadFingerprintIncludesProfileSelectionSemantics(t *testing.T) {
	s := Scenario{Name: "synthetic profiles", BaseURL: "http://127.0.0.1", Users: 2, Duration: "1s", MaxRPS: 10, P95LimitMS: 100, MaxErrorRate: 0,
		Profiles: []Profile{{Name: "read", Weight: 1, Steps: []Step{{Name: "read", Path: "/read"}}}, {Name: "write", Weight: 1, Steps: []Step{{Name: "write", Path: "/write"}}}}}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	perJourney := WorkloadFingerprint(v)
	if perJourney == "" {
		t.Fatal("per-journey workload fingerprint is empty")
	}
	legacySemantics := v
	legacySemantics.Scenario.Profiles = nil
	legacy := WorkloadFingerprint(legacySemantics)
	if legacy == "" || legacy == perJourney {
		t.Fatalf("profile selection semantics did not affect the declared workload fingerprint: new=%q old=%q", perJourney, legacy)
	}
}

func TestThinkTimeSamplesValidationAndFingerprint(t *testing.T) {
	s := good()
	s.Steps = []Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}
	s.ThinkTimeSamples = []string{"0ms", "10ms", "10ms"}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Scenario.WorkloadSeed != 1 || len(v.Profiles[0].ThinkTimeSamples) != 3 || v.Profiles[0].ThinkTime != 10*time.Millisecond {
		t.Fatalf("sampled policy not prepared: %+v", v.Profiles[0])
	}
	s.ThinkTimeSamples[0] = "5ms"
	changed, err := Parse(s, Options{Authorized: true})
	if err != nil || changed.WorkloadFingerprint == v.WorkloadFingerprint {
		t.Fatalf("sample change must alter fingerprint: %v", err)
	}
	s.WorkloadSeed = 42
	seeded, err := Parse(s, Options{Authorized: true})
	if err != nil || seeded.WorkloadFingerprint == changed.WorkloadFingerprint {
		t.Fatalf("seed change must alter fingerprint: %v", err)
	}
	for _, values := range [][]string{{}, {"1ms"}, {"-1ms", "1ms"}, {"1ms", "31s"}, {"bad", "1ms"}, make([]string, 33)} {
		invalid := s
		invalid.ThinkTimeSamples = values
		if _, err := Parse(invalid, Options{Authorized: true}); err == nil {
			t.Fatalf("invalid samples accepted: %v", values)
		}
	}
	conflict := s
	conflict.ThinkTimeRange = &ThinkTimeRange{Min: "1ms", Max: "2ms"}
	if _, err := Parse(conflict, Options{Authorized: true}); err == nil {
		t.Fatal("sample and uniform policies accepted together")
	}
	oneStep := s
	oneStep.Steps = oneStep.Steps[:1]
	if _, err := Parse(oneStep, Options{Authorized: true}); err == nil {
		t.Fatal("sample policy without an interval accepted")
	}
}

func TestProfileSamplesOverrideFixedPause(t *testing.T) {
	s := good()
	s.Steps = nil
	s.ThinkTime = "20ms"
	s.Profiles = []Profile{
		{Name: "sampled", Weight: 1, ThinkTimeSamples: []string{"0ms", "5ms"}, Steps: []Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}}},
		{Name: "fixed", Weight: 1, Steps: []Step{{Name: "fixed-first", Path: "/fixed-first"}, {Name: "fixed-second", Path: "/fixed-second"}}},
	}
	v, err := Parse(s, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Profiles[0].ThinkTime != 5*time.Millisecond || len(v.Profiles[0].ThinkTimeSamples) != 2 {
		t.Fatalf("sample override kept global fixed pause: %+v", v.Profiles[0])
	}
}

func TestConnectionPolicyIsBoundedAndFingerprintable(t *testing.T) {
	base, err := Parse(good(), Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	fresh := good()
	fresh.ConnectionPolicy = "new_per_request"
	newConnections, err := Parse(fresh, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if base.WorkloadFingerprint == newConnections.WorkloadFingerprint {
		t.Fatal("connection policy change did not change workload fingerprint")
	}
	explicitReuse := good()
	explicitReuse.ConnectionPolicy = "reuse"
	reuseValidated, err := Parse(explicitReuse, Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if base.WorkloadFingerprint != reuseValidated.WorkloadFingerprint {
		t.Fatal("explicit and default reuse policies must retain the same reference fingerprint")
	}
	invalid := good()
	invalid.ConnectionPolicy = "unbounded"
	if _, err := Parse(invalid, Options{Authorized: true}); err == nil {
		t.Fatal("unsupported connection policy was accepted")
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
			name:  "unknown query reference",
			steps: []Step{{Name: "session", Path: "/session", Extract: map[string]Extract{"id": {JSONPointer: "/id"}}}, {Name: "record", Path: "/records?id={{missing}}"}},
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

func TestQueryTemplateRequiresAnEarlierExtractedValue(t *testing.T) {
	s := good()
	s.Steps = []Step{
		{Name: "session", Path: "/session", Extract: map[string]Extract{"id": {JSONPointer: "/id"}}},
		{Name: "record", Path: "/records?filter={{id}}&fixed=one"},
	}
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatalf("valid extracted query template rejected: %v", err)
	}
	s.Steps[1].Path = "/records?filter={{id"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("malformed query template accepted")
	}
	s.Steps[1].Path = "/records?{{id}}=value"
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("query key template accepted")
	}
}

func TestJSONAssertionValidationIsBoundedAndRequiresValidPointer(t *testing.T) {
	s := good()
	s.Steps[0].JSONAssertions = map[string]string{"/state": "ready"}
	if _, err := Parse(s, Options{Authorized: true}); err != nil {
		t.Fatalf("valid JSON postcondition rejected: %v", err)
	}
	s.Steps[0].JSONAssertions = map[string]string{"/state~2name": "ready"}
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("invalid JSON pointer escape accepted")
	}
	s.Steps[0].JSONAssertions = map[string]string{"/state": strings.Repeat("x", 257)}
	if _, err := Parse(s, Options{Authorized: true}); err == nil {
		t.Fatal("oversized JSON assertion accepted")
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

func TestCleanupMustBeFinalSyntheticDeleteAndChangesFingerprint(t *testing.T) {
	t.Setenv("DAMOCLINE_TEST_DATA_PREFIX", "damocline-test")
	s := good()
	s.SyntheticDataPrefixEnv = "DAMOCLINE_TEST_DATA_PREFIX"
	s.Steps = []Step{{Name: "create", Method: "POST", Path: "/records/{{synthetic_id}}"}, {Name: "cleanup", Method: "DELETE", Path: "/records/{{synthetic_id}}", Cleanup: true}}
	base, err := Parse(s, Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Steps[1].Cleanup = false
	changed, err := Parse(s, Options{Authorized: true, AllowWrites: true})
	if err != nil || base.WorkloadFingerprint == changed.WorkloadFingerprint {
		t.Fatalf("cleanup semantic must change fingerprint: err=%v", err)
	}
	invalid := []Scenario{}
	noPrefix := s
	noPrefix.SyntheticDataPrefixEnv = ""
	noPrefix.Steps[1].Cleanup = true
	invalid = append(invalid, noPrefix)
	nonDelete := s
	nonDelete.Steps = append([]Step(nil), s.Steps...)
	nonDelete.Steps[1].Method = "POST"
	nonDelete.Steps[1].Cleanup = true
	invalid = append(invalid, nonDelete)
	unsafePath := s
	unsafePath.Steps = append([]Step(nil), s.Steps...)
	unsafePath.Steps[1].Path = "/records/all"
	unsafePath.Steps[1].Cleanup = true
	invalid = append(invalid, unsafePath)
	embeddedID := s
	embeddedID.Steps = append([]Step(nil), s.Steps...)
	embeddedID.Steps[1].Path = "/records/prefix-{{synthetic_id}}"
	embeddedID.Steps[1].Cleanup = true
	invalid = append(invalid, embeddedID)
	queryID := s
	queryID.Steps = append([]Step(nil), s.Steps...)
	queryID.Steps[1].Path = "/records?record={{synthetic_id}}"
	queryID.Steps[1].Cleanup = true
	invalid = append(invalid, queryID)
	notFinal := s
	notFinal.Steps = append([]Step(nil), s.Steps...)
	notFinal.Steps[1].Cleanup = true
	notFinal.Steps = append(notFinal.Steps, Step{Name: "after", Path: "/after"})
	invalid = append(invalid, notFinal)
	for i, scenario := range invalid {
		if _, err := Parse(scenario, Options{Authorized: true, AllowWrites: true}); err == nil {
			t.Errorf("invalid cleanup scenario %d accepted", i)
		}
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
