package config

import (
	"strings"
	"testing"
)

func TestWorkloadFingerprintIsStableAndOpaque(t *testing.T) {
	t.Setenv("DAMOCLINE_TOKEN", "private-token-value")
	t.Setenv("DAMOCLINE_SESSION_1", "sid=session-one")
	t.Setenv("DAMOCLINE_SESSION_2", "sid=session-two")
	base := Scenario{
		Name: "fingerprint", BaseURL: "https://app.example.com", Users: 2, Duration: "1s", MaxRPS: 20,
		P95LimitMS: 1000, PerVUCookieEnvPrefix: "DAMOCLINE_SESSION_",
		Steps: []Step{{Name: "read", Path: "/items", Headers: map[string]string{
			"Accept": "application/json", "Authorization": "${DAMOCLINE_TOKEN}",
		}, ExpectedStatus: []int{200, 202}, Extract: map[string]Extract{"item_id": {JSONPointer: "/id"}}}},
	}
	first := preparedFingerprint(t, base)
	second := preparedFingerprint(t, base)
	if first == "" || first != second || !strings.HasPrefix(first, "sha256:") || strings.Contains(first, "DAMOCLINE_TOKEN") {
		t.Fatalf("fingerprint is missing, unstable, or not opaque: %q %q", first, second)
	}

	reordered := base
	reordered.Steps = append([]Step(nil), base.Steps...)
	reordered.Steps[0].Headers = map[string]string{
		"Authorization": "${DAMOCLINE_TOKEN}", "Accept": "application/json",
	}
	reordered.Steps[0].ExpectedStatus = []int{202, 200}
	if got := preparedFingerprint(t, reordered); got != first {
		t.Fatalf("semantically unordered header/status fields changed the fingerprint: %q != %q", got, first)
	}
}

func TestWorkloadFingerprintChangesWhenJourneyDefinitionChanges(t *testing.T) {
	t.Setenv("DAMOCLINE_BODY", "synthetic-body")
	base := Scenario{Name: "fingerprint", BaseURL: "https://app.example.com", Users: 2, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000, Steps: []Step{{Name: "read", Method: "GET", Path: "/items", ExpectedStatus: []int{200}}}}
	want := preparedFingerprint(t, base)
	cases := map[string]func(*Scenario){
		"path":      func(s *Scenario) { s.Steps[0].Path = "/orders" },
		"header":    func(s *Scenario) { s.Steps[0].Headers = map[string]string{"Accept": "text/html"} },
		"assertion": func(s *Scenario) { s.Steps[0].MustContain = "ready" },
		"JSON postcondition": func(s *Scenario) {
			s.Steps[0].JSONAssertions = map[string]string{"/state": "ready"}
		},
		"body source": func(s *Scenario) { s.Steps[0].Method = "POST"; s.Steps[0].BodyEnv = "DAMOCLINE_BODY" },
		"extraction":  func(s *Scenario) { s.Steps[0].Extract = map[string]Extract{"id": {JSONPointer: "/id"}} },
		"profile weight": func(s *Scenario) {
			s.Steps = nil
			s.Profiles = []Profile{{Name: "browse", Weight: 2, Steps: []Step{{Name: "read", Method: "GET", Path: "/items", ExpectedStatus: []int{200}}}}, {Name: "search", Weight: 1, Steps: []Step{{Name: "search", Path: "/search"}}}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base
			changed.Steps = append([]Step(nil), base.Steps...)
			mutate(&changed)
			if got := preparedFingerprint(t, changed); got == "" || got == want {
				t.Fatalf("journey change did not produce a distinct fingerprint: %q", got)
			}
		})
	}
}

func TestWorkloadFingerprintCapturesEnvironmentBodyLengthWithoutContents(t *testing.T) {
	t.Setenv("DAMOCLINE_BODY", "synthetic-value-one")
	s := Scenario{Name: "fingerprint", BaseURL: "https://app.example.com", Users: 1, Duration: "1s", MaxRPS: 20, P95LimitMS: 1000, Steps: []Step{{Name: "write", Method: "POST", Path: "/items", BodyEnv: "DAMOCLINE_BODY"}}}
	first := preparedFingerprint(t, s)
	t.Setenv("DAMOCLINE_BODY", strings.Repeat("z", len("synthetic-value-one")))
	if sameSize := preparedFingerprint(t, s); sameSize != first {
		t.Fatalf("body content with the same byte length should not enter the fingerprint: %q != %q", sameSize, first)
	}
	t.Setenv("DAMOCLINE_BODY", "another-value-with-a-different-size")
	second := preparedFingerprint(t, s)
	if first == "" || second == "" || first == second || strings.Contains(first, "synthetic-value-one") || strings.Contains(second, "another-value-with-a-different-size") {
		t.Fatalf("body shape was not distinguished safely: first=%q second=%q", first, second)
	}
}

func preparedFingerprint(t *testing.T, scenario Scenario) string {
	t.Helper()
	validated, err := Parse(scenario, Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatalf("parse fingerprint fixture: %v", err)
	}
	return validated.WorkloadFingerprint
}
