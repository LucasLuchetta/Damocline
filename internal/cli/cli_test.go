package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), "run --config") {
		t.Fatalf("wrong help: %s", out.String())
	}
}
func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"unrecognized"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
}
func TestRunNeedsConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"run", "--authorized"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
}
