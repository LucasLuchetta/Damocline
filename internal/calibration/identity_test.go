package calibration

import (
	"regexp"
	"testing"
)

func TestSourceIdentityIsPathFreeAndReproducible(t *testing.T) {
	method, identity := SourceIdentity()
	if method == "unavailable" || identity == "" {
		t.Fatalf("source identity unavailable in test binary: method=%q", method)
	}
	if matched, _ := regexp.MatchString(`^[A-Za-z0-9._@+-]{1,120}$`, identity); !matched {
		t.Fatalf("source identity contains unsupported or path-like data: %q", identity)
	}
	if method != "git_commit" && method != "executable_sha256" && method != "module_version" {
		t.Fatalf("unexpected identity method %q", method)
	}
}
