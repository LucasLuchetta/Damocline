package cli

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func exampleHistoryScenario(base, name string) config.Scenario {
	return config.Scenario{
		Name: name, BaseURL: base, Users: 1, Duration: "1s",
		RampUp: "100ms", ThinkTime: "50ms", Timeout: "1s",
		MaxRPS: 5, P95LimitMS: 1500, MaxErrorRate: 0.01,
		MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "home", Method: "GET", Path: "/",
			ExpectedStatus: []int{200}}},
	}
}

func TestHistoryArchivesPreviousScenarioAndSupportsSelection(t *testing.T) {
	runInTempDir(t, func() {
		p := startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}
		first := exampleHistoryScenario("http://127.0.0.1:12345", "first")
		second := exampleHistoryScenario("http://127.0.0.1:12345", "second")
		if err := saveActiveProfile(first, p); err != nil { t.Fatal(err) }
		if err := saveActiveProfile(second, p); err != nil { t.Fatal(err) }
		options, err := savedHistory()
		if err != nil { t.Fatal(err) }
		if len(options) != 2 || options[0].Scenario.Name != "second" || !options[0].Current ||
			options[1].Scenario.Name != "first" {
			t.Fatalf("wrong history options: %+v", options)
		}
		var stdout bytes.Buffer
		w := wizard{input: bufioReader("2\n"), out: &stdout}
		selected, selectedPrefs, err := w.selectHistory()
		if err != nil || selected.Name != "first" || selectedPrefs.Kind != "run" {
			t.Fatalf("selection: scenario=%+v preferences=%+v err=%v", selected, selectedPrefs, err)
		}
		if err := saveActiveProfile(selected, selectedPrefs); err != nil { t.Fatal(err) }
		options, err = savedHistory()
		if err != nil || len(options) != 2 { t.Fatalf("history grew with duplicate snapshots: %d %v", len(options), err) }
		if !options[0].Current || options[0].Scenario.Name != "first" { t.Fatalf("active scenario not restored: %+v", options) }
		if err := saveActiveProfile(second, p); err != nil { t.Fatal(err) }
		options, err = savedHistory()
		if err != nil || len(options) != 2 { t.Fatalf("history grew again: %d %v", len(options), err) }
		entries, err := os.ReadDir(historyDir)
		if err != nil || len(entries) != 1 { t.Fatalf("unexpected history file count: %d %v", len(entries), err) }
	})
}

func bufioReader(content string) *bufio.Reader { return bufio.NewReader(strings.NewReader(content)) }

func TestHistoryRejectsLiteralHeaders(t *testing.T) {
	runInTempDir(t, func() {
		p := startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}
		s := exampleHistoryScenario("http://localhost:12345", "with literal")
		s.Steps[0].Headers = map[string]string{"X-Private-Token": "some-secret-value"}
		if err := saveActiveProfile(s, p); err != nil { t.Fatal(err) }
		if err := saveActiveProfile(exampleHistoryScenario(s.BaseURL, "new"), p); err == nil {
			t.Fatal("literal headers must not be archived")
		}
		files, _ := filepath.Glob(filepath.Join(historyDir, "*.json"))
		if len(files) != 0 { t.Fatalf("unexpected archived secret: %v", files) }
		current, _, err := loadStartProfile()
		if err != nil || current.Name != s.Name { t.Fatalf("failed to preserve old scenario on archive error: %v %+v", err, current) }
	})
}

func TestHistoryStoresVariableReferencesOnly(t *testing.T) {
	runInTempDir(t, func() {
		p := startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1, AuthMode: "browser", LoginPath: "/login"}
		s := exampleHistoryScenario("https://app.example.test", "cookies")
		s.Steps[0].Headers = map[string]string{"Cookie": "${DAMOCLINE_SESSION_COOKIE}"}
		if err := saveActiveProfile(s, p); err != nil { t.Fatal(err) }
		if err := saveActiveProfile(exampleHistoryScenario(s.BaseURL, "new"), startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}); err != nil { t.Fatal(err) }
		files, err := filepath.Glob(filepath.Join(historyDir, "*.json"))
		if err != nil || len(files) != 1 { t.Fatalf("history snapshot: %v %v", files, err) }
		data, err := os.ReadFile(files[0])
		if err != nil { t.Fatal(err) }
		if !bytes.Contains(data, []byte("${DAMOCLINE_SESSION_COOKIE}")) {
			t.Fatal("cookie must be an environment variable reference")
		}
		info, err := os.Stat(files[0])
		if err != nil || info.Mode().Perm()&0077 != 0 { t.Fatalf("history permissions: %v %v", info, err) }
	})
}

func TestHistoryCancelledAndEmptyNeverSendsTraffic(t *testing.T) {
	runInTempDir(t, func() {
		var out, errOut bytes.Buffer
		if code := startWithMode(strings.NewReader(""), &out, &errOut, "history"); code != 0 {
			t.Fatalf("empty history: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "No saved scenarios") { t.Fatal("missing empty state") }
		if err := saveActiveProfile(exampleHistoryScenario("http://localhost:12345", "safe"), startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}); err != nil { t.Fatal(err) }
		out.Reset()
		if code := startWithMode(strings.NewReader("0\n"), &out, &errOut, "history"); code != 0 {
			t.Fatalf("cancelled history: code=%d", code)
		}
		if !strings.Contains(out.String(), "No test started") { t.Fatal("missing cancellation message") }
	})
}

func TestHistoryReplayRequiresFreshAuthorization(t *testing.T) {
	runInTempDir(t, func() {
		var hits atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusOK) }))
		defer server.Close()
		p := startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}
		s := exampleHistoryScenario(server.URL, "replay")
		if err := saveActiveProfile(s, p); err != nil { t.Fatal(err) }
		for _, input := range []string{"1\nDENIED\n", "1\nAUTHORIZE\nn\n"} {
			var out, errOut bytes.Buffer
			if code := startWithMode(strings.NewReader(input), &out, &errOut, "history"); code != 0 {
				t.Fatalf("replay code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
			}
		}
		if hits.Load() != 0 { t.Fatalf("history replay sent %d requests without approval", hits.Load()) }
	})
}

func TestHistorySelectionCancelError(t *testing.T) {
	runInTempDir(t, func() {
		if err := saveActiveProfile(exampleHistoryScenario("http://localhost:12345", "sample"), startPreferences{Kind: "run", MinUsers: 1, MaxUsers: 1}); err != nil { t.Fatal(err) }
		w := wizard{input: bufioReader("0\n"), out: &bytes.Buffer{}}
		_, _, err := w.selectHistory()
		if !errors.Is(err, errHistoryCancelled) { t.Fatalf("expected cancel, got %v", err) }
	})
}
