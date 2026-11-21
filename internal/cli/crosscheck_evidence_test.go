package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrosscheckCLIRejectsUnassessableServerEvidence(t *testing.T) {
	dir := t.TempDir()
	runPath := filepath.Join(dir, "run.json")
	logPath := filepath.Join(dir, "access.log")
	if err := os.WriteFile(runPath, []byte(crosscheckRun), 0o600); err != nil {
		t.Fatal(err)
	}
	// The run has three 2xx responses, while only one matching server
	// response was logged. This falls within the historical count tolerance.
	line := `127.0.0.1 - - [02/Jan/2026:10:00:01 +0000] "GET / HTTP/1.1" 200 2 "-" "Damocline" 0.004` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := crosscheckCmd([]string{"--run", runPath, "--access-log", logPath, "--format", "nginx"}, &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "Overall: **NOT_ASSESSABLE**") {
		t.Fatalf("incomplete server evidence must block a successful CLI exit: code=%d\nstderr=%s\n%s", code, errb.String(), out.String())
	}

	// An empty log with a permissive tolerance still cannot certify a match.
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	code = crosscheckCmd([]string{
		"--run", runPath, "--access-log", logPath, "--format", "nginx",
		"--count-abs-tolerance", "5",
	}, &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "Overall: **NOT_ASSESSABLE**") {
		t.Fatalf("missing server evidence must block a successful CLI exit: code=%d\nstderr=%s\n%s", code, errb.String(), out.String())
	}
}
