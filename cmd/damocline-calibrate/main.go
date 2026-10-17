// damocline-calibrate runs a bounded loopback reference check in two processes.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const stageDuration = 5 * time.Second
const soakDuration = 30 * time.Second

type assessment struct {
	SchemaVersion  int                `json:"schema_version"`
	Status         string             `json:"status"`
	At             string             `json:"at"`
	OS             string             `json:"os"`
	Arch           string             `json:"arch"`
	GoVersion      string             `json:"go_version"`
	LogicalCPUs    int                `json:"logical_cpus"`
	FixtureDelay   string             `json:"fixture_delay"`
	StageDuration  string             `json:"stage_duration"`
	ArrivalRates   []int              `json:"arrival_rates"`
	Repeats        int                `json:"repeats"`
	Extended       bool               `json:"extended"`
	Runs           []report.Result    `json:"runs"`
	SoakRun        report.Result      `json:"soak_run"`
	Server         reference.Snapshot `json:"server"`
	OverloadRun    report.Result      `json:"overload_run"`
	OverloadServer reference.Snapshot `json:"overload_server"`
	FailureRun     report.Result      `json:"failure_run"`
	FailureServer  reference.Snapshot `json:"failure_server"`
	Checks         []string           `json:"checks"`
	Limitations    []string           `json:"limitations"`
}

func main() {
	serve := flag.Bool("serve", false, "internal loopback fixture process")
	out := flag.String("out", ".damocline/calibration/reference.json", "local JSON result path")
	repeats := flag.Int("repeats", 1, "repeat the fixed three-stage matrix (1..3)")
	extended := flag.Bool("extended", false, "add bounded 15, 20, 50, and 100 arrivals/s loopback stages; allows up to two repeats")
	transportMatrix := flag.Bool("transport-matrix", false, "run bounded local HTTP and trusted/untrusted HTTPS transport checks")
	flag.Parse()
	if *serve {
		if err := runFixture(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	var err error
	if *transportMatrix {
		if *extended || *repeats != 1 {
			err = errors.New("--transport-matrix cannot be combined with --extended or non-default --repeats")
		} else {
			err = runTransportMatrix(*out)
		}
	} else {
		err = runMatrix(*out, *repeats, *extended)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type transportCheck struct {
	Name           string         `json:"name"`
	Outcome        string         `json:"outcome"`
	Requests       int            `json:"requests"`
	Success        int            `json:"success"`
	Failures       int            `json:"failures"`
	Reasons        map[string]int `json:"failure_reasons,omitempty"`
	ServerReceived int64          `json:"server_received"`
}

type transportAssessment struct {
	SchemaVersion int              `json:"schema_version"`
	Status        string           `json:"status"`
	At            string           `json:"at"`
	OS            string           `json:"os"`
	Arch          string           `json:"arch"`
	Checks        []transportCheck `json:"checks"`
	Limitations   []string         `json:"limitations"`
}

func runTransportMatrix(path string) error {
	var httpRequests atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpRequests.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 4<<10))
	}))
	defer httpServer.Close()

	var httpsRequests atomic.Int64
	httpsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpsRequests.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 4<<10))
	}))
	httpsServer.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer httpsServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result := transportAssessment{SchemaVersion: 1, Status: "CHECKS_FAILED", At: time.Now().UTC().Format(time.RFC3339), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Checks: []transportCheck{}, Limitations: []string{"Loopback transport results do not measure internet DNS, routing, proxies, or remote TLS termination.", "The trusted TLS stage uses only the local fixture certificate; it does not establish trust settings for any external target.", "The untrusted TLS stage verifies certificate rejection and transport-error accounting, not application capacity."}}

	plain := runTransportStage(ctx, "http", httpServer.URL, nil)
	plain.ServerReceived = httpRequests.Load()
	result.Checks = append(result.Checks, plain)
	trustedPool := httpsServer.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	trustedBefore := httpsRequests.Load()
	trusted := runTransportStage(ctx, "https_trusted_fixture", httpsServer.URL, trustedPool)
	trusted.ServerReceived = httpsRequests.Load() - trustedBefore
	result.Checks = append(result.Checks, trusted)
	untrustedBefore := httpsRequests.Load()
	untrusted := runTransportStage(ctx, "https_untrusted_fixture", httpsServer.URL, nil)
	untrusted.ServerReceived = httpsRequests.Load() - untrustedBefore
	result.Checks = append(result.Checks, untrusted)
	if transportChecksPass(result.Checks) {
		result.Status = "TRANSPORT_MATRIX_PASSED"
	}
	if err := report.WriteAnyJSON(path, result); err != nil {
		return err
	}
	fmt.Printf("%s: HTTP and trusted HTTPS each delivered 10 requests; untrusted HTTPS was rejected before reaching the fixture. Report: %s\n", result.Status, path)
	if result.Status != "TRANSPORT_MATRIX_PASSED" {
		return errors.New("transport matrix checks failed; inspect the local report")
	}
	return nil
}

