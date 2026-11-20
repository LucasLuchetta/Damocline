package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
)

// G0-15: forced child termination at further client stages. The independent
// server ledger is the only surviving evidence; no final client report may
// appear. Each case waits for an observable server-side stage before killing.
func TestForcedTerminationStageMatrixReconcilesServerLedger(t *testing.T) {
	cases := []struct {
		name      string
		reference reference.Config
		scenario  config.Scenario
		// waitReceived is how many server receipts must happen before the kill.
		waitReceived int
		// waitCompleted additionally requires completed server work first.
		waitCompleted int64
		want          reference.Snapshot
	}{
		{
			// One request is being processed and one is queued at the server
			// behind its single slot when the generator is killed.
			name:      "kill with one request processing and one queued at server",
			reference: reference.Config{Delay: 2 * time.Second, MaxConcurrent: 1, MaxRequests: 4},
			scenario: config.Scenario{Users: 2, Duration: "10s", RampUp: "0s", Timeout: "8s", MaxRPS: 100,
				Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work"}}},
			waitReceived: 2,
			want:         reference.Snapshot{Received: 2, Cancelled: 2},
		},
		{
			// The first step completed at the server; the second step is held by
			// the client rate limiter (1 RPS) and never reaches the server.
			name:      "kill while second step waits for a client rate token",
			reference: reference.Config{Delay: 5 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 4},
			scenario: config.Scenario{Users: 1, Duration: "10s", RampUp: "0s", Timeout: "8s", MaxRPS: 1,
				Steps: []config.Step{{Name: "first", Method: "GET", Path: "/work"}, {Name: "second", Method: "GET", Path: "/work"}}},
			waitReceived:  1,
			waitCompleted: 1,
			want:          reference.Snapshot{Received: 1, Completed: 1, Success: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := reference.New(tc.reference)
			if err != nil {
				t.Fatal(err)
			}
			received := make(chan struct{}, 8)
			finished := make(chan struct{}, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- struct{}{}
				defer func() { finished <- struct{}{} }()
				fixture.ServeHTTP(w, r)
			}))
			defer server.Close()

			dir := t.TempDir()
			configPath := filepath.Join(dir, "scenario.json")
			reportPath := filepath.Join(dir, "run.json")
			s := tc.scenario
			s.Name, s.BaseURL, s.P95LimitMS = "forced stage fixture", server.URL, 1000
			// The server ledger counts only load requests; the network baseline is
			// covered by dedicated engine tests (ADR 0153).
			s.NetworkBaselineSkipReason = "server-ledger-fixture"
			configJSON, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, configJSON, 0o600); err != nil {
				t.Fatal(err)
			}
			argsJSON, err := json.Marshal([]string{"run", "--config", configPath, "--authorized", "--out", reportPath})
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestForcedInterruptionProcessHelper$")
			command.Env = append(os.Environ(),
				"DAMOCLINE_FORCED_INTERRUPTION_HELPER=1",
				"DAMOCLINE_FORCED_INTERRUPTION_ARGS="+base64.RawURLEncoding.EncodeToString(argsJSON),
			)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			kill := func() {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
			deadline := time.After(5 * time.Second)
			for range tc.waitReceived {
				select {
				case <-received:
				case <-deadline:
					kill()
					t.Fatalf("child did not reach the server stage: stdout=%s stderr=%s", stdout.String(), stderr.String())
				}
			}
			for fixture.Snapshot().Completed < tc.waitCompleted {
				select {
				case <-deadline:
					kill()
					t.Fatal("server work did not complete before the kill point")
				case <-time.After(5 * time.Millisecond):
				}
			}
			// The rate-token case must be killed inside the 1 s token wait.
			if err := command.Process.Kill(); err != nil {
				_ = command.Wait()
				t.Fatalf("failed to force-stop child generator: %v", err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("forcibly terminated generator exited successfully")
			}
			for range tc.waitReceived {
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("reference handler did not finish after the child was killed")
				}
			}
			ledger := fixture.Snapshot()
			if ledger.Received != tc.want.Received || ledger.Completed != tc.want.Completed || ledger.Success != tc.want.Success ||
				ledger.Failures != 0 || ledger.Cancelled != tc.want.Cancelled || ledger.Rejected != 0 ||
				ledger.Received != ledger.Completed+ledger.Cancelled+ledger.Rejected {
				t.Fatalf("server ledger = %+v; want %+v", ledger, tc.want)
			}
			if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
				t.Fatalf("forced termination must not leave a final run report, stat error=%v", err)
			}
		})
	}
}
