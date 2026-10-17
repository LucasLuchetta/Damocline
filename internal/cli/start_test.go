package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func runInTempDir(t *testing.T, fn func()) {
	t.Helper()
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err = os.Chdir(before); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}
func TestStartNeverSendsUnapprovedTraffic(t *testing.T) {
	runInTempDir(t, func() {
		var hits atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(200) }))
		defer srv.Close()
		input := strings.Join([]string{srv.URL, "Local test", "/sample", "", "1", "1", "2", "1s", "5", "DENIED"}, "\n") + "\n"
		var out, errOut bytes.Buffer
		code := start(strings.NewReader(input), &out, &errOut)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
		}
		if hits.Load() != 0 {
			t.Fatal("network requests sent without authorization")
		}
		info, err := os.Stat(scenarioPath)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			t.Fatalf("scenario permissions=%s", info.Mode())
		}
	})
}
func TestStartReusesSavedScenarioAndRunsLocalTest(t *testing.T) {
	runInTempDir(t, func() {
		var hits atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(200) }))
		defer srv.Close()
		s := config.Scenario{Name: "test", BaseURL: srv.URL, Users: 2, Duration: "6s", RampUp: "1s", ThinkTime: "100ms", Timeout: "2s", MaxRPS: 20, P95LimitMS: 1500, MaxErrorRate: 0.01, MinSamplesPerStep: 5, Steps: []config.Step{{Name: "home", Path: "/", Method: "GET", ExpectedStatus: []int{200}}}}
		p := startPreferences{Kind: "run", MinUsers: 2, MaxUsers: 2}
		if err := writePrivateJSON(scenarioPath, s); err != nil {
			t.Fatal(err)
		}
		if err := writePrivateJSON(preferencesPath, p); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		code := start(strings.NewReader("y\nAUTHORIZE\ny\n"), &out, &errOut)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
		}
		if hits.Load() == 0 {
			t.Fatal("expected requests to loopback fixture")
		}
		matches, err := filepath.Glob(filepath.Join(profileDir, "reports", "*.json"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("missing JSON report: %v %v", matches, err)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		var parsed any
		if err = json.Unmarshal(data, &parsed); err != nil {
			t.Fatal(err)
		}
	})
}
func TestStartDoesNotStoreAuthenticationCookie(t *testing.T) {
	runInTempDir(t, func() {
		input := "https://app.example.test\nMy App\n/dashboard\nexpected text\n2\n1\n2\n1s\n5\n"
		var out, errOut bytes.Buffer
		if code := start(strings.NewReader(input), &out, &errOut); code != 2 {
			t.Fatalf("expected missing env failure, got %d", code)
		}
		data, err := os.ReadFile(scenarioPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "${DAMOCLINE_SESSION_COOKIE}") {
			t.Fatal("profile does not refer to environment variable")
		}
	})
}

func TestNormalizeApplicationOrigin(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare domain", "app.example.com", "https://app.example.com"},
		{"bare domain with slash", "app.example.com/", "https://app.example.com"},
		{"HTTPS without slash", "https://app.example.com", "https://app.example.com"},
		{"HTTPS with slash", "https://app.example.com/", "https://app.example.com"},
		{"surrounding whitespace", " https://app.example.com/  ", "https://app.example.com"},
		{"local loopback", "http://127.0.0.1:8080/", "http://127.0.0.1:8080"},
		{"localhost", "http://localhost:8080/", "http://localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeApplicationOrigin(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("normalizeApplicationOrigin(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestNormalizeApplicationOriginRejectsUnsafeInputs(t *testing.T) {
	bad := []string{
		"", "https://", "ftp://example.com", "http://app.example.com",
		"https://user:password@app.example.com", "https://app.example.com/dashboard",
		"https://app.example.com/?token=secret", "https://app.example.com/#fragment",
		"app.example.com/path/to/page", "https://app.example.com/ unexpected",
		"javascript:alert(1)",
	}
	for _, input := range bad {
		t.Run(input, func(t *testing.T) {
			if got, err := normalizeApplicationOrigin(input); err == nil {
				t.Fatalf("unexpectedly accepted %q as %q", input, got)
			}
		})
	}
}

func TestWizardPromptsAgainAfterInvalidOrigin(t *testing.T) {
	runInTempDir(t, func() {
		input := strings.Join([]string{
			"https://app.example.com/dashboard", // must not exit the wizard
			"app.example.com/",                  // this is accepted
			"Example smoke", "/dashboard", "", "1", "1", "2", "1s", "5", "DENIED",
		}, "\n") + "\n"
		var out, errOut bytes.Buffer
		if code := start(strings.NewReader(input), &out, &errOut); code != 0 {
			t.Fatalf("exit code = %d; output=%q; stderr=%q", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "Please try again") ||
			!strings.Contains(out.String(), "Using https://app.example.com") {
			t.Fatalf("wizard should retry and display canonical URL: %s", out.String())
		}
		s, err := config.Load(scenarioPath)
		if err != nil || s.BaseURL != "https://app.example.com" {
			t.Fatalf("bad saved origin: %+v, %v", s, err)
		}
	})
}
