package calibration

import (
	"runtime/debug"
	"strings"
	"testing"
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
