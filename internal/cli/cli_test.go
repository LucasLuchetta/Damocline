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

func TestSlashCommandHelpAndHistoryRouting(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		if code := Run([]string{"help"}, &out, &errOut); code != 0 {
			t.Fatalf("help exit=%d", code)
		}
		if !strings.Contains(out.String(), "damocline /start") || !strings.Contains(out.String(), "damocline /history") {
			t.Fatalf("slash commands absent from help: %s", out.String())
		}
		out.Reset()
		if code := Run([]string{"/history"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "No saved scenarios") {
			t.Fatalf("empty history exit=%d output=%s stderr=%s", code, out.String(), errOut.String())
		}
		out.Reset()
		if code := Run([]string{"/history", "extra"}, &out, &errOut); code != 2 {
			t.Fatalf("history must reject unknown arguments, exit=%d", code)
		}
		if code := Run([]string{"/start", "extra"}, &out, &errOut); code != 2 {
			t.Fatalf("new scenario must reject unknown arguments, exit=%d", code)
		}
	})
}
