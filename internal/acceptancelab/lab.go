// Package acceptancelab runs a bounded end-to-end qualification of the local
// load generator against its independent loopback reference fixture.
package acceptancelab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/protocolcheck"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const (
	SchemaVersion       = 2
	calibratedProfiles  = 30
	MinRepeats          = 2
	MaxRepeats          = 3
	MaxExtendedRepeats  = 2
	MaxArtifactBytes    = 16 << 20
	MaxProcessLogBytes  = 1 << 20
	DefaultRunTimeout   = 235 * time.Second
	MaximumRunTimeout   = 240 * time.Second
	calibratorStageTime = 5 * time.Second
)

// Options describes one immutable evidence directory. The directory must not
// already exist so a failed attempt can never overwrite earlier evidence.
type Options struct {
	CalibratorPath  string
	OutputDir       string
	Repeats         int
	Extended        bool
	RunTimeout      time.Duration
	Runner          Runner
	qualityAssessor func(report.Result) *report.Quality
}

// Runner executes the calibrator without invoking a shell. Output streams are
// written as the process runs so interruption still leaves partial logs.
type Runner interface {
	Run(context.Context, string, []string, io.Writer, io.Writer) Execution
}

// Execution records the child-process result without interpreting its output.
type Execution struct {
	ExitCode int
	TimedOut bool
	Error    string
}

// Manifest is the audit record for all requested independent attempts.
type Manifest struct {
	SchemaVersion int           `json:"schema_version"`
	Status        string        `json:"status"`
	StartedAt     string        `json:"started_at"`
	FinishedAt    string        `json:"finished_at,omitempty"`
	Environment   Environment   `json:"environment"`
	Configuration Configuration `json:"configuration"`
	Rules         []Gate        `json:"rules"`
	Attempts      []Attempt     `json:"attempts"`
	P95Spread     []P95Spread   `json:"repeat_p95_spread,omitempty"`
	Transport     *Transport    `json:"transport,omitempty"`
	Limitations   []string      `json:"limitations"`
}

type Environment struct {
	OS               string `json:"os"`
	Architecture     string `json:"architecture"`
	GoVersion        string `json:"go_version"`
	LogicalCPUs      int    `json:"logical_cpus"`
	GOMAXPROCS       int    `json:"gomaxprocs"`
	CalibratorSHA256 string `json:"calibrator_sha256"`
	CalibratorBytes  int64  `json:"calibrator_bytes"`
}

type Configuration struct {
	IndependentRuns int    `json:"independent_runs"`
	Extended        bool   `json:"extended"`
	RunTimeout      string `json:"run_timeout"`
	TransportCases  int    `json:"transport_cases"`
	RunModel        string `json:"run_model"`
	EvidencePolicy  string `json:"evidence_policy"`
}

type Gate struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
}

type Attempt struct {
	Number               int          `json:"number"`
	Status               string       `json:"status"`
	StartedAt            string       `json:"started_at"`
	FinishedAt           string       `json:"finished_at,omitempty"`
	CalibrationArtifact  string       `json:"calibration_artifact"`
	StdoutArtifact       string       `json:"stdout_artifact"`
	StderrArtifact       string       `json:"stderr_artifact"`
	ExitCode             *int         `json:"exit_code,omitempty"`
	TimedOut             bool         `json:"timed_out,omitempty"`
	ProcessError         string       `json:"process_error,omitempty"`
	StdoutTruncated      bool         `json:"stdout_truncated,omitempty"`
	StderrTruncated      bool         `json:"stderr_truncated,omitempty"`
	ArtifactSHA256       string       `json:"artifact_sha256,omitempty"`
	ArtifactBytes        int64        `json:"artifact_bytes,omitempty"`
	PreflightFingerprint string       `json:"preflight_fingerprint,omitempty"`
	RunnerVersion        string       `json:"runner_version,omitempty"`
	SoakQuality          string       `json:"soak_quality,omitempty"`
	ProfileP95MS         []ProfileP95 `json:"profile_p95_ms,omitempty"`
	Gates                []Gate       `json:"gates"`
}

type ProfileP95 struct {
	ID    string  `json:"id"`
	Rate  int     `json:"arrival_rate"`
	P95MS float64 `json:"p95_ms"`
}

type P95Spread struct {
	ID             string  `json:"id"`
	Rate           int     `json:"arrival_rate"`
	Runs           int     `json:"runs"`
	MinimumMS      float64 `json:"minimum_ms"`
	MaximumMS      float64 `json:"maximum_ms"`
	RangeMS        float64 `json:"range_ms"`
	Interpretation string  `json:"interpretation"`
}

