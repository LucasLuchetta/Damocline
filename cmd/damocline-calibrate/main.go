// damocline-calibrate runs a bounded loopback reference check in two processes.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/protocolcheck"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const stageDuration = 5 * time.Second
const soakDuration = 30 * time.Second

type assessment struct {
	SchemaVersion  int                     `json:"schema_version"`
	Preflight      preflightRecord         `json:"preflight"`
	Status         string                  `json:"status"`
	At             string                  `json:"at"`
	OS             string                  `json:"os"`
	Arch           string                  `json:"arch"`
	GoVersion      string                  `json:"go_version"`
	LogicalCPUs    int                     `json:"logical_cpus"`
	FixtureDelay   string                  `json:"fixture_delay"`
	StageDuration  string                  `json:"stage_duration"`
	ArrivalRates   []int                   `json:"arrival_rates"`
	Repeats        int                     `json:"repeats"`
	Extended       bool                    `json:"extended"`
	Runs           []report.Result         `json:"runs"`
	SoakRun        report.Result           `json:"soak_run"`
	Server         reference.Snapshot      `json:"server"`
	OverloadRun    report.Result           `json:"overload_run"`
	OverloadServer reference.Snapshot      `json:"overload_server"`
	FailureRun     report.Result           `json:"failure_run"`
	FailureServer  reference.Snapshot      `json:"failure_server"`
	SchedulerLag   schedulerLagCalibration `json:"scheduler_lag_calibration"`
	Checks         []string                `json:"checks"`
	Limitations    []string                `json:"limitations"`
}

