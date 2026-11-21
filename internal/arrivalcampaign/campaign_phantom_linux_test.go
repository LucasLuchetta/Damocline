//go:build linux && integration

package arrivalcampaign

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const phantomWindow = 5 * time.Second

func TestPhantomCapacity(t *testing.T) {
	const childResult = "DAMOCLINE_PHANTOM_RESULT"
	if output := os.Getenv(childResult); output != "" {
		runPhantomGenerator(t, output)
		return
	}

	cases := []struct {
		name  string
		rate  int
		pause bool
		want  string
	}{
		{name: "below-service-limit", rate: 15, want: "PASS_OBSERVED"},
		{name: "above-service-limit", rate: 30, want: "FAIL_OBSERVED"},
		{name: "generator-denied-cpu", rate: 30, pause: true, want: "INVALID_DELIVERY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var received, completed, rejected, cancelled atomic.Int64
			firstPair := make(chan struct{})
			slots := make(chan struct{}, 2)
			admitted := make(chan struct{}, 4) // Two service slots and two waiting requests.
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if received.Add(1) == 2 {
					close(firstPair)
				}
				select {
				case admitted <- struct{}{}:
					defer func() { <-admitted }()
				default:
					rejected.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-r.Context().Done():
					cancelled.Add(1)
					return
				}
				select {
				case <-time.After(100 * time.Millisecond):
					completed.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case <-r.Context().Done():
					cancelled.Add(1)
				}
			}))
			defer target.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			output := filepath.Join(t.TempDir(), "run.json")
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPhantomCapacity$")
			cmd.Env = append(os.Environ(), childResult+"="+output,
				"DAMOCLINE_PHANTOM_TARGET="+target.URL, "DAMOCLINE_PHANTOM_RATE="+strconv.Itoa(tc.rate))
			log := filepath.Join(filepath.Dir(output), "child.log")
			logFile, err := os.Create(log)
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			cmd.Stdout, cmd.Stderr = logFile, logFile
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}()
			if tc.pause {
				select {
				case <-firstPair:
				case <-ctx.Done():
					t.Fatal("generator did not reach the intervention trigger")
				}
				// Freeze only the generator process. The target continues serving on the host.
				if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
					t.Fatal(err)
				}
				time.Sleep(phantomWindow - 100*time.Millisecond)
				if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Wait(); err != nil {
				data, _ := os.ReadFile(log)
				t.Fatalf("generator process: %v\n%s", err, data)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var run report.Result
			if err := json.Unmarshal(data, &run); err != nil {
				t.Fatal(err)
			}

			wantStarts := int64(tc.rate) * int64(phantomWindow/time.Second)
			got, successes, rejections := received.Load(), completed.Load(), rejected.Load()
			state, reason := ClassifyEvidence(run)
			t.Logf("scheduled=%d received=%d completed=%d rejected=%d late=%d native_requests=%d native_successes=%d outcome=%s phase=%s (%s)",
				run.ArrivalsScheduled, got, successes, rejections, run.ArrivalsDroppedLate, run.Metrics.Total, run.Metrics.Success, run.Outcome, state, reason)
			if got != successes+rejections || cancelled.Load() != 0 {
				t.Fatal("target ledger did not reconcile after drain")
			}
			if run.ArrivalsScheduled != wantStarts || int64(run.Metrics.Total) != got || int64(run.Metrics.Success) != successes {
				t.Fatal("scheduled count or native request ledger disagrees with the independent reference")
			}
			if state != tc.want {
				t.Fatalf("phase classification = %s, want %s", state, tc.want)
			}
			if tc.pause {
				if got < 2 || got > wantStarts/2 || rejections != 0 || run.ArrivalsDroppedLate == 0 || run.Outcome != "INCONCLUSIVE" {
					t.Fatal("intervention must cause material underdelivery with a healthy target and no passing verdict")
				}
				return
			}
			if got != wantStarts || run.ArrivalsDropped != 0 {
				t.Fatal("control must deliver every planned start")
			}
			if tc.rate > 20 {
				// Allow completion of up to four outstanding requests during drain.
				if successes < 75 || successes > 104 || rejections == 0 {
					t.Fatal("above-limit control must sustain service and overflow the bounded target queue")
				}
			} else if rejections != 0 {
				t.Fatal("below-limit control must complete every request")
			}
		})
	}
}

func runPhantomGenerator(t *testing.T, output string) {
	t.Helper()
	rate, err := strconv.Atoi(os.Getenv("DAMOCLINE_PHANTOM_RATE"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := config.Parse(config.Scenario{
		Name: "phantom capacity reference", BaseURL: os.Getenv("DAMOCLINE_PHANTOM_TARGET"),
		Mode: "arrival", Users: 20, ArrivalRate: rate, MaxRPS: 50, Duration: phantomWindow.String(),
		Timeout: "6s", P95LimitMS: 500, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Path: "/work", ExpectedStatus: []int{http.StatusNoContent}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if base.Base.Hostname() != "127.0.0.1" || (rate != 15 && rate != 30) {
		t.Fatal("the generator requires a loopback target and one of the predeclared rates")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := json.Marshal(engine.Run(ctx, base))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
}