func runTransportStage(ctx context.Context, name, target string, roots *x509.CertPool) transportCheck {
	scenario := config.Scenario{Name: "Local transport " + name, BaseURL: target, Users: 2, Duration: "5s", MaxRPS: 2,
		Mode: "arrival", ArrivalRate: 2, Timeout: "1s", P95LimitMS: 1000, MaxErrorRate: 0,
		Steps: []config.Step{{Name: "transport", Method: "GET", Path: "/", ExpectedStatus: []int{200}}}}
	validated, err := config.Parse(scenario, config.Options{Authorized: true, TLSRootCAs: roots})
	if err != nil {
		return transportCheck{Name: name, Outcome: "CONFIG_ERROR"}
	}
	run := engine.Run(ctx, validated)
	return transportCheck{Name: name, Outcome: run.Outcome, Requests: run.Metrics.Total, Success: run.Metrics.Success, Failures: run.Metrics.Failures, Reasons: run.Metrics.Reasons}
}

func transportChecksPass(checks []transportCheck) bool {
	if len(checks) != 3 {
		return false
	}
	for _, check := range checks[:2] {
		if check.Outcome != "PASS" || check.Requests != 10 || check.Success != 10 || check.Failures != 0 || check.ServerReceived != 10 {
			return false
		}
	}
	untrusted := checks[2]
	return untrusted.Outcome == "FAIL" && untrusted.Requests == 10 && untrusted.Success == 0 && untrusted.Failures == 10 && untrusted.Reasons["transport"] == 10 && untrusted.ServerReceived == 0
}

func runFixture() error {
	h, err := reference.New(reference.Config{Delay: 20 * time.Millisecond, ResponseBytes: 4 << 10, MaxConcurrent: 10, MaxRequests: 10000})
	if err != nil {
		return err
	}
	overload, err := reference.New(reference.Config{Delay: 200 * time.Millisecond, MaxConcurrent: 1, MaxRequests: 100})
	if err != nil {
		return err
	}
	failures, err := reference.New(reference.Config{Delay: 20 * time.Millisecond, FailureFirst: 2, MaxConcurrent: 10, MaxRequests: 20})
	if err != nil {
		return err
	}
	var current atomic.Pointer[reference.Handler]
	current.Store(h)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().ServeHTTP(w, r) }), ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	fmt.Println("READY " + listener.Addr().String())
	input := make(chan string, 2)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			input <- scanner.Text()
		}
	}()
	select {
	case command := <-input:
		if command == "overload" {
			current.Store(overload)
			fmt.Println("READY_OVERLOAD")
			select {
			case command := <-input:
				if command == "failures" {
					current.Store(failures)
					fmt.Println("READY_FAILURES")
					select {
					case <-input:
					case <-time.After(15 * time.Second):
					}
				}
			case <-time.After(15 * time.Second):
			}
		}
	case <-time.After(140 * time.Second):
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return err
	}
	if err := <-finished; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	data, err := json.Marshal(struct {
		Baseline reference.Snapshot `json:"baseline"`
		Overload reference.Snapshot `json:"overload"`
		Failures reference.Snapshot `json:"failures"`
	}{h.Snapshot(), overload.Snapshot(), failures.Snapshot()})
	if err != nil {
		return err
	}
	fmt.Println("SNAPSHOT " + string(data))
	return nil
}

