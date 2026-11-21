package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestDataRowsFeedPathAndHeaderPerVirtualUser(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if seen[r.URL.Path] == nil {
			seen[r.URL.Path] = map[string]bool{}
		}
		seen[r.URL.Path][r.Header.Get("X-Tenant")] = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "accounts.csv")
	if err := os.WriteFile(csvPath, []byte("account,tenant\nacct-one,t1\nacct two,t2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := config.Scenario{Name: "data", BaseURL: srv.URL, Users: 2, Duration: "1s", MaxRPS: 50, P95LimitMS: 2000, Timeout: "300ms",
		Data:  &config.DataSource{File: csvPath, Mode: config.DataModeUniquePerVU},
		Steps: []config.Step{{Name: "account", Path: "/accounts/${data.account}", Headers: map[string]string{"X-Tenant": "${data.tenant}"}}}}
	v, err := config.Parse(raw, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 300 * time.Millisecond
	result := Run(context.Background(), v)
	if result.Metrics.Success < 2 || result.Metrics.Failures != 0 {
		t.Fatalf("unexpected result: %+v", result.Metrics)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || !seen["/accounts/acct-one"]["t1"] || !seen["/accounts/acct two"]["t2"] || len(seen["/accounts/acct-one"]) != 1 {
		t.Fatalf("each virtual user must keep its own row: %v", seen)
	}
	for _, step := range result.Steps {
		if strings.Contains(step.Path, "acct") {
			t.Fatalf("report must keep the template, not row values: %q", step.Path)
		}
	}
}
