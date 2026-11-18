package calibration

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestSourceIdentityRequiresExplicitCleanRevision(t *testing.T) {
	revision := strings.Repeat("A", 40)
	digest := strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, modified, wantMethod, wantIdentity string
	}{
		{"clean", "false", "git_commit", strings.ToLower(revision)},
		{"dirty", "true", "executable_sha256", digest},
		{"missing", "", "executable_sha256", digest},
		{"malformed", "unknown", "executable_sha256", digest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}}
			if tc.modified != "" {
				info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.modified", Value: tc.modified})
			}
			method, identity := sourceIdentity(info, func() string {
				if tc.modified == "false" {
					t.Fatal("clean source unexpectedly read executable")
				}
				return digest
			})
			if method != tc.wantMethod || identity != tc.wantIdentity {
				t.Fatalf("identity=(%q, %q), want (%q, %q)", method, identity, tc.wantMethod, tc.wantIdentity)
			}
		})
	}
}

func TestSourceIdentityDoesNotUseModuleVersionOrInvalidDigest(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	for _, value := range []string{"", strings.Repeat("a", 24), strings.Repeat("z", 64)} {
		method, identity := sourceIdentity(info, func() string { return value })
		if method != "unavailable" || identity != "" {
			t.Fatalf("unverifiable identity accepted: %q %q", method, identity)
		}
	}
}

func TestSourceIdentityRequiresValidRevisionEvenWithCleanMetadata(t *testing.T) {
	digest := strings.Repeat("b", 64)
	for _, revision := range []string{"", "123456789abc", strings.Repeat("z", 40)} {
		info := &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "false"},
		}}
		method, identity := sourceIdentity(info, func() string { return digest })
		if method != "executable_sha256" || identity != digest {
			t.Fatalf("invalid clean revision accepted: revision=%q method=%q identity=%q", revision, method, identity)
		}
	}
	method, identity := sourceIdentity(nil, func() string { return digest })
	if method != "executable_sha256" || identity != digest {
		t.Fatalf("missing build info lost executable identity: %q %q", method, identity)
	}
}

func TestSourceRunnerVersionRetainsFullIdentity(t *testing.T) {
	method, identity := SourceIdentity()
	prefix := "binary-"
	if method == "git_commit" {
		prefix = "source-"
	}
	if got := SourceRunnerVersion(); got != prefix+identity {
		t.Fatalf("runner identity %q does not retain full %s identity %q", got, method, identity)
	}
}

func TestLegacyRunnerIdentityRemainsReadableWithoutMatchingFullIdentity(t *testing.T) {
	for _, tc := range []struct{ name, full, short string }{
		{"source", "source-" + strings.Repeat("a", 40), "source-" + strings.Repeat("a", 12)},
		{"binary", "binary-" + strings.Repeat("b", 64), "binary-" + strings.Repeat("b", 24)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := passingArtifact(t)
			artifact.Preflight.Generator.RunnerVersion = tc.short
			data, err := json.Marshal(artifact)
			if err != nil {
				t.Fatal(err)
			}
			record, err := ValidateArtifactBytes(data, true)
			if err != nil {
				t.Fatalf("legacy identity became unreadable: %v", err)
			}
			local := testRuntime()
			local.RunnerVersion = tc.full
			scenario := config.Scenario{Name: "synthetic", BaseURL: "http://127.0.0.1:8080", Mode: "arrival", ArrivalRate: 10, Users: 10,
				MaxRPS: 50, Duration: "5s", Timeout: "1s", Steps: []config.Step{{Name: "read", Method: "GET", Path: "/work"}}}
			assessment := AssessScenario(scenario, record, local, time.Now().UTC())
			for _, dimension := range assessment.Dimensions {
				if dimension.Name == "generator_runtime" {
					if dimension.Status != "OUTSIDE_REFERENCE" {
						t.Fatalf("short identity matched full identity: %+v", dimension)
					}
					return
				}
			}
			t.Fatal("generator comparison was not recorded")
		})
	}
}