func main() {
	serve := flag.Bool("serve", false, "internal loopback fixture process")
	out := flag.String("out", ".damocline/calibration/reference.json", "local JSON result path")
	repeats := flag.Int("repeats", 1, "repeat the fixed three-stage matrix (1..3)")
	extended := flag.Bool("extended", false, "add bounded 15, 20, 50, and 100 arrivals/s loopback stages; allows up to two repeats")
	transportMatrix := flag.Bool("transport-matrix", false, "run a bounded, loopback-only HTTP transport fixture matrix (not a capacity test)")
	checkScenario := flag.String("check-scenario", "", "offline-only: compare a scenario against a saved local reference artifact")
	calibration := flag.String("calibration", ".damocline/calibration/reference.json", "local reference JSON to use with --check-scenario")
	flag.Parse()
	if *transportMatrix {
		if *serve || *checkScenario != "" {
			fmt.Fprintln(os.Stderr, "--transport-matrix cannot be combined with --serve or --check-scenario")
			os.Exit(1)
		}
		result := protocolcheck.Run(context.Background())
		if err := protocolcheck.Validate(result); err != nil {
			fmt.Fprintln(os.Stderr, "transport matrix is incomplete or failed:", err)
			os.Exit(1)
		}
		if err := report.WriteAnyJSON(*out, result); err != nil {
			fmt.Fprintln(os.Stderr, "cannot write transport evidence:", err)
			os.Exit(1)
		}
		fmt.Println(protocolcheck.Summary(result))
		if result.Status != "TRANSPORT_FIXTURES_PASSED" {
			os.Exit(1)
		}
		return
	}
	if *checkScenario != "" {
		if *serve {
			fmt.Fprintln(os.Stderr, "--serve cannot be combined with offline --check-scenario")
			os.Exit(1)
		}
		if err := runOfflineCoverage(*calibration, *checkScenario, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, errInconclusive) {
				os.Exit(2)
			}
			os.Exit(1)
		}
		return
	}
	if *serve {
		if err := runFixture(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := runMatrix(*out, *repeats, *extended); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	preflightStarted := time.Now().UTC()
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
	result.SchedulerLag = calibrateSchedulerLag()
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
	if result.SchedulerLag.Status != "PASSED" {
		for i, observation := range result.SchedulerLag.Observations {
			if observation.Outcome != "PASSED" {
				result.Checks = append(result.Checks, fmt.Sprintf("scheduler-lag observation %d: expected max %.3fms, observed %.3fms, tolerance ±%.3fms; planned=%d admitted=%d dropped=%d", i+1, observation.ExpectedMaxLagMS, observation.ObservedMaxLagMS, result.SchedulerLag.ToleranceMS, observation.Planned, observation.Admitted, observation.Dropped))
			}
		}
	}
	if len(result.Checks) == 0 {
		result.Status = "REFERENCE_SMOKE_PASSED"
	}
	observedStages := make([]preflightStage, 0, len(result.Runs))
	for i, stage := range result.Runs {
		observedStages = append(observedStages, preflightStage{
			ArrivalRate: rates[i], Scheduled: stage.ArrivalsScheduled,
			Started: stage.IterationsStarted, Completed: stage.IterationsCompleted,
			Dropped: stage.ArrivalsDropped, Outcome: stage.Outcome,
		})
	}
	result.Preflight, err = newPreflightRecord(
		preflightStarted, time.Now().UTC(),
		preflightGenerator{
			OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(),
			GOMAXPROCS: runtime.GOMAXPROCS(0), GoVersion: runtime.Version(),
			RunnerVersion: sourceRunnerVersion(),
		},
		preflightFixture{
			NetworkMode:   "loopback_same_host_separate_process",
			WorkloadModel: "open_loop_journey_arrivals",
			Method:        "GET", HealthyResponseBytes: 4 << 10,
			HealthyDelay: "20ms", StageDuration: stageDuration.String(),
			SoakDuration: soakDuration.String(), StageUsers: 10,
			MaximumRPS: 100, Repeats: repeats, Extended: extended,
			HealthyRates: append([]int(nil), rates...),
		},
		observedStages, result.Status,
	)
	if err != nil {
		return fmt.Errorf("construct preflight evidence: %w", err)
	}
	if err := report.WriteAnyJSON(path, result); err != nil {
		return err
	}
	fmt.Printf("%s: %d healthy stages plus %s soak, fixture %d/%d completed; scheduler lag %s across %d injected observations (tolerance ±%.0f ms); slow-target drops %d/%d; recovery %d/%d success; %d discrepancies. Report: %s\n",
		result.Status, len(result.Runs), soakDuration, result.Server.Completed, result.Server.Received,
		result.SchedulerLag.Status, len(result.SchedulerLag.Observations), result.SchedulerLag.ToleranceMS,
		result.OverloadRun.ArrivalsDropped, result.OverloadRun.ArrivalsScheduled,
		result.FailureRun.Metrics.Success, result.FailureRun.Metrics.Total, len(result.Checks), path)
	if len(result.Checks) > 0 {
		return fmt.Errorf("reference check found %d discrepancies: %s", len(result.Checks), strings.Join(result.Checks, "; "))
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
		run.Generator.GoCPUUtilizationRatio >= 0.9 || run.Generator.MeanProcessCPUUtilizationRatio >= 0.9 ||
		run.Generator.PeakHeapAllocBytes > 256<<20 || run.Generator.PeakProcessRSSBytes > 512<<20 {
		return []string{fmt.Sprintf("soak reconciliation failed: planned=%d started=%d completed_iterations=%d requests=%d/%d succeeded failures=%d dropped=%d outcome=%s generator_samples=%d process_samples=%d process_rss=%d go_cpu_valid=%t process_cpu_valid=%t go_cpu=%.3f process_cpu_mean=%.3f process_cpu_peak=%.3f heap=%d", planned, run.IterationsStarted, run.IterationsCompleted, run.Metrics.Success, run.Metrics.Total, run.Metrics.Failures, run.ArrivalsDropped, run.Outcome, generatorSamples(run), generatorProcessSamples(run), generatorRSS(run), generatorGoCPUValid(run), generatorProcessCPUValid(run), generatorGoCPU(run), generatorProcessCPU(run), generatorPeakProcessCPU(run), generatorHeap(run))}
	}
	return nil
}

func checkFailures(run report.Result, server reference.Snapshot) []string {
	if run.ArrivalsScheduled != 10 || run.ArrivalsDropped != 0 || run.IterationsStarted != 10 ||
		run.IterationsCompleted != 10 || run.Metrics.Total != 10 ||
		run.Metrics.Failures != 2 || run.Metrics.Success != 8 || run.Outcome != "FAIL" ||
		server.Received != 10 || server.Completed != 10 || server.Failures != 2 || server.Success != 8 ||
		server.Cancelled != 0 || server.Rejected != 0 {
		return []string{fmt.Sprintf("failure/recovery ledger mismatch: expected scheduled/started/completed=10/10/10, requests=8 success/2 failures, server received/completed=10/10; actual scheduled/started/completed=%d/%d/%d requests=%d success/%d failures server received/completed=%d/%d success=%d failures=%d cancelled=%d rejected=%d outcome=%s", run.ArrivalsScheduled, run.IterationsStarted, run.IterationsCompleted, run.Metrics.Success, run.Metrics.Failures, server.Received, server.Completed, server.Success, server.Failures, server.Cancelled, server.Rejected, run.Outcome)}
	}
	return nil
}

func checkOverload(run report.Result, server reference.Snapshot) []string {
	if run.ArrivalsScheduled != 50 || run.ArrivalsDropped == 0 || run.ArrivalsDroppedBusy == 0 ||
		run.IterationsStarted+run.ArrivalsDropped != run.ArrivalsScheduled ||
		run.Outcome == "PASS" || server.Received != run.IterationsStarted ||
		server.Completed != int64(run.Metrics.Total) || server.Success != server.Completed ||
		server.Cancelled != int64(run.Metrics.Cancelled) || server.Failures != 0 || server.Rejected != 0 {
		return []string{fmt.Sprintf("overload ledger mismatch: expected scheduled=50, drops>0 including busy workers, server received=started and completed=requests; actual scheduled=%d started=%d dropped=%d late=%d busy=%d requests=%d cancelled=%d server received=%d completed=%d success=%d failures=%d cancelled=%d rejected=%d outcome=%s", run.ArrivalsScheduled, run.IterationsStarted, run.ArrivalsDropped, run.ArrivalsDroppedLate, run.ArrivalsDroppedBusy, run.Metrics.Total, run.Metrics.Cancelled, server.Received, server.Completed, server.Success, server.Failures, server.Cancelled, server.Rejected, run.Outcome)}
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
		issues = append(issues, checkStage(rates[i], planned, run)...)
	}
	serverExpected := expected + int64(10*(soakDuration/time.Second))
	if server.Received != serverExpected || server.Completed != serverExpected || server.Success != serverExpected ||
		server.Failures != 0 || server.Rejected != 0 || server.Cancelled != 0 || started != expected || completed != expected {
		issues = append(issues, fmt.Sprintf("aggregate fixture/generator ledger mismatch: expected_stage_requests=%d expected_including_soak=%d started=%d completed=%d; server received=%d completed=%d success=%d failures=%d cancelled=%d rejected=%d", expected, serverExpected, started, completed, server.Received, server.Completed, server.Success, server.Failures, server.Cancelled, server.Rejected))
	}
	return issues
}

func checkStage(rate int, planned int64, run report.Result) []string {
	var issues []string
	if run.ArrivalsScheduled != planned || run.ArrivalsDropped != 0 || run.IterationsStarted != planned || run.IterationsCompleted != planned {
		issues = append(issues, fmt.Sprintf("stage %d arrivals: expected scheduled/started/completed/dropped=%d/%d/%d/0, got %d/%d/%d/%d (late=%d busy=%d)", rate, planned, planned, planned, run.ArrivalsScheduled, run.IterationsStarted, run.IterationsCompleted, run.ArrivalsDropped, run.ArrivalsDroppedLate, run.ArrivalsDroppedBusy))
	}
	if run.Metrics.Total != int(planned) || run.Metrics.Success != int(planned) || run.Metrics.Failures != 0 {
		issues = append(issues, fmt.Sprintf("stage %d request outcomes: expected total/success/failures=%d/%d/0, got %d/%d/%d", rate, planned, planned, run.Metrics.Total, run.Metrics.Success, run.Metrics.Failures))
	}
	if run.Journeys.Total != int(planned) || run.Journeys.Success != int(planned) || run.Journeys.Failures != 0 || run.Journeys.Cancelled != 0 {
		issues = append(issues, fmt.Sprintf("stage %d journey outcomes: expected total/success/failures/cancelled=%d/%d/0/0, got %d/%d/%d/%d", rate, planned, planned, run.Journeys.Total, run.Journeys.Success, run.Journeys.Failures, run.Journeys.Cancelled))
	}
	if run.Outcome != "PASS" {
		issues = append(issues, fmt.Sprintf("stage %d outcome: expected PASS, got %s", rate, run.Outcome))
	}
	if run.Quality == nil || run.Quality.State == report.QualityInvalid {
		state := "missing"
		if run.Quality != nil {
			state = run.Quality.State
		}
		issues = append(issues, fmt.Sprintf("stage %d measurement quality: expected a non-INVALID run quality state, got %s", rate, state))
	}
	if !inLatencyEnvelope(run.Metrics.P50MS) || !inLatencyEnvelope(run.Metrics.P95MS) ||
		!inLatencyEnvelope(run.Journeys.P50MS) || !inLatencyEnvelope(run.Journeys.P95MS) {
		issues = append(issues, fmt.Sprintf("stage %d latency envelope: expected request/journey p50 and p95 in [15,150]ms, got request=%.3f/%.3fms journey=%.3f/%.3fms", rate, run.Metrics.P50MS, run.Metrics.P95MS, run.Journeys.P50MS, run.Journeys.P95MS))
	}
	g := run.Generator
	if g == nil || g.Samples < 2 || !g.GoCPUValid || g.ProcessSamples < 2 || !g.ProcessCPUValid || g.PeakProcessRSSBytes == 0 ||
		!below(g.GoCPUUtilizationRatio, 0.9) || !below(g.MeanProcessCPUUtilizationRatio, 0.9) || g.PeakHeapAllocBytes > 256<<20 || g.PeakProcessRSSBytes > 512<<20 {
		issues = append(issues, fmt.Sprintf("stage %d generator envelope: expected valid runtime/process samples, sustained CPU < 0.90 and heap/RSS <= 256/512MiB; got samples=%d process_samples=%d rss=%d go_cpu_valid=%t process_cpu_valid=%t go_cpu=%.3f process_cpu_mean=%.3f process_cpu_peak=%.3f heap=%d", rate, generatorSamples(run), generatorProcessSamples(run), generatorRSS(run), generatorGoCPUValid(run), generatorProcessCPUValid(run), generatorGoCPU(run), generatorProcessCPU(run), generatorPeakProcessCPU(run), generatorHeap(run)))
	}
	lagP95Limit, lagMaxLimit := 500/float64(rate), 1000/float64(rate)
	if run.SchedulerLagP95MS > lagP95Limit || run.SchedulerLagMaxMS >= lagMaxLimit {
		issues = append(issues, fmt.Sprintf("stage %d scheduler lag: expected p95 <= %.3fms and max < %.3fms, got p95=%.3fms max=%.3fms", rate, lagP95Limit, lagMaxLimit, run.SchedulerLagP95MS, run.SchedulerLagMaxMS))
	}
	return issues
}

func inLatencyEnvelope(value float64) bool {
	finite := !math.IsNaN(value) && !math.IsInf(value, 0)
	withinBounds := value >= calibration.HealthyLatencyMinMS && value <= calibration.HealthyLatencyMaxMS
	return finite && withinBounds
}

func below(value, limit float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value < limit
}

func generatorSamples(run report.Result) int {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.Samples
}
func generatorProcessSamples(run report.Result) int {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.ProcessSamples
}
func generatorRSS(run report.Result) uint64 {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.PeakProcessRSSBytes
}
func generatorGoCPUValid(run report.Result) bool {
	return run.Generator != nil && run.Generator.GoCPUValid
}
func generatorProcessCPUValid(run report.Result) bool {
	return run.Generator != nil && run.Generator.ProcessCPUValid
}
func generatorGoCPU(run report.Result) float64 {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.GoCPUUtilizationRatio
}
func generatorProcessCPU(run report.Result) float64 {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.MeanProcessCPUUtilizationRatio
}
func generatorPeakProcessCPU(run report.Result) float64 {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.PeakProcessCPUUtilizationRatio
}
func generatorHeap(run report.Result) uint64 {
	if run.Generator == nil {
		return 0
	}
	return run.Generator.PeakHeapAllocBytes
}
