package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const crosscheckRun = `{"schema_version":50,"name":"synthetic","started_at":"2026-01-02T10:00:00Z","elapsed_seconds":10,"observation_seconds":10,"outcome":"PASS","error_rate":0,
"config":{"users":2,"mode":"closed","max_error_rate":0.01,"p95_limit_ms":500,"duration":"10s","request_timeout":"5s","max_rps":10,"separate_sessions":false,"min_samples_per_step":1},
"metrics":{"total":3,"success":3,"failures":0,"cancelled":0,"p50_ms":5,"mean_ms":5,"p95_ms":8,"p99_ms":8},
"journeys":{"total":3,"success":3,"failures":0,"cancelled":0,"p50_ms":5,"mean_ms":5,"p95_ms":8,"p99_ms":8},
"steps":[{"name":"home","method":"GET","path":"/","metrics":{"total":3,"success":3,"failures":0,"cancelled":0,"p50_ms":5,"mean_ms":5,"p95_ms":8,"p99_ms":8,"http_diagnostics":{"status_1xx":0,"status_2xx":3,"status_3xx":0,"status_4xx":0,"status_5xx":0,"redirect_responses":0,"rate_limited_responses":0,"assertion_failures":0,"network_timeouts":0,"dns_failures":0,"connection_failures":0,"tls_failures":0,"transport_failures":0,"unexpected_response_sizes":0}},"http_trace":{}}],
"iterations_started":3,"iterations_completed":3,"arrivals_scheduled":0,"arrivals_dropped":0,"note":""}`

func TestCrosscheckAndCompareExternalCommands(t *testing.T) {
	dir := t.TempDir()
	runPath := filepath.Join(dir, "run.json")
	logPath := filepath.Join(dir, "access.log")
	k6Path := filepath.Join(dir, "k6.json")
	os.WriteFile(runPath, []byte(crosscheckRun), 0o600)
	line := `127.0.0.1 - - [02/Jan/2026:10:00:01 +0000] "GET / HTTP/1.1" 200 2 "-" "Damocline" 0.004` + "\n"
	os.WriteFile(logPath, []byte(strings.Repeat(line, 3)), 0o600)
	os.WriteFile(k6Path, []byte(`{"metrics":{"http_reqs":{"count":3,"rate":0.3},"http_req_failed":{"value":0},"http_req_duration":{"p(95)":7.5},"vus_max":{"max":2}}}`), 0o600)

	var out, errb bytes.Buffer
	if code := crosscheckCmd([]string{"--run", runPath, "--access-log", logPath, "--format", "nginx", "--out", filepath.Join(dir, "cc.json")}, &out, &errb); code != 0 {
		t.Fatalf("crosscheck exit %d: %s\n%s", code, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), "Overall: **MATCH**") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := compareExternalCmd([]string{"--damocline", runPath, "--k6-summary", k6Path}, &out, &errb); code != 0 {
		t.Fatalf("compare-external exit %d: %s\n%s", code, errb.String(), out.String())
	}
	if code := crosscheckCmd([]string{"--run", runPath}, &out, &errb); code != 2 {
		t.Fatalf("missing flags must exit 2, got %d", code)
	}
}
