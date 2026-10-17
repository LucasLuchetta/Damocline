package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestWelcomeArtIsReadableWithoutANSI(t *testing.T) {
	var out bytes.Buffer
	welcomeArt(&out, false)
	if !strings.Contains(out.String(), "D A M O C L I N E") ||
		!strings.Contains(out.String(), "Find the limit before it finds you.") ||
		!strings.Contains(out.String(), "▼") {
		t.Fatalf("missing branded artwork: %s", out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("artwork contains ANSI codes when color is disabled")
	}
}

func TestWelcomeMenuChoices(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
		hasCurrent bool
	}{
		{"new by default", "\n", "new", false},
		{"history", "2\n", "history", false},
		{"existing profile", "3\n", "current", true},
		{"unavailable current retries", "3\n1\n", "new", false},
		{"help", "4\n", "help", false},
		{"exit", "0\n", "exit", true},
		{"invalid retries", "8\n1\n", "new", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := wizard{input: bufioReader(tt.input), out: &out}
			got, err := welcomeMenu(&w, tt.hasCurrent, false)
			if err != nil || got != tt.want {
				t.Fatalf("got=%q err=%v want=%q output=%s", got, err, tt.want, out.String())
			}
		})
	}
}

func TestWelcomeHelpAndExitDoNotSendTraffic(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		if code := welcome(strings.NewReader("4\n0\n"), &out, &errOut); code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "run --config") ||
			!strings.Contains(out.String(), "Goodbye") {
			t.Fatalf("help or exit was missing: %s", out.String())
		}
		if _, err := os.Stat(scenarioPath); !os.IsNotExist(err) {
			t.Fatalf("unexpected persisted scenario: %v", err)
		}
		if strings.Contains(out.String(), "\x1b[") {
			t.Fatal("redirected output should not contain ANSI codes")
		}
	})
}

func TestWelcomeEOFIsSafe(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		if code := welcome(strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "Goodbye") {
			t.Fatalf("missing exit text: %s", out.String())
		}
	})
}

func TestWelcomeNewTestCannotSendTrafficWithoutAuthorization(t *testing.T) {
	runInTempDir(t, func() {
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			w.WriteHeader(200)
		}))
		defer server.Close()

		input := strings.Join([]string{
			"1", server.URL, "Sample read-only", "/sample", "", "1",
			"1", "1", "1s", "5", "DENIED",
		}, "\n") + "\n"
		var out, errOut bytes.Buffer
		if code := welcome(strings.NewReader(input), &out, &errOut); code != 0 {
			t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
		}
		if requests.Load() != 0 {
			t.Fatalf("unapproved menu traffic: %d requests", requests.Load())
		}
		scenario, err := config.Load(scenarioPath)
		if err != nil || scenario.Name != "Sample read-only" {
			t.Fatalf("new scenario not persisted: %+v %v", scenario, err)
		}
	})
}

func TestWelcomeSavedChoicesStillRequireAuthorization(t *testing.T) {
	runInTempDir(t, func() {
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			w.WriteHeader(200)
		}))
		defer server.Close()
		scenario := exampleHistoryScenario(server.URL, "Synthetic example")
		prefs := startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}
		if err := saveActiveProfile(scenario, prefs); err != nil {
			t.Fatal(err)
		}

		for _, input := range []string{
			"3\nAUTHORIZE\nn\n", // current, explicit refusal
			"2\n1\nDENIED\n",    // history, no authorization
		} {
			var out, errOut bytes.Buffer
			if code := welcome(strings.NewReader(input), &out, &errOut); code != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
		}
		if requests.Load() != 0 {
			t.Fatalf("saved scenario traffic was not authorized: %d requests", requests.Load())
		}
	})
}