func run(path string, repeats int) error {
	return runMatrix(path, repeats, false)
}

func runMatrix(path string, repeats int, extended bool) error {
	if repeats < 1 || repeats > 3 {
		return errors.New("repeats must be between 1 and 3")
	}
	if extended && repeats > 2 {
		return errors.New("extended matrix permits at most two repeats within the hard deadline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, "--serve")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 2)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	var ready string
	select {
	case ready = <-lines:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !strings.HasPrefix(ready, "READY 127.0.0.1:") {
		return fmt.Errorf("reference fixture did not start: %q: %s", ready, stderr.String())
	}
	address := strings.TrimPrefix(ready, "READY ")
	if _, port, err := net.SplitHostPort(address); err != nil || port == "" {
		return fmt.Errorf("invalid local fixture address: %q", address)
	}
	var rates []int
	for range repeats {
		rates = append(rates, 2, 5, 10)
		if extended {
			rates = append(rates, 15, 20, 50, 100)
		}
	}
	result := assessment{SchemaVersion: 1, Status: "CHECKS_FAILED", At: time.Now().UTC().Format(time.RFC3339), OS: runtime.GOOS, Arch: runtime.GOARCH,
		GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), FixtureDelay: "20ms", StageDuration: stageDuration.String(), ArrivalRates: rates, Repeats: repeats, Extended: extended,
		Limitations: []string{"Local loopback checks are not generator calibration across operating systems or load ranges.", "Generator and target are separate processes but share one host; process pressure can still interfere.", "No application, database, or customer infrastructure capacity is inferred."}}
	for _, rate := range rates {
		scenario := config.Scenario{Name: "Local reference " + strconv.Itoa(rate) + " arrivals/s", BaseURL: "http://" + address,
			Users: 10, Duration: stageDuration.String(), MaxRPS: max(50, rate), Mode: "arrival", ArrivalRate: rate,
			Timeout: "1s", P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}}}
		v, err := config.Parse(scenario, config.Options{Authorized: true})
		if err != nil {
			return err
		}
		result.Runs = append(result.Runs, engine.Run(ctx, v))
	}
	soakScenario := config.Scenario{Name: "Local reference sustained 10 arrivals/s", BaseURL: "http://" + address,
		Users: 10, Duration: soakDuration.String(), MaxRPS: 50, Mode: "arrival", ArrivalRate: 10,
		Timeout: "1s", P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}}}
	soakValidated, err := config.Parse(soakScenario, config.Options{Authorized: true})
	if err != nil {
		return err
	}
	result.SoakRun = engine.Run(ctx, soakValidated)
	if _, err := stdin.Write([]byte("overload\n")); err != nil {
		return err
	}
	select {
	case line := <-lines:
		if line != "READY_OVERLOAD" {
			return fmt.Errorf("reference fixture did not enter overload mode: %q", line)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	overloadScenario := config.Scenario{Name: "Local reference underdelivery", BaseURL: "http://" + address,
		Users: 1, Duration: stageDuration.String(), MaxRPS: 50, Mode: "arrival", ArrivalRate: 10,
		Timeout: "1s", P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}}}
	overloadValidated, err := config.Parse(overloadScenario, config.Options{Authorized: true})
	if err != nil {
		return err
	}
	result.OverloadRun = engine.Run(ctx, overloadValidated)
	if _, err := stdin.Write([]byte("failures\n")); err != nil {
		return err
	}
	select {
	case line := <-lines:
		if line != "READY_FAILURES" {
			return fmt.Errorf("reference fixture did not enter failure mode: %q", line)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	failureScenario := config.Scenario{Name: "Local reference errors and recovery", BaseURL: "http://" + address,
		Users: 2, Duration: stageDuration.String(), MaxRPS: 20, Mode: "arrival", ArrivalRate: 2,
		Timeout: "1s", P95LimitMS: 500, MaxErrorRate: 0, Steps: []config.Step{{Name: "work", Method: "GET", Path: "/work", ExpectedStatus: []int{200}}}}
	failureValidated, err := config.Parse(failureScenario, config.Options{Authorized: true})
	if err != nil {
		return err
	}
	result.FailureRun = engine.Run(ctx, failureValidated)
	if _, err := stdin.Write([]byte("stop\n")); err != nil {
		return err
	}
	var snapshotLine string
	select {
	case snapshotLine = <-lines:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !strings.HasPrefix(snapshotLine, "SNAPSHOT ") {
		return fmt.Errorf("reference fixture did not return counters: %q: %s", snapshotLine, stderr.String())
	}
	var snapshots struct {
		Baseline reference.Snapshot `json:"baseline"`
		Overload reference.Snapshot `json:"overload"`
		Failures reference.Snapshot `json:"failures"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(snapshotLine, "SNAPSHOT ")), &snapshots); err != nil {
		return err
	}
	result.Server, result.OverloadServer, result.FailureServer = snapshots.Baseline, snapshots.Overload, snapshots.Failures
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("reference fixture failed: %w: %s", err, stderr.String())
	}
	result.Checks = check(result.Runs, result.Server, rates)
	result.Checks = append(result.Checks, checkSoak(result.SoakRun)...)
	result.Checks = append(result.Checks, checkOverload(result.OverloadRun, result.OverloadServer)...)
	result.Checks = append(result.Checks, checkFailures(result.FailureRun, result.FailureServer)...)
	if len(result.Checks) == 0 {
		result.Status = "REFERENCE_SMOKE_PASSED"
	}
	if err := report.WriteAnyJSON(path, result); err != nil {
		return err
	}
	fmt.Printf("%s: %d healthy stages plus %s soak, fixture %d/%d completed; slow-target drops %d/%d; recovery %d/%d success; %d discrepancies. Report: %s\n",
		result.Status, len(result.Runs), soakDuration, result.Server.Completed, result.Server.Received,
		result.OverloadRun.ArrivalsDropped, result.OverloadRun.ArrivalsScheduled,
		result.FailureRun.Metrics.Success, result.FailureRun.Metrics.Total, len(result.Checks), path)
	if len(result.Checks) > 0 {
		return fmt.Errorf("reference check found %d discrepancies; inspect the local report", len(result.Checks))
	}
	return nil
}

func checkSoak(run report.Result) []string {
	planned := int64(10 * (soakDuration / time.Second))
	if run.ArrivalsScheduled != planned || run.ArrivalsDropped != 0 || run.IterationsStarted != planned ||
		run.IterationsCompleted != planned || run.Metrics.Total != int(planned) || run.Metrics.Success != int(planned) ||
		run.Journeys.Total != int(planned) || run.Journeys.Success != int(planned) || run.Journeys.Cancelled != 0 ||
		run.Metrics.Failures != 0 || run.Outcome != "PASS" || run.Generator == nil ||
		run.Generator.Samples < 100 || run.Generator.ProcessSamples < 100 || run.Generator.PeakProcessRSSBytes == 0 ||
		!run.Generator.GoCPUValid || !run.Generator.ProcessCPUValid ||
		run.Generator.GoCPUUtilizationRatio >= 0.9 || run.Generator.PeakProcessCPUUtilizationRatio >= 0.9 ||
		run.Generator.PeakHeapAllocBytes > 256<<20 || run.Generator.PeakProcessRSSBytes > 512<<20 {
		return []string{"sustained reference workload did not reconcile or retain generator health samples"}
	}
	return nil
}

func checkFailures(run report.Result, server reference.Snapshot) []string {
	if run.ArrivalsScheduled != 10 || run.ArrivalsDropped != 0 || run.IterationsStarted != 10 ||
		run.IterationsCompleted != 10 || run.Metrics.Total != 10 ||
		run.Metrics.Failures != 2 || run.Metrics.Success != 8 || run.Outcome != "FAIL" ||
		server.Received != 10 || server.Completed != 10 || server.Failures != 2 || server.Success != 8 ||
		server.Cancelled != 0 || server.Rejected != 0 {
		return []string{"controlled errors and recovery did not reconcile"}
	}
	return nil
}

func checkOverload(run report.Result, server reference.Snapshot) []string {
	if run.ArrivalsScheduled != 50 || run.ArrivalsDropped == 0 || run.ArrivalsDroppedBusy == 0 ||
		run.IterationsStarted+run.ArrivalsDropped != run.ArrivalsScheduled ||
		run.Outcome == "PASS" || server.Received != run.IterationsStarted ||
		server.Completed != int64(run.Metrics.Total) || server.Success != server.Completed ||
		server.Cancelled != int64(run.Metrics.Cancelled) || server.Failures != 0 || server.Rejected != 0 {
		return []string{"controlled overload did not expose and reconcile dropped starts"}
	}
	return nil
}

func check(runs []report.Result, server reference.Snapshot, rates []int) []string {
	if len(runs) != len(rates) {
		return []string{"reference stage count differs from the configured matrix"}
	}
	var issues []string
	var expected, started, completed int64
	for i, run := range runs {
		planned := int64(rates[i]) * int64(stageDuration/time.Second)
		expected += planned
		started += run.IterationsStarted
		completed += int64(run.Metrics.Total)
		if run.ArrivalsScheduled != planned || run.ArrivalsDropped != 0 ||
			run.IterationsStarted != planned || run.IterationsCompleted != planned || run.Metrics.Total != int(planned) ||
			run.Metrics.Success != int(planned) || run.Metrics.Failures != 0 ||
			run.Journeys.Total != int(planned) || run.Journeys.Success != int(planned) ||
			run.Outcome != "PASS" || run.Metrics.P50MS < 15 || run.Metrics.P50MS > 150 ||
			run.Metrics.P95MS < 15 || run.Metrics.P95MS > 150 ||
			run.Journeys.P50MS < 15 || run.Journeys.P50MS > 150 ||
			run.Journeys.P95MS < 15 || run.Journeys.P95MS > 150 ||
			run.Generator == nil || run.Generator.Samples < 2 || !run.Generator.GoCPUValid ||
			run.Generator.ProcessSamples < 2 || !run.Generator.ProcessCPUValid || run.Generator.PeakProcessRSSBytes == 0 ||
			run.Generator.GoCPUUtilizationRatio >= 0.9 || run.Generator.PeakProcessCPUUtilizationRatio >= 0.9 ||
			run.Generator.PeakHeapAllocBytes > 256<<20 || run.Generator.PeakProcessRSSBytes > 512<<20 ||
			run.SchedulerLagP95MS > 500/float64(rates[i]) || run.SchedulerLagMaxMS >= 1000/float64(rates[i]) {
			issues = append(issues, fmt.Sprintf("stage %d did not reconcile planned, started, completed, and timed work", rates[i]))
		}
	}
	serverExpected := expected + int64(10*(soakDuration/time.Second))
	if server.Received != serverExpected || server.Completed != serverExpected || server.Success != serverExpected ||
		server.Failures != 0 || server.Rejected != 0 || server.Cancelled != 0 || started != expected || completed != expected {
		issues = append(issues, "fixture and generator counters differ")
	}
	return issues
}
