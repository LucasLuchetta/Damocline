package cli

import (
	"bytes"
	"fmt"
	"io"
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

func TestWelcomeEclipsePixelsAreTerminalFriendly(t *testing.T) {
	lines := strings.Split(eclipseArt, "\n")
	if len(lines) != 13 {
		t.Fatalf("unexpected Eclipse artwork height: %d", len(lines))
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\t\r") || strings.TrimRight(line, " ") != line {
			t.Fatalf("art line uses unstable terminal spacing: %q", line)
		}
		if len([]rune(line)) > 48 {
			t.Fatalf("art line is too wide for a compact terminal: %q", line)
		}
	}
	var plain, colored bytes.Buffer
	welcomeArt(&plain, false)
	welcomeArt(&colored, true)
	for _, fragment := range []string{"▄████████", "▀▀▀▀▀▀▀▀▀", "▼", "LOCAL-FIRST HTTP CAPACITY EXPLORER"} {
		if !strings.Contains(plain.String(), fragment) {
			t.Fatalf("missing Eclipse motif %q in: %s", fragment, plain.String())
		}
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("plain welcome contains ANSI escape codes")
	}
	if !strings.Contains(colored.String(), "\x1b[1;97m") || !strings.Contains(colored.String(), "\x1b[0m") {
		t.Fatalf("colored welcome missing ANSI formatting: %q", colored.String())
	}
}

type scriptedHomeKeyboard struct {
	keys []homeKey
	next int
	closed bool
}

func (k *scriptedHomeKeyboard) readKey() (homeKey, error) {
	if k.next >= len(k.keys) {
		return "", io.EOF
	}
	key := k.keys[k.next]
	k.next++
	return key, nil
}

func (k *scriptedHomeKeyboard) close() { k.closed = true }

func TestKeyboardHomeNavigation(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []homeKey
		hasCurrent bool
		want string
	}{
		{"enter picks first", []homeKey{homeKeyEnter}, false, "new"},
		{"down then enter picks history", []homeKey{homeKeyDown, homeKeyEnter}, false, "history"},
		{"two downs select current", []homeKey{homeKeyDown, homeKeyDown, homeKeyEnter}, true, "current"},
		{"up wraps to exit", []homeKey{homeKeyUp, homeKeyEnter}, false, "exit"},
		{"escape exits", []homeKey{homeKeyEscape}, true, "exit"},
		{"number shortcut", []homeKey{"4"}, false, "help"},
		{"unavailable current is not launched", []homeKey{homeKeyDown, homeKeyDown, homeKeyEnter, homeKeyUp, homeKeyUp, homeKeyEnter}, false, "new"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := wizard{input: bufioReader(""), out: &out}
			keys := &scriptedHomeKeyboard{keys: tt.keys}
			got, err := welcomeMenuWithKeyboard(&w, keys, tt.hasCurrent, false)
			if err != nil || got != tt.want {
				t.Fatalf("selected %q, err %v; want %q; output %s", got, err, tt.want, out.String())
			}
			if !strings.Contains(out.String(), "Navigate") {
				t.Fatalf("missing keyboard navigation hint: %s", out.String())
			}
			if !strings.Contains(out.String(), "> [1] New load test") {
				t.Fatalf("first option not highlighted: %s", out.String())
			}
			if len(tt.keys) > 1 && !strings.Contains(out.String(), "\x1b[") {
				t.Fatal("highlight was not redrawn for keyboard navigation")
			}
		})
	}
}

func TestWelcomeChecksBeforeSplashAndOnlyOnce(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		calls := 0
		check := func(w *wizard, stderr io.Writer, mode string) (bool, int) {
			calls++
			if mode != "default" {
				t.Fatalf("unexpected update mode: %q", mode)
			}
			fmt.Fprintln(w.out, "UPDATE CHECK FIRST")
			return false, 0
		}
		exit := welcomeWithUpdateCheck(strings.NewReader("4\n0\n"), &out, &errOut, check)
		if exit != 0 || calls != 1 {
			t.Fatalf("exit=%d check calls=%d stderr=%s", exit, calls, errOut.String())
		}
		got := out.String()
		if i, j := strings.Index(got, "UPDATE CHECK FIRST"), strings.Index(got, "D A M O C L I N E"); i < 0 || j <= i {
			t.Fatalf("update check must precede splash and menu: %s", got)
		}
		if !strings.Contains(got, "What would you like to do?") {
			t.Fatal("update check prevented ordinary menu display")
		}
	})
}

func TestWelcomeRestartsWithoutDrawingOldSplash(t *testing.T) {
	var out, errOut bytes.Buffer
	check := func(w *wizard, stderr io.Writer, mode string) (bool, int) {
		fmt.Fprintln(w.out, "UPDATE INSTALLED")
		return true, 7
	}
	if exit := welcomeWithUpdateCheck(strings.NewReader(""), &out, &errOut, check); exit != 7 {
		t.Fatalf("wanted restart exit code 7, got %d", exit)
	}
	if strings.Contains(out.String(), "D A M O C L I N E") ||
		strings.Contains(out.String(), "What would you like to do?") {
		t.Fatalf("rendered old menu before restart: %s", out.String())
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
