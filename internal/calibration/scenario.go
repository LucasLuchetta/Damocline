package calibration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
)

// Workload self-calibration (ADR 0154) replays one declared scenario shape
// against a bounded loopback mirror on the generator host. It is generator
// evidence only: it never contacts the target and cannot establish target
// identity, application behavior, or workload representativeness.
const (
	ScenarioCalibrationKind          = "workload_self_calibration_v1"
	ScenarioCalibrationSchemaVersion = 1
	ScenarioCalibrationPassed        = "SELF_CALIBRATION_PASSED"
	ScenarioCalibrationFailed        = "CHECKS_FAILED"
	ScenarioCalibrationNetworkMode   = "loopback_same_host_same_process_mirror"
	// ScenarioCalibrationMaxAge matches the 24-hour review threshold used by
	// the fixed reference calibration gates.
	ScenarioCalibrationMaxAge = 24 * time.Hour
	// ScenarioCalibrationMaxLevels bounds one invocation (for example a
	// closed-loop explore ladder).
	ScenarioCalibrationMaxLevels = 12
	// ScenarioCalibrationMaxWallTime is the absolute wall-time cap for one
	// self-calibration invocation, including all levels and drains.
	ScenarioCalibrationMaxWallTime = 15 * time.Minute
	// Generator-overhead limits: client request latency beyond the mirror's
	// declared server delay on loopback, and the open-arrival scheduler lag
	// limits reused from the fixed reference soak.
	ScenarioOverheadP50LimitMS = 25.0
	ScenarioOverheadP95LimitMS = 50.0
	// Think-time accuracy: observed mean pause may exceed the declared mean by
	// at most this fraction plus the absolute allowance.
	ScenarioThinkTimeRelativeTolerance   = 0.10
	ScenarioThinkTimeAbsoluteToleranceMS = 20.0
	// Closed-loop delivery: achieved request starts must reach this fraction
	// of the bound implied by users, server delay, think time and max_rps.
	ScenarioMinimumDeliveryRatio = 0.85
)

var scenarioFingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var safeLabelPattern = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,120}$`)

// ScenarioGenerator binds the artifact to the generator executable and the
// host CPU entitlement observed while calibrating.
type ScenarioGenerator struct {
	OS                   string  `json:"os"`
	Architecture         string  `json:"architecture"`
	GoVersion            string  `json:"go_version"`
	RunnerIdentityMethod string  `json:"runner_identity_method"`
	RunnerIdentity       string  `json:"runner_identity"`
	LogicalCPUs          int     `json:"logical_cpus"`
	GOMAXPROCS           int     `json:"gomaxprocs"`
	CPUCapacityCores     float64 `json:"cpu_capacity_cores"`
	CPUCapacitySource    string  `json:"cpu_capacity_source"`
	CPUCapacityKnown     bool    `json:"cpu_capacity_known"`
}

// MirrorRoute is one loopback mirror response. Paths are scenario path
// prefixes before any template variable or query string.
type MirrorRoute struct {
	Method        string  `json:"method"`
	PathPrefix    string  `json:"path_prefix"`
	Status        int     `json:"status"`
	ResponseBytes int     `json:"response_bytes"`
	DelayMS       float64 `json:"delay_ms"`
}

// MirrorModel records where mirror response sizes and delays came from. A
// recorded report is identified only by its content digest.
type MirrorModel struct {
	Source       string        `json:"source"`
	SourceSHA256 string        `json:"source_sha256,omitempty"`
	Routes       []MirrorRoute `json:"routes"`
	ReplayNotes  []string      `json:"replay_notes,omitempty"`
}

// ScenarioControls are the run controls that must equal a run's recorded
// configuration for the artifact to apply. Duration and ramp-up are recorded
// per level but are not matched (see ADR 0154).
type ScenarioControls struct {
	Mode                  string   `json:"mode"`
	Steps                 int      `json:"steps"`
	MaxRPS                int      `json:"max_rps"`
	RequestTimeout        string   `json:"request_timeout"`
	ConnectionPolicy      string   `json:"connection_policy"`
	MaxConnectionsPerHost int      `json:"max_connections_per_host"`
	ThinkTime             string   `json:"think_time"`
	ThinkTimeRangeMin     string   `json:"think_time_range_min,omitempty"`
	ThinkTimeRangeMax     string   `json:"think_time_range_max,omitempty"`
	ThinkTimeSamples      []string `json:"think_time_samples,omitempty"`
	WorkloadSeed          uint64   `json:"workload_seed,omitempty"`
	PerVUCookieSessions   bool     `json:"per_vu_cookie_sessions"`
}

// ScenarioLevel records client and independent mirror-ledger evidence for
// one calibrated level. It contains no request or response content.
type ScenarioLevel struct {
	Users                   int      `json:"users"`
	ArrivalRate             int      `json:"arrival_rate,omitempty"`
	WorkloadFingerprint     string   `json:"workload_fingerprint"`
	Duration                string   `json:"duration"`
	RampUp                  string   `json:"ramp_up,omitempty"`
	Status                  string   `json:"status"`
	FailedChecks            []string `json:"failed_checks"`
	Outcome                 string   `json:"outcome"`
	ClientRequests          int64    `json:"client_requests"`
	ClientSuccess           int64    `json:"client_success"`
	ClientFailures          int64    `json:"client_failures"`
	ClientCancelled         int64    `json:"client_cancelled"`
	ClientResponseBodyBytes int64    `json:"client_response_body_bytes"`
	ServerReceived          int64    `json:"server_received"`
	ServerCompleted         int64    `json:"server_completed"`
	ServerResponseBodyBytes int64    `json:"server_response_body_bytes"`
	ServerRequestBodyBytes  int64    `json:"server_request_body_bytes"`
	ServerCookieRequests    int64    `json:"server_cookie_requests"`
	IterationsStarted       int64    `json:"iterations_started"`
	IterationsCompleted     int64    `json:"iterations_completed"`
	ArrivalsScheduled       int64    `json:"arrivals_scheduled"`
	ArrivalsDropped         int64    `json:"arrivals_dropped"`
	SchedulerLagP95MS       float64  `json:"scheduler_lag_p95_ms"`
	SchedulerLagMaxMS       float64  `json:"scheduler_lag_max_ms"`
	ClientP50MS             float64  `json:"client_p50_ms"`
	ClientP95MS             float64  `json:"client_p95_ms"`
	OverheadP50MS           float64  `json:"overhead_p50_ms"`
	OverheadP95MS           float64  `json:"overhead_p95_ms"`
	ThinkTimeObservations   int      `json:"think_time_observations"`
	MeanThinkTimeMS         float64  `json:"mean_think_time_ms"`
	DeliveryRatio           float64  `json:"delivery_ratio"`
	PeakProcessCPUPercent   float64  `json:"peak_process_cpu_percent"`
	PeakProcessRSSBytes     uint64   `json:"peak_process_rss_bytes"`
	GuardrailTriggered      bool     `json:"guardrail_triggered"`
}

// ScenarioRecord is the versioned workload self-calibration artifact.
type ScenarioRecord struct {
	SchemaVersion  int                        `json:"schema_version"`
	Kind           string                     `json:"kind"`
	Status         string                     `json:"status"`
	StartedAt      string                     `json:"started_at"`
	FinishedAt     string                     `json:"finished_at"`
	NetworkMode    string                     `json:"network_mode"`
	Generator      ScenarioGenerator          `json:"generator"`
	Mirror         MirrorModel                `json:"mirror"`
	Controls       ScenarioControls           `json:"controls"`
	Guardrails     generatorhealth.Guardrails `json:"generator_guardrails"`
	Levels         []ScenarioLevel            `json:"levels"`
	Interpretation string                     `json:"interpretation"`
}

// ScenarioInterpretation is the fixed scope statement embedded in every
// self-calibration artifact.
const ScenarioInterpretation = "Generator-side evidence only: this host and executable delivered the declared workload shape to a loopback mirror with a matching independent ledger and bounded overhead. It does not identify the target, verify application behavior, or establish workload representativeness or target capacity."

// IsScenarioCalibrationArtifact reports whether bounded JSON bytes declare
// the workload self-calibration kind. It does not validate the artifact.
func IsScenarioCalibrationArtifact(b []byte) bool {
	var probe struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Kind == ScenarioCalibrationKind
}

// ReadCalibrationBytes reads a bounded calibration artifact of either kind.
func ReadCalibrationBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("calibration artifact is unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxArtifactBytes+1))
	if err != nil || len(b) > maxArtifactBytes {
		return nil, errors.New("calibration artifact exceeds the read limit or cannot be read")
	}
	return b, nil
}

// LoadScenarioRecord reads and validates a passing self-calibration artifact.
func LoadScenarioRecord(path string) (ScenarioRecord, error) {
	b, err := ReadCalibrationBytes(path)
	if err != nil {
		return ScenarioRecord{}, err
	}
	return ParseScenarioRecord(b, true)
}

// ParseScenarioRecord decodes strictly and validates a self-calibration
// artifact. Callers attaching evidence to a run must set requirePassing.
func ParseScenarioRecord(b []byte, requirePassing bool) (ScenarioRecord, error) {
	if len(b) > maxArtifactBytes {
		return ScenarioRecord{}, errors.New("self-calibration artifact exceeds the read limit")
	}
	if err := jsonartifact.RejectDuplicateKeys(b); err != nil {
		return ScenarioRecord{}, errors.New("self-calibration artifact contains ambiguous JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	var record ScenarioRecord
	if err := decoder.Decode(&record); err != nil {
		return ScenarioRecord{}, errors.New("self-calibration artifact is not valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ScenarioRecord{}, errors.New("self-calibration artifact contains trailing data")
	}
	if err := ValidateScenarioRecord(record); err != nil {
		return ScenarioRecord{}, err
	}
	if requirePassing && record.Status != ScenarioCalibrationPassed {
		return ScenarioRecord{}, errors.New("self-calibration artifact did not pass its generator checks")
	}
	return record, nil
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	return true
}

// ValidateScenarioRecord checks structure, ledger reconciliation and the
// consistency of every recorded verdict. It never contacts any host.
func ValidateScenarioRecord(r ScenarioRecord) error {
	bad := func(reason string) error { return fmt.Errorf("self-calibration artifact is invalid: %s", reason) }
	if r.SchemaVersion != ScenarioCalibrationSchemaVersion || r.Kind != ScenarioCalibrationKind {
		return bad("unsupported schema or kind")
	}
	if r.NetworkMode != ScenarioCalibrationNetworkMode || r.Interpretation != ScenarioInterpretation {
		return bad("network mode or interpretation is not the fixed loopback scope")
	}
	started, err1 := time.Parse(time.RFC3339Nano, r.StartedAt)
	finished, err2 := time.Parse(time.RFC3339Nano, r.FinishedAt)
	if err1 != nil || err2 != nil || finished.Before(started) || finished.Sub(started) > ScenarioCalibrationMaxWallTime+time.Minute {
		return bad("timestamps are missing, reversed, or exceed the wall-time cap")
	}
	g := r.Generator
	if !safeLabelPattern.MatchString(g.OS) || !safeLabelPattern.MatchString(g.Architecture) || !safeLabelPattern.MatchString(g.GoVersion) ||
		g.LogicalCPUs < 1 || g.GOMAXPROCS < 1 || !finite(g.CPUCapacityCores) || g.CPUCapacitySource == "" {
		return bad("generator identity is incomplete")
	}
	switch g.RunnerIdentityMethod {
	case "git_commit", "executable_sha256", "module_version":
		if !safeLabelPattern.MatchString(g.RunnerIdentity) {
			return bad("runner identity is malformed")
		}
	default:
		return bad("runner identity is unavailable")
	}
	if r.Mirror.Source != "declared" && r.Mirror.Source != "recorded_report" {
		return bad("mirror source is unknown")
	}
	if (r.Mirror.Source == "recorded_report") != scenarioFingerprintPattern.MatchString(r.Mirror.SourceSHA256) {
		return bad("mirror source digest is inconsistent")
	}
	if len(r.Mirror.Routes) == 0 || len(r.Mirror.Routes) > 64 {
		return bad("mirror routes are missing or unbounded")
	}
	for _, route := range r.Mirror.Routes {
		if route.Method == "" || route.PathPrefix == "" || route.PathPrefix[0] != '/' || route.Status < 100 || route.Status > 599 ||
			route.ResponseBytes < 0 || route.ResponseBytes > MaxMirrorResponseBytes || !finite(route.DelayMS) || route.DelayMS > MaxMirrorDelayMS {
			return bad("mirror route is out of bounds")
		}
	}
	c := r.Controls
	if (c.Mode != "closed" && c.Mode != "arrival") || c.Steps < 1 || c.MaxRPS < 1 || c.RequestTimeout == "" {
		return bad("controls are incomplete")
	}
	if !r.Guardrails.Validate() || !r.Guardrails.Enabled() {
		return bad("generator guardrails are missing or invalid")
	}
	if len(r.Levels) == 0 || len(r.Levels) > ScenarioCalibrationMaxLevels {
		return bad("level count is out of bounds")
	}
	seen := map[[2]int]bool{}
	allPassed := true
	for _, l := range r.Levels {
		key := [2]int{l.Users, l.ArrivalRate}
		if seen[key] || l.Users < 1 || (c.Mode == "arrival") != (l.ArrivalRate > 0) || !scenarioFingerprintPattern.MatchString(l.WorkloadFingerprint) {
			return bad("level identity is duplicated or malformed")
		}
		seen[key] = true
		if _, err := time.ParseDuration(l.Duration); err != nil {
			return bad("level duration is malformed")
		}
		if l.FailedChecks == nil || (l.Status == ScenarioCalibrationPassed) != (len(l.FailedChecks) == 0) ||
			(l.Status != ScenarioCalibrationPassed && l.Status != ScenarioCalibrationFailed) {
			return bad("level verdict is inconsistent with its checks")
		}
		if !finite(l.SchedulerLagP95MS, l.SchedulerLagMaxMS, l.ClientP50MS, l.ClientP95MS, l.MeanThinkTimeMS, l.DeliveryRatio, l.PeakProcessCPUPercent) ||
			math.IsNaN(l.OverheadP50MS) || math.IsNaN(l.OverheadP95MS) || math.IsInf(l.OverheadP50MS, 0) || math.IsInf(l.OverheadP95MS, 0) ||
			l.ClientRequests < 0 || l.ClientSuccess < 0 || l.ClientFailures < 0 || l.ClientCancelled < 0 || l.ServerReceived < 0 || l.ServerCompleted < 0 {
			return bad("level counters are malformed")
		}
		if l.Status == ScenarioCalibrationPassed {
			if failures := EvaluateScenarioLevel(c, l, r.Guardrails); len(failures) != 0 {
				return bad("a passing level does not satisfy its recorded checks")
			}
		} else {
			allPassed = false
		}
	}
	if (r.Status == ScenarioCalibrationPassed) != allPassed || (r.Status != ScenarioCalibrationPassed && r.Status != ScenarioCalibrationFailed) {
		return bad("artifact verdict is inconsistent with its levels")
	}
	return nil
}

// Bounds on mirror responses.
const (
	MaxMirrorResponseBytes = 1 << 20
	MaxMirrorDelayMS       = 10000.0
)

// EvaluateScenarioLevel recomputes every generator-delivery check from the
// recorded level evidence. It returns the names of failed checks.
func EvaluateScenarioLevel(c ScenarioControls, l ScenarioLevel, guardrails generatorhealth.Guardrails) []string {
	failed := []string{}
	fail := func(name string) { failed = append(failed, name) }
	if l.Outcome == "ABORTED" || l.ClientRequests < 1 {
		fail("run_completed")
	}
	if l.ClientFailures != 0 || l.ClientSuccess+l.ClientCancelled != l.ClientRequests || l.ClientCancelled > int64(l.Users) {
		fail("client_outcomes_clean")
	}
	if l.ServerReceived != l.ClientRequests || l.ServerCompleted < l.ClientSuccess || l.ServerCompleted > l.ServerReceived {
		fail("mirror_ledger_reconciles")
	}
	if l.ClientResponseBodyBytes > l.ServerResponseBodyBytes {
		fail("response_bytes_reconcile")
	}
	if c.PerVUCookieSessions && l.ServerCookieRequests != l.ServerReceived {
		fail("per_vu_cookies_delivered")
	}
	if l.IterationsCompleted > l.IterationsStarted || l.IterationsStarted < 1 {
		fail("journey_conservation")
	}
	if c.Mode == "arrival" {
		if l.ArrivalsDropped != 0 || l.IterationsStarted != l.ArrivalsScheduled || l.ArrivalsScheduled < 1 {
			fail("arrival_conservation")
		}
		if l.SchedulerLagP95MS > ReferenceSoakSchedulerLagP95LimitMS || l.SchedulerLagMaxMS >= ReferenceSoakSchedulerLagMaxLimitMS {
			fail("scheduler_lag_bounded")
		}
	} else if l.DeliveryRatio < ScenarioMinimumDeliveryRatio {
		fail("closed_loop_delivery")
	}
	if l.OverheadP50MS > ScenarioOverheadP50LimitMS || l.OverheadP95MS > ScenarioOverheadP95LimitMS {
		fail("generator_overhead_bounded")
	}
	if declared, ok := declaredMeanThinkMS(c); ok && l.ThinkTimeObservations > 0 &&
		l.MeanThinkTimeMS > declared*(1+ScenarioThinkTimeRelativeTolerance)+ScenarioThinkTimeAbsoluteToleranceMS {
		fail("think_time_accuracy")
	}
	if l.GuardrailTriggered || (guardrails.MaxProcessCPUPercent > 0 && l.PeakProcessCPUPercent > guardrails.MaxProcessCPUPercent) ||
		(guardrails.MaxProcessRSSBytes > 0 && l.PeakProcessRSSBytes > guardrails.MaxProcessRSSBytes) || l.PeakProcessRSSBytes == 0 {
		fail("generator_headroom")
	}
	return failed
}

func declaredMeanThinkMS(c ScenarioControls) (float64, bool) {
	ms := func(s string) (float64, bool) {
		d, err := time.ParseDuration(s)
		return float64(d) / float64(time.Millisecond), err == nil
	}
	switch {
	case len(c.ThinkTimeSamples) > 0:
		total := 0.0
		for _, s := range c.ThinkTimeSamples {
			v, ok := ms(s)
			if !ok {
				return 0, false
			}
			total += v
		}
		return total / float64(len(c.ThinkTimeSamples)), true
	case c.ThinkTimeRangeMax != "":
		lo, ok1 := ms(c.ThinkTimeRangeMin)
		hi, ok2 := ms(c.ThinkTimeRangeMax)
		return (lo + hi) / 2, ok1 && ok2
	case c.ThinkTime != "":
		return ms(c.ThinkTime)
	}
	return 0, false
}

// AssessScenarioSelfCalibration is the ExtendedCoverageAssessor for workload
// self-calibration artifacts (ADR 0154). It is READY only when the exact
// planned level is a passed level whose workload fingerprint matches, the
// artifact is fresh, and the current executable identity and CPU
// entitlement equal the calibrated ones.
func AssessScenarioSelfCalibration(s config.Scenario, rec Record, local Runtime, now time.Time) ExtendedCoverage {
	blocked := func(reason string) ExtendedCoverage { return ExtendedCoverage{Status: "BLOCKED", Reason: reason} }
	sc := rec.SelfCalibration
	if sc == nil {
		return ExtendedCoverage{Status: "BLOCKED"}
	}
	if ValidateScenarioRecord(*sc) != nil || sc.Status != ScenarioCalibrationPassed {
		return blocked("Workload self-calibration is invalid or did not pass.")
	}
	finished, err := time.Parse(time.RFC3339Nano, sc.FinishedAt)
	if err != nil || now.Before(finished.Add(-5*time.Minute)) || now.Sub(finished) > ScenarioCalibrationMaxAge {
		return blocked("Workload self-calibration is future-dated or older than the 24-hour review threshold.")
	}
	g := sc.Generator
	method, identity := SourceIdentity()
	if method != g.RunnerIdentityMethod || identity != g.RunnerIdentity || local.OS != g.OS || local.Arch != g.Architecture ||
		local.GoVersion != g.GoVersion {
		return blocked("Workload self-calibration was produced by a different generator executable or runtime.")
	}
	if !local.CPUCapacityKnown || !g.CPUCapacityKnown || local.CPUCapacityCores != g.CPUCapacityCores || local.CPUCapacitySource != g.CPUCapacitySource ||
		local.GOMAXPROCS != g.GOMAXPROCS || local.LogicalCPUs != g.LogicalCPUs {
		return blocked("Workload self-calibration was produced under a different or unknown CPU entitlement.")
	}
	mode := s.Mode
	if mode == "" {
		mode = "closed"
	}
	if mode != sc.Controls.Mode {
		return blocked("Workload self-calibration mode differs from the planned run.")
	}
	for _, level := range sc.Levels {
		if level.Users != s.Users || (mode == "arrival" && level.ArrivalRate != s.ArrivalRate) {
			continue
		}
		if level.Status != ScenarioCalibrationPassed || rec.SelfCalibrationFingerprint == "" || level.WorkloadFingerprint != rec.SelfCalibrationFingerprint {
			return blocked("The self-calibrated level did not pass or its workload fingerprint differs.")
		}
		return ExtendedCoverage{Status: "READY", Reason: "Exact passed workload self-calibration level with matching fingerprint (generator-side evidence only)."}
	}
	return blocked("The exact planned level was not self-calibrated.")
}