type Transport struct {
	Status         string `json:"status"`
	Artifact       string `json:"artifact"`
	StdoutArtifact string `json:"stdout_artifact"`
	StderrArtifact string `json:"stderr_artifact"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	TimedOut       bool   `json:"timed_out,omitempty"`
	ProcessError   string `json:"process_error,omitempty"`
	ArtifactSHA256 string `json:"artifact_sha256,omitempty"`
	ArtifactBytes  int64  `json:"artifact_bytes,omitempty"`
	Gates          []Gate `json:"gates"`
}

// Run performs every bounded calibration repeat and the HTTP transport matrix.
// It writes a state snapshot after each material transition and a final
// manifest even when a required gate fails.
func Run(ctx context.Context, options Options) (Manifest, error) {
	if err := validateOptions(options); err != nil {
		return Manifest{}, err
	}
	calibratorInfo, err := os.Stat(options.CalibratorPath)
	if err != nil || !calibratorInfo.Mode().IsRegular() {
		return Manifest{}, errors.New("calibrator must be an existing regular executable file")
	}
	calibratorHash, err := fileSHA256(options.CalibratorPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("identify calibrator executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(options.OutputDir)), 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create evidence parent directory: %w", err)
	}
	if err := os.Mkdir(options.OutputDir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create fresh evidence directory: %w", err)
	}
	if options.RunTimeout == 0 {
		options.RunTimeout = DefaultRunTimeout
	}
	if options.Runner == nil {
		options.Runner = ExecRunner{}
	}
	if options.qualityAssessor == nil {
		options.qualityAssessor = report.AssessQuality
	}
	started := time.Now().UTC()
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		Status:        "RUNNING",
		StartedAt:     started.Format(time.RFC3339Nano),
		Environment: Environment{
			OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version(),
			LogicalCPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0),
			CalibratorSHA256: calibratorHash, CalibratorBytes: calibratorInfo.Size(),
		},
		Configuration: Configuration{
			IndependentRuns: options.Repeats, Extended: options.Extended, RunTimeout: options.RunTimeout.String(),
			TransportCases: 10,
			RunModel:       "Each repeat starts a fresh calibrator process and a fresh independently counted loopback fixture; runs share this host and are not statistically independent samples.",
			EvidencePolicy: "Repeat-to-repeat p95 spread is descriptive only. No confidence interval or cross-host equivalence is inferred.",
		},
		Limitations: []string{
			"This lab qualifies only the recorded local loopback fixture, binary, runtime, workload matrix, and execution windows.",
			"Independent child processes do not establish independence of operating-system conditions or statistical observations.",
			"No external application capacity, network path, physical host identity, or universal generator ceiling is certified.",
		},
	}
	stateNumber := 0
	writeState := func() error {
		stateNumber++
		return writeJSON(filepath.Join(options.OutputDir, fmt.Sprintf("state-%03d.json", stateNumber)), manifest)
	}
	if err := writeState(); err != nil {
		return manifest, fmt.Errorf("persist initial evidence state: %w", err)
	}

	for number := 1; number <= options.Repeats; number++ {
		manifest.Attempts = append(manifest.Attempts, Attempt{Number: number, Status: "RUNNING"})
		if err := writeState(); err != nil {
			manifest.Attempts[number-1] = failAttempt(manifest.Attempts[number-1], "cannot persist pending attempt state: "+err.Error())
			break
		}
		attempt := runCalibration(ctx, options, number, func(startedAttempt Attempt) error {
			manifest.Attempts[number-1] = startedAttempt
			return writeState()
		})
		manifest.Attempts[number-1] = attempt
		if err := writeState(); err != nil {
			manifest.Status = "INCONCLUSIVE"
			manifest.Rules = append(manifest.Rules, Gate{Name: "persist_state", Status: "FAILED", Expected: "each attempt state is persisted", Observed: err.Error()})
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if len(manifest.Attempts) == options.Repeats {
		manifest.Rules = append(manifest.Rules, assessRepeatEquivalence(manifest.Attempts)...)
		manifest.P95Spread = summarizeP95Spread(manifest.Attempts)
	}
	if ctx.Err() == nil {
		manifest.Transport = runTransport(ctx, options, func(startedTransport *Transport) error {
			manifest.Transport = startedTransport
			return writeState()
		})
		if err := writeState(); err != nil {
			manifest.Status = "INCONCLUSIVE"
			manifest.Rules = append(manifest.Rules, Gate{Name: "persist_transport_state", Status: "FAILED", Expected: "transport result is persisted", Observed: err.Error()})
		}
	} else {
		manifest.Rules = append(manifest.Rules, Gate{Name: "run_completion", Status: "INCONCLUSIVE", Expected: "all requested independent runs and transport cases finish", Observed: "execution context ended before all runs completed"})
	}
	manifest.Rules = append(manifest.Rules, assessAttemptCompletion(manifest.Attempts, options.Repeats)...)
	if manifest.Transport != nil {
		manifest.Rules = append(manifest.Rules, transportGates(*manifest.Transport)...)
	}
	manifest.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	manifest.Status = overallStatus(manifest.Rules)
	if err := writeJSON(filepath.Join(options.OutputDir, "manifest.json"), manifest); err != nil {
		return manifest, fmt.Errorf("persist final evidence manifest: %w", err)
	}
	if manifest.Status != "PASS_LOCAL_REFERENCE_ONLY" {
		return manifest, fmt.Errorf("G0 acceptance lab did not pass: %s (evidence: %s)", manifest.Status, filepath.Join(options.OutputDir, "manifest.json"))
	}
	return manifest, nil
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.CalibratorPath) == "" || strings.TrimSpace(options.OutputDir) == "" {
		return errors.New("calibrator path and fresh evidence output directory are required")
	}
	if options.Repeats < MinRepeats || options.Repeats > MaxRepeats {
		return fmt.Errorf("independent repeats must be between %d and %d", MinRepeats, MaxRepeats)
	}
	if options.Extended && options.Repeats > MaxExtendedRepeats {
		return fmt.Errorf("extended runs permit at most %d independent repeats", MaxExtendedRepeats)
	}
	if options.RunTimeout != 0 && (options.RunTimeout < 30*time.Second || options.RunTimeout > MaximumRunTimeout) {
		return fmt.Errorf("run timeout must be between 30s and %s", MaximumRunTimeout)
	}
	return nil
}

func runCalibration(parent context.Context, options Options, number int, onStart func(Attempt) error) Attempt {
	name := fmt.Sprintf("repeat-%02d", number)
	directory := filepath.Join(options.OutputDir, name)
	_ = os.Mkdir(directory, 0o700)
	attempt := Attempt{
		Number: number, Status: "RUNNING", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		CalibrationArtifact: filepath.ToSlash(filepath.Join(name, "calibration.json")),
		StdoutArtifact:      filepath.ToSlash(filepath.Join(name, "stdout.log")),
		StderrArtifact:      filepath.ToSlash(filepath.Join(name, "stderr.log")),
	}
	stdoutFile, stdoutLimit, err := createLimitedLog(filepath.Join(directory, "stdout.log"))
	if err != nil {
		return failAttempt(attempt, "cannot create stdout evidence: "+err.Error())
	}
	stderrFile, stderrLimit, err := createLimitedLog(filepath.Join(directory, "stderr.log"))
	if err != nil {
		_ = stdoutFile.Close()
		return failAttempt(attempt, "cannot create stderr evidence: "+err.Error())
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()
	if err := onStart(attempt); err != nil {
		return failAttempt(attempt, "cannot persist running attempt state: "+err.Error())
	}
	runTimeout := options.RunTimeout
	if runTimeout == 0 {
		runTimeout = DefaultRunTimeout
	}
	ctx, cancel := context.WithTimeout(parent, runTimeout)
	defer cancel()
	args := []string{"--repeats", "1", "--out", filepath.Join(directory, "calibration.json")}
	if options.Extended {
		args = append(args, "--extended")
	}
	execution := options.Runner.Run(ctx, options.CalibratorPath, args, stdoutLimit, stderrLimit)
	_ = stdoutFile.Sync()
	_ = stderrFile.Sync()
	attempt.ExitCode = intPointer(execution.ExitCode)
	attempt.TimedOut = execution.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded)
	attempt.ProcessError = execution.Error
	attempt.StdoutTruncated = stdoutLimit.Truncated()
	attempt.StderrTruncated = stderrLimit.Truncated()
	attempt.Gates = assessCalibrationArtifact(filepath.Join(directory, "calibration.json"), &attempt, options.Extended, options.qualityAssessor)
	if execution.ExitCode != 0 || execution.Error != "" || attempt.TimedOut {
		observed := fmt.Sprintf("exit_code=%d timed_out=%t", execution.ExitCode, attempt.TimedOut)
		if execution.Error != "" {
			observed += " error=" + execution.Error
		}
		gateStatus := "FAILED"
		if attempt.TimedOut || (parent.Err() != nil) {
			gateStatus = "INCONCLUSIVE"
		}
		attempt.Gates = append(attempt.Gates, Gate{Name: "calibrator_process", Status: gateStatus, Expected: "exit code 0 within the bounded deadline", Observed: observed})
	}
	if ctx.Err() != nil && parent.Err() != nil {
		attempt.Gates = append(attempt.Gates, Gate{Name: "run_not_interrupted", Status: "INCONCLUSIVE", Expected: "requested lab completes", Observed: "parent context was cancelled"})
	}
	attempt.Status = statusFromGates(attempt.Gates)
	attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return attempt
}

func runTransport(parent context.Context, options Options, onStart func(*Transport) error) *Transport {
	directory := filepath.Join(options.OutputDir, "transport")
	_ = os.Mkdir(directory, 0o700)
	transport := &Transport{
		Status: "RUNNING", Artifact: "transport/transport.json",
		StdoutArtifact: "transport/stdout.log", StderrArtifact: "transport/stderr.log",
	}
	stdoutFile, stdoutLimit, err := createLimitedLog(filepath.Join(directory, "stdout.log"))
	if err != nil {
		transport.ProcessError = "cannot create stdout evidence: " + err.Error()
		transport.Status = "FAILED"
		return transport
	}
	stderrFile, stderrLimit, err := createLimitedLog(filepath.Join(directory, "stderr.log"))
	if err != nil {
		_ = stdoutFile.Close()
		transport.ProcessError = "cannot create stderr evidence: " + err.Error()
		transport.Status = "FAILED"
		return transport
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()
	if err := onStart(transport); err != nil {
		transport.Status = "FAILED"
		transport.ProcessError = "cannot persist running transport state: " + err.Error()
		return transport
	}
	runTimeout := options.RunTimeout
	if runTimeout == 0 {
		runTimeout = DefaultRunTimeout
	}
	ctx, cancel := context.WithTimeout(parent, min(runTimeout, 30*time.Second))
	defer cancel()
	execution := options.Runner.Run(ctx, options.CalibratorPath,
		[]string{"--transport-matrix", "--out", filepath.Join(directory, "transport.json")}, stdoutLimit, stderrLimit)
	_ = stdoutFile.Sync()
	_ = stderrFile.Sync()
	transport.ExitCode = intPointer(execution.ExitCode)
	transport.TimedOut = execution.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded)
	transport.ProcessError = execution.Error
	transport.Gates = assessTransportArtifact(filepath.Join(directory, "transport.json"))
	if execution.ExitCode != 0 || execution.Error != "" || transport.TimedOut {
		gateStatus := "FAILED"
		if transport.TimedOut || parent.Err() != nil {
			gateStatus = "INCONCLUSIVE"
		}
		transport.Gates = append(transport.Gates, Gate{Name: "transport_process", Status: gateStatus, Expected: "exit code 0 within 30 seconds", Observed: fmt.Sprintf("exit_code=%d timed_out=%t error=%s", execution.ExitCode, transport.TimedOut, execution.Error)})
	}
	transport.Status = statusFromGates(transport.Gates)
	if hash, size, err := fileDigest(filepath.Join(directory, "transport.json")); err == nil {
		transport.ArtifactSHA256, transport.ArtifactBytes = hash, size
	}
	return transport
}

type calibrationDocument struct {
	SchemaVersion int      `json:"schema_version"`
	Status        string   `json:"status"`
	Checks        []string `json:"checks"`
	Preflight     struct {
		SchemaVersion       int    `json:"schema_version"`
		ScenarioFingerprint string `json:"scenario_fingerprint_sha256"`
		Generator           struct {
			OS            string `json:"os"`
			Arch          string `json:"arch"`
			LogicalCPUs   int    `json:"logical_cpus"`
			GOMAXPROCS    int    `json:"gomaxprocs"`
			GoVersion     string `json:"go_version"`
			RunnerVersion string `json:"runner_version"`
		} `json:"generator"`
		Fixture struct {
			HealthyRates     []int               `json:"healthy_rates"`
			Extended         bool                `json:"extended"`
			WorkloadProfiles []calibratedProfile `json:"workload_profiles"`
		} `json:"fixture"`
	} `json:"preflight"`
	Runs                 []calibrationRun       `json:"runs"`
	WorkloadRuns         []report.Result        `json:"workload_runs"`
	WorkloadObservations []workloadObservation  `json:"workload_observations"`
	SoakRun              report.Result          `json:"soak_run"`
	Server               calibrationServer      `json:"server"`
	OverloadRun          calibrationOverloadRun `json:"overload_run"`
	OverloadServer       calibrationServer      `json:"overload_server"`
	FailureRun           calibrationFailureRun  `json:"failure_run"`
	FailureServer        calibrationServer      `json:"failure_server"`
}

type calibrationRun struct {
	ArrivalsScheduled   int64  `json:"arrivals_scheduled"`
	ArrivalsAdmitted    *int64 `json:"arrivals_admitted"`
	ArrivalsDropped     int64  `json:"arrivals_dropped"`
	IterationsStarted   int64  `json:"iterations_started"`
	IterationsCompleted int64  `json:"iterations_completed"`
	Outcome             string `json:"outcome"`
	Config              struct {
		Mode           string `json:"mode"`
		ArrivalRate    int    `json:"arrival_rate"`
		MaxRPS         int    `json:"max_rps"`
		Users          int    `json:"users"`
		Duration       string `json:"duration"`
		RequestTimeout string `json:"request_timeout"`
	} `json:"config"`
	Metrics struct {
		Total     int `json:"total"`
		Success   int `json:"success"`
		Failures  int `json:"failures"`
		Cancelled int `json:"cancelled"`
	} `json:"metrics"`
}

type calibrationServer struct {
	Received          int64 `json:"received"`
	Completed         int64 `json:"completed"`
	Success           int64 `json:"success"`
	Failures          int64 `json:"failures"`
	Cancelled         int64 `json:"cancelled"`
	Rejected          int64 `json:"rejected"`
	RequestBodyBytes  int64 `json:"RequestBodyBytes"`
	ResponseBodyBytes int64 `json:"ResponseBodyBytes"`
}

type calibrationOverloadRun struct {
	ArrivalsScheduled   int64 `json:"arrivals_scheduled"`
	ArrivalsDropped     int64 `json:"arrivals_dropped"`
	ArrivalsDroppedBusy int64 `json:"arrivals_dropped_busy"`
	IterationsStarted   int64 `json:"iterations_started"`
	Metrics             struct {
		Total     int `json:"total"`
		Cancelled int `json:"cancelled"`
	} `json:"metrics"`
	Outcome string `json:"outcome"`
}

type calibrationFailureRun struct {
	Metrics struct {
		Total     int                    `json:"total"`
		Success   int                    `json:"success"`
		Failures  int                    `json:"failures"`
		Cancelled int                    `json:"cancelled"`
		HTTP      report.HTTPDiagnostics `json:"http_diagnostics"`
	} `json:"metrics"`
	Outcome string `json:"outcome"`
}

type calibratedProfile struct {
	ID                string `json:"id"`
	Mode              string `json:"mode"`
	TransportScheme   string `json:"transport_scheme"`
	Method            string `json:"method"`
	Path              string `json:"path"`
	RequestBodyBytes  int    `json:"request_body_bytes"`
	ResponseBodyBytes int    `json:"response_body_bytes"`
	ArrivalRate       int    `json:"arrival_rate"`
	MaxRPS            int    `json:"max_rps"`
	Users             int    `json:"users"`
	Duration          string `json:"duration"`
	Timeout           string `json:"timeout"`
	ConnectionPolicy  string `json:"connection_policy"`
}

type workloadObservation struct {
	ProfileID                      string `json:"profile_id"`
	Scheduled                      int64  `json:"scheduled"`
	Admitted                       int64  `json:"admitted"`
	Started                        int64  `json:"started"`
	Completed                      int64  `json:"completed"`
	Dropped                        int64  `json:"dropped"`
	Outcome                        string `json:"outcome"`
	ClientCompleteBodyObservations int64  `json:"client_complete_body_observations"`
	ClientResponseMinBytes         int64  `json:"client_response_min_bytes"`
	ClientResponseMaxBytes         int64  `json:"client_response_max_bytes"`
	ClientResponseTotalBytes       int64  `json:"client_response_total_bytes"`
	ServerReceived                 int64  `json:"server_received"`
	ServerCompleted                int64  `json:"server_completed"`
	ServerTCPConnections           int64  `json:"server_tcp_connections"`
	ServerRequestBodyBytes         int64  `json:"server_request_body_bytes"`
	ServerResponseBodyBytes        int64  `json:"server_response_body_bytes"`
}

func assessCalibrationArtifact(path string, attempt *Attempt, extended bool, qualityAssessor func(report.Result) *report.Quality) []Gate {
	data, err := readBounded(path, MaxArtifactBytes)
	if err != nil {
		return []Gate{{Name: "calibration_artifact", Status: "INCONCLUSIVE", Expected: "bounded readable calibration JSON is preserved", Observed: err.Error()}}
	}
	hash := sha256.Sum256(data)
	attempt.ArtifactSHA256 = hex.EncodeToString(hash[:])
	attempt.ArtifactBytes = int64(len(data))
	var document calibrationDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return []Gate{{Name: "calibration_artifact", Status: "FAILED", Expected: "well-formed calibration JSON", Observed: "JSON decode failed"}}
	}
	attempt.PreflightFingerprint = document.Preflight.ScenarioFingerprint
	attempt.RunnerVersion = document.Preflight.Generator.RunnerVersion
	if document.SoakRun.Quality != nil {
		attempt.SoakQuality = document.SoakRun.Quality.State
	}
	recomputedQuality := qualityAssessor(document.SoakRun)
	gates := []Gate{
		gate("calibration_schema", document.SchemaVersion == 1 && document.Preflight.SchemaVersion == 6, "calibration report v1 with preflight schema v6", fmt.Sprintf("report=%d preflight=%d", document.SchemaVersion, document.Preflight.SchemaVersion)),
		gate("requested_run_matrix", document.Preflight.Fixture.Extended == extended, fmt.Sprintf("extended=%t", extended), fmt.Sprintf("extended=%t", document.Preflight.Fixture.Extended)),
		gate("calibration_status", document.Status == "REFERENCE_SMOKE_PASSED", "REFERENCE_SMOKE_PASSED", document.Status),
		gate("calibration_checklist", len(document.Checks) == 0, "zero failed checks", fmt.Sprintf("%d failed checks", len(document.Checks))),
		gate("exact_profile_matrix", validProfileMatrix(document.Preflight.Fixture.WorkloadProfiles), "30 unique HTTP loopback shape/connection/rate pairs at exact rates 2, 5, and 10 arrivals/s", fmt.Sprintf("%d profiles", len(document.Preflight.Fixture.WorkloadProfiles))),
		gate("native_engine_profile_reports", workloadRunsMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadRuns), "every profile report reconciles configured arrival controls, client outcomes, response size, and HTTP status counters", fmt.Sprintf("%d reports for %d profiles", len(document.WorkloadRuns), len(document.Preflight.Fixture.WorkloadProfiles))),
		gate("independent_profile_ledgers", profileLedgersMatch(document.Preflight.Fixture.WorkloadProfiles, document.WorkloadObservations), "every planned arrival, complete body, independent request/byte ledger, and accepted TCP connection count reconciles per profile", fmt.Sprintf("%d observations for %d profiles", len(document.WorkloadObservations), len(document.Preflight.Fixture.WorkloadProfiles))),
		gate("healthy_stage_delivery", healthyStagesMatch(document.Preflight.Fixture.HealthyRates, document.Runs, extended), "all required exact-rate stages deliver every planned request with zero drops", fmt.Sprintf("%d rates, %d stages", len(document.Preflight.Fixture.HealthyRates), len(document.Runs))),
		gate("aggregate_healthy_server_ledger", aggregateServerLedgerMatches(document), "aggregate reference receipt, completion, success, and body-byte counters reconcile with healthy stages, profile matrix, and soak", serverDescription(document.Server)),
		gate("persisted_quality_recomputes", document.SoakRun.Quality != nil && reflect.DeepEqual(recomputedQuality, document.SoakRun.Quality), "persisted soak quality exactly matches quality recalculated from the stored report", "persisted="+qualityState(document.SoakRun.Quality)+" recomputed="+qualityState(recomputedQuality)),
		gate("controlled_soak_quality", soakMatches(document.SoakRun), "300 planned, admitted, started, completed and independently completed requests; VALID_WITHIN_ENVELOPE", soakDescription(document.SoakRun)),
		gate("overload_negative_control", overloadControlMatches(document), "bounded overload is visible as worker-busy drops and reconciled server receipts, never a pass", overloadDescription(document)),
		gate("http_failure_negative_control", failureControlMatches(document), "two induced HTTP failures are visible in ten independently server-observed requests", failureDescription(document)),
	}
	if len(document.WorkloadRuns) == len(document.Preflight.Fixture.WorkloadProfiles) {
		for index, profile := range document.Preflight.Fixture.WorkloadProfiles {
			attempt.ProfileP95MS = append(attempt.ProfileP95MS, ProfileP95{ID: profile.ID, Rate: profile.ArrivalRate, P95MS: document.WorkloadRuns[index].Metrics.P95MS})
		}
		sortProfileSummaries(attempt.ProfileP95MS)
	}
	if len(document.Checks) > 0 {
		gates = append(gates, Gate{Name: "calibrator_diagnostics", Status: "FAILED", Expected: "no calibration discrepancies", Observed: strings.Join(document.Checks, "; ")})
	}
	return gates
}

func validProfileMatrix(profiles []calibratedProfile) bool {
	if len(profiles) != calibratedProfiles {
		return false
	}
	seen := make(map[string]bool, len(profiles))
	shapeRates := make(map[string]bool, len(profiles))
	profileDurations := map[string]string{"reuse": "5s", "new_per_request": "2s"}
	shapes := map[string]bool{
		"GET|/work|0|4096":               true,
		"GET|/matrix/get-empty|0|0":      true,
		"GET|/matrix/get-1k|0|1024":      true,
		"GET|/matrix/get-64k|0|65536":    true,
		"POST|/matrix/post-4k|4096|4096": true,
	}
	rates := map[int]bool{2: true, 5: true, 10: true}
	for _, profile := range profiles {
		if profile.ID == "" || seen[profile.ID] || !rates[profile.ArrivalRate] || profile.Mode != "arrival" || profile.TransportScheme != "http" || profile.MaxRPS != 50 || profile.Users != 10 || profile.Duration != profileDurations[profile.ConnectionPolicy] || profile.Timeout != "1s" {
			return false
		}
		shape := fmt.Sprintf("%s|%s|%d|%d", profile.Method, profile.Path, profile.RequestBodyBytes, profile.ResponseBodyBytes)
		if !shapes[shape] {
			return false
		}
		key := fmt.Sprintf("%s|%s|%d", shape, profile.ConnectionPolicy, profile.ArrivalRate)
		if shapeRates[key] {
			return false
		}
		seen[profile.ID] = true
		shapeRates[key] = true
	}
	if len(shapeRates) != calibratedProfiles {
		return false
	}
	for shape := range shapes {
		for policy := range profileDurations {
			for rate := range rates {
				if !shapeRates[fmt.Sprintf("%s|%s|%d", shape, policy, rate)] {
					return false
				}
			}
		}
	}
	return true
}

func profileLedgersMatch(profiles []calibratedProfile, observations []workloadObservation) bool {
	if len(profiles) != calibratedProfiles || len(observations) != len(profiles) {
		return false
	}
	byID := make(map[string]workloadObservation, len(observations))
	for _, observation := range observations {
		if observation.ProfileID == "" || byID[observation.ProfileID].ProfileID != "" {
			return false
		}
		byID[observation.ProfileID] = observation
	}
	for _, profile := range profiles {
		observation, ok := byID[profile.ID]
		planned := plannedProfileArrivals(profile)
		if planned < 0 {
			return false
		}
		expectedConnections, supported := calibration.ExpectedReferenceTCPConnections(calibration.WorkloadProfile{ConnectionPolicy: profile.ConnectionPolicy}, planned)
		if !ok || !supported || observation.ServerTCPConnections != expectedConnections || observation.Scheduled != planned || observation.Admitted != planned || observation.Started != planned || observation.Completed != planned || observation.Dropped != 0 || observation.Outcome != "PASS" ||
			observation.ClientCompleteBodyObservations != planned || observation.ClientResponseMinBytes != int64(profile.ResponseBodyBytes) || observation.ClientResponseMaxBytes != int64(profile.ResponseBodyBytes) || observation.ClientResponseTotalBytes != planned*int64(profile.ResponseBodyBytes) ||
			observation.ServerReceived != planned || observation.ServerCompleted != planned || observation.ServerRequestBodyBytes != planned*int64(profile.RequestBodyBytes) || observation.ServerResponseBodyBytes != planned*int64(profile.ResponseBodyBytes) {
			return false
		}
	}
	return true
}

func workloadRunsMatch(profiles []calibratedProfile, runs []report.Result) bool {
	if len(profiles) != calibratedProfiles || len(runs) != len(profiles) {
		return false
	}
	for index, profile := range profiles {
		run := runs[index]
		planned := plannedProfileArrivals(profile)
		if planned < 0 {
			return false
		}
		body := run.Metrics.ResponseBody
		if run.ArrivalsScheduled != planned || run.ArrivalsAdmitted == nil || *run.ArrivalsAdmitted != planned || run.ArrivalsDropped != 0 || run.IterationsStarted != planned || run.IterationsCompleted != planned || run.Outcome != "PASS" ||
			run.Metrics.Total != int(planned) || run.Metrics.Success != int(planned) || run.Metrics.Failures != 0 || run.Metrics.Cancelled != 0 ||
			body.CompleteObservations != planned || body.IncompleteObservations != 0 || body.OversizedObservations != 0 || body.MinBytes != int64(profile.ResponseBodyBytes) || body.MaxBytes != int64(profile.ResponseBodyBytes) || body.TotalBytes != planned*int64(profile.ResponseBodyBytes) ||
			run.Metrics.HTTP.StatusCodes[200] != int(planned) || run.Metrics.HTTP.Status2xx != int(planned) ||
			run.Config.Mode != profile.Mode || run.Config.ArrivalRate != profile.ArrivalRate || run.Config.MaxRPS != profile.MaxRPS || run.Config.Users != profile.Users || run.Config.Duration != profile.Duration || run.Config.RequestTimeout != profile.Timeout || run.Config.ConnectionPolicy != profile.ConnectionPolicy ||
			len(run.Steps) != 1 || run.Steps[0].Method != profile.Method || run.Steps[0].Path != profile.Path || run.Steps[0].Metrics.Total != int(planned) || run.Steps[0].Metrics.Success != int(planned) || run.Steps[0].Metrics.HTTP.StatusCodes[200] != int(planned) {
			return false
		}
	}
	return true
}

func plannedProfileArrivals(profile calibratedProfile) int64 {
	duration, err := time.ParseDuration(profile.Duration)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return -1
	}
	return int64(profile.ArrivalRate) * int64(duration/time.Second)
}

func healthyStagesMatch(rates []int, runs []calibrationRun, extended bool) bool {
	expectedRates := []int{2, 5, 10}
	if extended {
		expectedRates = append(expectedRates, 15, 20, 50, 100)
	}
	if !reflect.DeepEqual(rates, expectedRates) || len(rates) != len(runs) {
		return false
	}
	for i, rate := range rates {
		planned := int64(rate) * int64(calibratorStageTime/time.Second)
		run := runs[i]
		if run.ArrivalsScheduled != planned || run.ArrivalsAdmitted == nil || *run.ArrivalsAdmitted != planned || run.ArrivalsDropped != 0 || run.IterationsStarted != planned || run.IterationsCompleted != planned || run.Outcome != "PASS" ||
			run.Metrics.Total != int(planned) || run.Metrics.Success != int(planned) || run.Metrics.Failures != 0 || run.Metrics.Cancelled != 0 || run.Config.Mode != "arrival" || run.Config.ArrivalRate != rate || run.Config.MaxRPS != max(50, rate) || run.Config.Users != 10 || run.Config.Duration != calibratorStageTime.String() || run.Config.RequestTimeout != "1s" {
			return false
		}
	}
	return true
}

func soakMatches(soak report.Result) bool {
	if soak.Quality == nil || soak.Quality.State != "VALID_WITHIN_ENVELOPE" || soak.ReferenceEvidence == nil {
		return false
	}
	const planned int64 = 300
	return soak.ArrivalsScheduled == planned && soak.ArrivalsAdmitted != nil && *soak.ArrivalsAdmitted == planned && soak.ArrivalsDropped == 0 && soak.IterationsStarted == planned && soak.IterationsCompleted == planned && soak.Outcome == "PASS" && soak.SchedulerLagSamples == planned && soak.SchedulerLagP95MS <= 50 && soak.SchedulerLagMaxMS < 100 &&
		soak.Metrics.Total == int(planned) && soak.Metrics.Success == int(planned) && soak.Metrics.Failures == 0 && soak.Metrics.ResponseBody.CompleteObservations == planned && soak.Metrics.ResponseBody.MinBytes == 4096 && soak.Metrics.ResponseBody.MaxBytes == 4096 && soak.Metrics.ResponseBody.TotalBytes == planned*4096 &&
		soak.ReferenceEvidence.ServerReceived == planned && soak.ReferenceEvidence.ServerCompleted == planned && soak.ReferenceEvidence.ServerSuccess == planned && soak.ReferenceEvidence.ServerFailures == 0 && soak.ReferenceEvidence.ServerCancelled == 0 && soak.ReferenceEvidence.ServerRejected == 0
}

func soakDescription(soak report.Result) string {
	state := "missing"
	if soak.Quality != nil {
		state = soak.Quality.State
	}
	return fmt.Sprintf("scheduled=%d admitted=%s started=%d completed=%d server_received=%s server_completed=%s lag_samples=%d lag_p95_ms=%.3f lag_max_ms=%.3f quality=%s", soak.ArrivalsScheduled, pointerValue(soak.ArrivalsAdmitted), soak.IterationsStarted, soak.IterationsCompleted, serverValue(soak.ReferenceEvidence, true), serverValue(soak.ReferenceEvidence, false), soak.SchedulerLagSamples, soak.SchedulerLagP95MS, soak.SchedulerLagMaxMS, state)
}

func assessTransportArtifact(path string) []Gate {
	data, err := readBounded(path, MaxArtifactBytes)
	if err != nil {
		return []Gate{{Name: "transport_artifact", Status: "INCONCLUSIVE", Expected: "bounded readable transport JSON is preserved", Observed: err.Error()}}
	}
	var document protocolcheck.Result
	if err := json.Unmarshal(data, &document); err != nil {
		return []Gate{{Name: "transport_artifact", Status: "FAILED", Expected: "well-formed transport JSON", Observed: "JSON decode failed"}}
	}
	validationError := protocolcheck.Validate(document)
	allReconciled := validationError == nil && document.Status == "TRANSPORT_FIXTURES_PASSED"
	validationDetails := "all ten exact fixture definitions validate"
	if validationError != nil {
		validationDetails = validationError.Error()
	}
	return []Gate{
		gate("transport_schema", document.SchemaVersion == protocolcheck.SchemaVersion && document.Kind == "http_transport_reference" && document.BoundedToLoopback, "schema-v1 loopback-only HTTP transport fixture", fmt.Sprintf("schema=%d kind=%s loopback=%t", document.SchemaVersion, document.Kind, document.BoundedToLoopback)),
		gate("transport_cases", document.Status == "TRANSPORT_FIXTURES_PASSED" && len(document.Cases) == 10, "10 bounded HTTP/HTTPS fixture cases pass", fmt.Sprintf("status=%s cases=%d", document.Status, len(document.Cases))),
		gate("transport_independent_ledgers", allReconciled, "every exact case reconciles client completion, first-byte, connection, and independent server ledgers", validationDetails),
	}
}

func assessRepeatEquivalence(attempts []Attempt) []Gate {
	if len(attempts) < MinRepeats {
		return []Gate{{Name: "repeat_equivalence", Status: "INCONCLUSIVE", Expected: "at least two independent process runs", Observed: fmt.Sprintf("%d completed attempts", len(attempts))}}
	}
	fingerprint, runner := attempts[0].PreflightFingerprint, attempts[0].RunnerVersion
	matched := fingerprint != "" && runner != ""
	for _, attempt := range attempts[1:] {
		matched = matched && attempt.PreflightFingerprint == fingerprint && attempt.RunnerVersion == runner
	}
	return []Gate{{Name: "repeat_equivalence", Status: status(matched), Expected: "all runs use the same fixture fingerprint and exact generator identity", Observed: fmt.Sprintf("runs=%d fingerprint_match=%t runner_identity_match=%t", len(attempts), fingerprint != "" && allSameFingerprint(attempts), runner != "" && allSameRunner(attempts))}}
}

func assessAttemptCompletion(attempts []Attempt, expected int) []Gate {
	if len(attempts) != expected {
		return []Gate{{Name: "independent_repeat_completion", Status: "INCONCLUSIVE", Expected: fmt.Sprintf("%d complete passing independent runs", expected), Observed: fmt.Sprintf("%d runs completed", len(attempts))}}
	}
	passed := true
	failed := false
	for _, attempt := range attempts {
		passed = passed && attempt.Status == "PASSED"
		failed = failed || attempt.Status == "FAILED"
	}
	gateStatus := "INCONCLUSIVE"
	if passed {
		gateStatus = "PASSED"
	} else if failed {
		gateStatus = "FAILED"
	}
	return []Gate{{Name: "independent_repeat_completion", Status: gateStatus, Expected: fmt.Sprintf("%d complete passing independent runs", expected), Observed: fmt.Sprintf("%d runs; statuses=%s", len(attempts), attemptStatuses(attempts))}}
}

func transportGates(transport Transport) []Gate {
	return transport.Gates
}

func overallStatus(gates []Gate) string {
	if len(gates) == 0 {
		return "INCONCLUSIVE"
	}
	allPass := true
	for _, gate := range gates {
		if gate.Status == "FAILED" {
			return "FAILED"
		}
		if gate.Status != "PASSED" {
			allPass = false
		}
	}
	if allPass {
		return "PASS_LOCAL_REFERENCE_ONLY"
	}
	return "INCONCLUSIVE"
}

func statusFromGates(gates []Gate) string {
	switch overallStatus(gates) {
	case "PASS_LOCAL_REFERENCE_ONLY":
		return "PASSED"
	case "FAILED":
		return "FAILED"
	default:
		return "INCONCLUSIVE"
	}
}

func gate(name string, passed bool, expected, observed string) Gate {
	return Gate{Name: name, Status: status(passed), Expected: expected, Observed: observed}
}

func status(passed bool) string {
	if passed {
		return "PASSED"
	}
	return "FAILED"
}

func failAttempt(attempt Attempt, message string) Attempt {
	attempt.Status = "FAILED"
	attempt.ProcessError = message
	attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	attempt.Gates = []Gate{{Name: "attempt_execution", Status: "FAILED", Expected: "bounded child process and persistent logs", Observed: message}}
	return attempt
}

func allSameFingerprint(attempts []Attempt) bool {
	if len(attempts) == 0 || attempts[0].PreflightFingerprint == "" {
		return false
	}
	for _, attempt := range attempts[1:] {
		if attempt.PreflightFingerprint != attempts[0].PreflightFingerprint {
			return false
		}
	}
	return true
}

func allSameRunner(attempts []Attempt) bool {
	if len(attempts) == 0 || attempts[0].RunnerVersion == "" {
		return false
	}
	for _, attempt := range attempts[1:] {
		if attempt.RunnerVersion != attempts[0].RunnerVersion {
			return false
		}
	}
	return true
}

func attemptStatuses(attempts []Attempt) string {
	values := make([]string, len(attempts))
	for index, attempt := range attempts {
		values[index] = attempt.Status
	}
	return strings.Join(values, ",")
}

func pointerValue(value *int64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprint(*value)
}

func qualityState(value *report.Quality) string {
	if value == nil {
		return "missing"
	}
	return value.State
}

func serverValue(value *report.ControlledReferenceEvidence, received bool) string {
	if value == nil {
		return "unknown"
	}
	if received {
		return fmt.Sprint(value.ServerReceived)
	}
	return fmt.Sprint(value.ServerCompleted)
}

func aggregateServerLedgerMatches(document calibrationDocument) bool {
	if len(document.Runs) != len(document.Preflight.Fixture.HealthyRates) {
		return false
	}
	var expectedRequests, expectedRequestBytes, expectedResponseBytes int64
	for _, rate := range document.Preflight.Fixture.HealthyRates {
		planned := int64(rate) * int64(calibratorStageTime/time.Second)
		expectedRequests += planned
		expectedResponseBytes += planned * 4096
	}
	for _, profile := range document.Preflight.Fixture.WorkloadProfiles {
		planned := plannedProfileArrivals(profile)
		if planned < 0 {
			return false
		}
		expectedRequests += planned
		expectedRequestBytes += planned * int64(profile.RequestBodyBytes)
		expectedResponseBytes += planned * int64(profile.ResponseBodyBytes)
	}
	const soakRequests int64 = 300
	expectedRequests += soakRequests
	expectedResponseBytes += soakRequests * 4096
	server := document.Server
	return server.Received == expectedRequests && server.Completed == expectedRequests && server.Success == expectedRequests && server.Failures == 0 && server.Cancelled == 0 && server.Rejected == 0 && server.RequestBodyBytes == expectedRequestBytes && server.ResponseBodyBytes == expectedResponseBytes
}

func overloadControlMatches(document calibrationDocument) bool {
	run, server := document.OverloadRun, document.OverloadServer
	return run.ArrivalsScheduled == 50 && run.ArrivalsDropped > 0 && run.ArrivalsDropped == run.ArrivalsDroppedBusy &&
		run.IterationsStarted+run.ArrivalsDropped == run.ArrivalsScheduled && run.Outcome != "PASS" &&
		server.Received == run.IterationsStarted && server.Completed == int64(run.Metrics.Total) && server.Success == server.Completed &&
		server.Failures == 0 && server.Cancelled == int64(run.Metrics.Cancelled) && server.Rejected == 0
}

func overloadDescription(document calibrationDocument) string {
	run, server := document.OverloadRun, document.OverloadServer
	return fmt.Sprintf("scheduled=%d started=%d dropped=%d busy=%d server_received=%d completed=%d cancelled=%d rejected=%d outcome=%s", run.ArrivalsScheduled, run.IterationsStarted, run.ArrivalsDropped, run.ArrivalsDroppedBusy, server.Received, server.Completed, server.Cancelled, server.Rejected, run.Outcome)
}

func failureControlMatches(document calibrationDocument) bool {
	run, server := document.FailureRun, document.FailureServer
	statusCodes := run.Metrics.HTTP.StatusCodes
	return run.Metrics.Total == 10 && run.Metrics.Success == 8 && run.Metrics.Failures == 2 && run.Metrics.Cancelled == 0 && run.Outcome == "FAIL" &&
		len(statusCodes) == 2 && statusCodes[200] == 8 && statusCodes[503] == 2 && run.Metrics.HTTP.Status2xx == 8 && run.Metrics.HTTP.Status5xx == 2 &&
		server.Received == 10 && server.Completed == 10 && server.Success == 8 && server.Failures == 2 && server.Cancelled == 0 && server.Rejected == 0
}

func failureDescription(document calibrationDocument) string {
	run, server := document.FailureRun, document.FailureServer
	return fmt.Sprintf("client requests=%d success=%d failures=%d server received=%d completed=%d success=%d failures=%d cancelled=%d rejected=%d outcome=%s", run.Metrics.Total, run.Metrics.Success, run.Metrics.Failures, server.Received, server.Completed, server.Success, server.Failures, server.Cancelled, server.Rejected, run.Outcome)
}

func serverDescription(server calibrationServer) string {
	return fmt.Sprintf("received=%d completed=%d success=%d failures=%d cancelled=%d rejected=%d request_bytes=%d response_bytes=%d", server.Received, server.Completed, server.Success, server.Failures, server.Cancelled, server.Rejected, server.RequestBodyBytes, server.ResponseBodyBytes)
}

func summarizeP95Spread(attempts []Attempt) []P95Spread {
	values := make(map[string][]float64)
	for _, attempt := range attempts {
		for _, item := range attempt.ProfileP95MS {
			key := fmt.Sprintf("%s|%d", item.ID, item.Rate)
			values[key] = append(values[key], item.P95MS)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]P95Spread, 0, len(keys))
	for _, key := range keys {
		observations := values[key]
		if len(observations) == 0 {
			continue
		}
		minimum, maximum := observations[0], observations[0]
		for _, observation := range observations[1:] {
			minimum = min(minimum, observation)
			maximum = max(maximum, observation)
		}
		separator := strings.LastIndexByte(key, '|')
		id, rate := key[:separator], 0
		_, _ = fmt.Sscanf(key[separator+1:], "%d", &rate)
		result = append(result, P95Spread{
			ID: id, Rate: rate, Runs: len(observations), MinimumMS: minimum,
			MaximumMS: maximum, RangeMS: maximum - minimum,
			Interpretation: "Descriptive range across independent process executions; not a confidence interval.",
		})
	}
	return result
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return nil, errors.New("artifact is empty or exceeds the 16 MiB limit")
	}
	return data, nil
}

func fileSHA256(path string) (string, error) {
	hash, _, err := fileDigest(path)
	return hash, err
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, MaxArtifactBytes+1))
	if err != nil {
		return "", count, err
	}
	if count > MaxArtifactBytes {
		return "", count, errors.New("artifact exceeds the 16 MiB limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), count, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > MaxArtifactBytes {
		return errors.New("evidence manifest exceeds the 16 MiB limit")
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

type limitedWriter struct {
	writer    io.Writer
	limit     int64
	written   int64
	truncated bool
}

func (writer *limitedWriter) Write(data []byte) (int, error) {
	remaining := writer.limit - writer.written
	if remaining <= 0 {
		writer.truncated = true
		return len(data), nil
	}
	writeData := data
	if int64(len(writeData)) > remaining {
		writeData = writeData[:remaining]
		writer.truncated = true
	}
	count, err := writer.writer.Write(writeData)
	writer.written += int64(count)
	if err == nil && count != len(writeData) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return count, err
	}
	return len(data), nil
}

func (writer *limitedWriter) Truncated() bool { return writer.truncated }

func createLimitedLog(path string) (*os.File, *limitedWriter, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return file, &limitedWriter{writer: file, limit: MaxProcessLogBytes}, nil
}

func intPointer(value int) *int { return &value }

func sortProfileSummaries(values []ProfileP95) {
	sort.Slice(values, func(left, right int) bool {
		if values[left].ID == values[right].ID {
			return values[left].Rate < values[right].Rate
		}
		return values[left].ID < values[right].ID
	})
}
