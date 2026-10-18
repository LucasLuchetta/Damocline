// Package report summarizes measured traffic and emits redacted run artifacts.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

// CurrentSchemaVersion is the newest persisted HTTP run report schema emitted
// by the engine. Readers must reject future versions until their semantics are
// reviewed and supported. Version 20 adds DNS and connection failure details
// to HTTP diagnostics.
const CurrentSchemaVersion = 20

type MetricSet struct {
	Total                 int                     `json:"total"`
	Success               int                     `json:"success"`
	Failures              int                     `json:"failures"`
	Cancelled             int                     `json:"cancelled"`
	P50MS                 float64                 `json:"p50_ms"`
	MeanMS                float64                 `json:"mean_ms"`
	P95MS                 float64                 `json:"p95_ms"`
	P95IID95LowerMS       float64                 `json:"p95_iid_95_lower_ms,omitempty"`
	P95IID95UpperMS       float64                 `json:"p95_iid_95_upper_ms,omitempty"`
	P95IID95Approximation bool                    `json:"p95_iid_95_approximation,omitempty"`
	P99MS                 float64                 `json:"p99_ms"`
	Reasons               map[string]int          `json:"failure_reasons,omitempty"`
	HTTP                  HTTPDiagnostics         `json:"http_diagnostics,omitempty"`
	ResponseBody          ResponseBodyDiagnostics `json:"response_body,omitzero"`
	durations             []time.Duration
	totalDuration         time.Duration
}

// ResponseBodyDiagnostics summarizes body-read outcomes without preserving body data.
type ResponseBodyDiagnostics struct {
	CompleteObservations   int64 `json:"complete_observations"`
	IncompleteObservations int64 `json:"incomplete_observations"`
	OversizedObservations  int64 `json:"oversized_observations"`
	MinBytes               int64 `json:"min_bytes"`
	MaxBytes               int64 `json:"max_bytes"`
	TotalBytes             int64 `json:"total_bytes"`
}

// ResponseBodyState classifies one response body's bounded read outcome.
type ResponseBodyState string

const (
	ResponseBodyComplete   ResponseBodyState = "complete"
	ResponseBodyIncomplete ResponseBodyState = "incomplete"
	ResponseBodyOversized  ResponseBodyState = "oversized"
)

// RecordResponseBody adds one response body read outcome. Byte sizes are kept
// only for complete bodies within the configured response limit.
func (m *MetricSet) RecordResponseBody(state ResponseBodyState, size int64) {
	d := &m.ResponseBody
	switch state {
	case ResponseBodyComplete:
		if d.CompleteObservations == 0 || size < d.MinBytes {
			d.MinBytes = size
		}
		if size > d.MaxBytes {
			d.MaxBytes = size
		}
		d.CompleteObservations++
		d.TotalBytes += size
	case ResponseBodyIncomplete:
		d.IncompleteObservations++
	case ResponseBodyOversized:
		d.OversizedObservations++
	}
}

// HTTPDiagnostics counts observed HTTP responses and classified client-side
// outcomes. A zero status count is unknown/no response, not an HTTP status.
type HTTPDiagnostics struct {
	FailureClassificationVersion int         `json:"failure_classification_version,omitempty"`
	StatusCodes                  map[int]int `json:"status_codes,omitempty"`
	Status1xx                    int         `json:"status_1xx"`
	Status2xx                    int         `json:"status_2xx"`
	Status3xx                    int         `json:"status_3xx"`
	Status4xx                    int         `json:"status_4xx"`
	Status5xx                    int         `json:"status_5xx"`
	RedirectResponses            int         `json:"redirect_responses"`
	RateLimitedResponses         int         `json:"rate_limited_responses"`
	AssertionFailures            int         `json:"assertion_failures"`
	NetworkTimeouts              int         `json:"network_timeouts"`
	DNSFailures                  int         `json:"dns_failures"`
	ConnectionFailures           int         `json:"connection_failures"`
	TLSFailures                  int         `json:"tls_failures"`
	TransportFailures            int         `json:"transport_failures"`
	UnexpectedResponseSizes      int         `json:"unexpected_response_sizes"`
}

func (d *HTTPDiagnostics) Record(statusCode int, reason string, assertionFailure, tlsFailure bool) {
	d.FailureClassificationVersion = 1
	if statusCode >= 100 && statusCode <= 599 {
		if d.StatusCodes == nil {
			d.StatusCodes = make(map[int]int)
		}
		d.StatusCodes[statusCode]++
		switch statusCode / 100 {
		case 1:
			d.Status1xx++
		case 2:
			d.Status2xx++
		case 3:
			d.Status3xx++
			if statusCode == 300 || statusCode == 301 || statusCode == 302 || statusCode == 303 || statusCode == 305 || statusCode == 307 || statusCode == 308 {
				d.RedirectResponses++
			}
		case 4:
			d.Status4xx++
		case 5:
			d.Status5xx++
		}
		if statusCode == 429 {
			d.RateLimitedResponses++
		}
	}
	if assertionFailure {
		d.AssertionFailures++
	}
	if tlsFailure {
		d.TLSFailures++
	}
	switch reason {
	case "timeout":
		d.NetworkTimeouts++
	case "dns_failure":
		d.DNSFailures++
		d.TransportFailures++
	case "connection_failure":
		d.ConnectionFailures++
		d.TransportFailures++
	case "transport", "body_read":
		if !tlsFailure {
			d.TransportFailures++
		}
	case "response_too_large":
		d.UnexpectedResponseSizes++
	}
}

// HTTPTrace records client-observed transport phases. A zero count means the
// phase was not attempted (for example, DNS on a reused connection); it does
// not mean the phase took zero time.
type HTTPTrace struct {
	RequestAttemptCount    int     `json:"request_attempt_count"`
	WriteCount             int     `json:"request_write_count"`
	WriteMS                float64 `json:"request_write_ms,omitempty"`
	WriteFailedCount       int     `json:"request_write_failed_count,omitempty"`
	DNSStartCount          int     `json:"dns_start_count"`
	RateWaitCount          int     `json:"rate_wait_count"`
	RateWaitMS             float64 `json:"rate_wait_ms,omitempty"`
	RateWaitCancelledCount int     `json:"rate_wait_cancelled_count,omitempty"`
	DNSCount               int     `json:"dns_count"`
	DNSMS                  float64 `json:"dns_ms,omitempty"`
	ConnectCount           int     `json:"connect_count"`
	ConnectStartCount      int     `json:"connect_start_count"`
	ConnectMS              float64 `json:"connect_ms,omitempty"`
	TLSCount               int     `json:"tls_count"`
	TLSStartCount          int     `json:"tls_start_count"`
	TLSMS                  float64 `json:"tls_ms,omitempty"`
	FirstByteCount         int     `json:"first_byte_count"`
	FirstByteMS            float64 `json:"first_byte_ms,omitempty"`
	BodyCount              int     `json:"body_count"`
	BodyMS                 float64 `json:"body_ms,omitempty"`
	BodyIncompleteCount    int     `json:"body_incomplete_count,omitempty"`
	CompleteCount          int     `json:"complete_count"`
	CompleteMS             float64 `json:"complete_ms,omitempty"`
}

func (t *HTTPTrace) Record(o HTTPTrace) {
	t.RequestAttemptCount += o.RequestAttemptCount
	t.WriteCount += o.WriteCount
	t.WriteMS += o.WriteMS
	t.WriteFailedCount += o.WriteFailedCount
	t.DNSStartCount += o.DNSStartCount
	t.RateWaitCount += o.RateWaitCount
	t.RateWaitMS += o.RateWaitMS
	t.RateWaitCancelledCount += o.RateWaitCancelledCount
	t.DNSCount += o.DNSCount
	t.DNSMS += o.DNSMS
	t.ConnectCount += o.ConnectCount
	t.ConnectStartCount += o.ConnectStartCount
	t.ConnectMS += o.ConnectMS
	t.TLSCount += o.TLSCount
	t.TLSStartCount += o.TLSStartCount
	t.TLSMS += o.TLSMS
	t.FirstByteCount += o.FirstByteCount
	t.FirstByteMS += o.FirstByteMS
	t.BodyCount += o.BodyCount
	t.BodyMS += o.BodyMS
	t.BodyIncompleteCount += o.BodyIncompleteCount
	t.CompleteCount += o.CompleteCount
	t.CompleteMS += o.CompleteMS
}

func NewMetricSet() *MetricSet {
	return &MetricSet{
		Reasons: make(map[string]int),
		HTTP:    HTTPDiagnostics{FailureClassificationVersion: 1},
	}
}
func (m *MetricSet) Record(latency time.Duration, success, cancelled bool, reason string) {
	if cancelled {
		m.Cancelled++
		return
	}
	m.Total++
	if success {
		m.Success++
	} else {
		m.Failures++
		m.Reasons[reason]++
	}
	m.durations = append(m.durations, latency)
	m.totalDuration += latency
}

func (m *MetricSet) RecordHTTPOutcome(statusCode int, reason string, assertionFailure, tlsFailure bool) {
	m.HTTP.Record(statusCode, reason, assertionFailure, tlsFailure)
}
func (m *MetricSet) Snapshot() MetricSet {
	v := *m
	v.Reasons = make(map[string]int, len(m.Reasons))
	for k, n := range m.Reasons {
		v.Reasons[k] = n
	}
	v.HTTP.StatusCodes = make(map[int]int, len(m.HTTP.StatusCodes))
	for code, count := range m.HTTP.StatusCodes {
		v.HTTP.StatusCodes[code] = count
	}
	if len(m.durations) == 0 {
		v.durations = nil
		return v
	}
	d := append([]time.Duration(nil), m.durations...)
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	v.P50MS = roundMS(percentile(d, 0.50))
	v.MeanMS = roundMS(m.totalDuration / time.Duration(len(m.durations)))
	v.P95MS = roundMS(percentile(d, 0.95))
	if low, high, err := statistics.PercentileRankInterval95(len(d), 0.95); err == nil {
		v.P95IID95LowerMS = roundMS(d[low-1])
		v.P95IID95UpperMS = roundMS(d[high-1])
		v.P95IID95Approximation = true
	}
	v.P99MS = roundMS(percentile(d, 0.99))
	v.durations = nil
	return v
}
func percentile(values []time.Duration, p float64) time.Duration {
	n := len(values)
	if n == 0 {
		return 0
	}
	// nearest-rank percentile, using ceil(p*n)-1 without floating point edge handling.
	idx := int(float64(n) * p)
	if float64(idx) == float64(n)*p {
		idx--
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return values[idx]
}
func roundMS(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }

type RunConfig struct {
	WorkloadRevision    string          `json:"workload_revision,omitempty"`
	ApplicationRevision string          `json:"application_revision,omitempty"`
	WorkloadFingerprint string          `json:"workload_fingerprint,omitempty"`
	Environment         string          `json:"environment,omitempty"`
	ProfileMixBasis     string          `json:"profile_mix_basis,omitempty"`
	ProfileMixReference string          `json:"profile_mix_reference,omitempty"`
	Users               int             `json:"users"`
	Duration            string          `json:"duration"`
	RampUp              string          `json:"ramp_up,omitempty"`
	ThinkTime           string          `json:"think_time,omitempty"`
	RequestTimeout      string          `json:"request_timeout"`
	ConnectionPolicy    string          `json:"connection_policy,omitempty"`
	MaxRPS              int             `json:"max_rps"`
	P95LimitMS          float64         `json:"p95_limit_ms"`
	MaxErrorRate        float64         `json:"max_error_rate"`
	Mode                string          `json:"mode"`
	ArrivalRate         int             `json:"arrival_rate,omitempty"`
	SeparateSessions    bool            `json:"separate_sessions"`
	MinSamplesPerStep   int             `json:"min_samples_per_step"`
	Profiles            []ProfileConfig `json:"profiles,omitempty"`
	WindowDuration      string          `json:"window_duration,omitempty"`
}

// ProfileConfig records the intended workflow mix used for a run.
type ProfileConfig struct {
	Name      string `json:"name"`
	Weight    int    `json:"weight"`
	ThinkTime string `json:"think_time"`
}
type StepResult struct {
	Profile   string    `json:"profile,omitempty"`
	Name      string    `json:"name"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Metrics   MetricSet `json:"metrics"`
	HTTPTrace HTTPTrace `json:"http_trace"`
}

// ProfileResult records completed and interrupted journeys by workflow. Its
// actual share is based on starts, because failures and cancellation can alter
// the share of completed journeys.
type ProfileResult struct {
	Name                  string    `json:"name"`
	Weight                int       `json:"weight"`
	AssignedVUs           int       `json:"assigned_vus,omitempty"`
	Journeys              MetricSet `json:"journeys"`
	ObservedThinkTimeMS   float64   `json:"observed_think_time_ms,omitempty"`
	ThinkTimeObservations int       `json:"think_time_observations,omitempty"`
}
type WindowResult struct {
	StartSeconds float64        `json:"start_seconds"`
	EndSeconds   float64        `json:"end_seconds"`
	Metrics      MetricSet      `json:"metrics"`
	Steps        []WindowMetric `json:"steps,omitempty"`
	Profiles     []WindowMetric `json:"profiles,omitempty"`
}

// WindowMetric retains an independent named-step or workflow ledger inside
// one observation window. Names are already part of the sanitized run config.
type WindowMetric struct {
	Name    string    `json:"name"`
	Metrics MetricSet `json:"metrics"`
}
type Result struct {
	SchemaVersion                    int                          `json:"schema_version"`
	Name                             string                       `json:"name"`
	Target                           string                       `json:"target"`
	StartedAt                        string                       `json:"started_at"`
	ElapsedSeconds                   float64                      `json:"elapsed_seconds"`
	ObservationSeconds               float64                      `json:"observation_seconds,omitempty"`
	Config                           RunConfig                    `json:"config"`
	Metrics                          MetricSet                    `json:"metrics"`
	Journeys                         MetricSet                    `json:"journeys"`
	Steps                            []StepResult                 `json:"steps"`
	Profiles                         []ProfileResult              `json:"profiles,omitempty"`
	Windows                          []WindowResult               `json:"windows,omitempty"`
	Outcome                          string                       `json:"outcome"`
	ErrorsRate                       float64                      `json:"error_rate"`
	Note                             string                       `json:"note"`
	IterationsStarted                int64                        `json:"iterations_started"`
	IterationsCompleted              int64                        `json:"iterations_completed"`
	ArrivalsScheduled                int64                        `json:"arrivals_scheduled"`
	ArrivalsAdmitted                 *int64                       `json:"arrivals_admitted,omitempty"`
	ArrivalsDropped                  int64                        `json:"arrivals_dropped"`
	ArrivalsDroppedLate              int64                        `json:"arrivals_dropped_late,omitempty"`
	ArrivalsDroppedBusy              int64                        `json:"arrivals_dropped_busy,omitempty"`
	ArrivalsCancelledBeforeAdmission *int64                       `json:"arrivals_cancelled_before_admission,omitempty"`
	SchedulerLagSamples              int64                        `json:"scheduler_lag_samples,omitempty"`
	SchedulerLagP50MS                float64                      `json:"scheduler_lag_p50_ms,omitempty"`
	SchedulerLagP95MS                float64                      `json:"scheduler_lag_p95_ms,omitempty"`
	SchedulerLagMaxMS                float64                      `json:"scheduler_lag_max_ms,omitempty"`
	SchedulerLagMethod               string                       `json:"scheduler_lag_method,omitempty"`
	AverageInFlight                  float64                      `json:"average_in_flight_requests,omitempty"`
	LittleLawEstimate                float64                      `json:"little_law_in_flight_estimate,omitempty"`
	ObservedThinkTimeMS              float64                      `json:"observed_think_time_ms,omitempty"`
	ThinkTimeObservations            int                          `json:"think_time_observations,omitempty"`
	Warnings                         []string                     `json:"warnings,omitempty"`
	Generator                        *generatorhealth.Summary     `json:"generator,omitempty"`
	Calibration                      *CalibrationEvidence         `json:"calibration,omitempty"`
	ReferenceEvidence                *ControlledReferenceEvidence `json:"controlled_reference_evidence,omitempty"`
	Quality                          *Quality                     `json:"data_quality,omitempty"`
	Signals                          []Signal                     `json:"diagnostic_signals,omitempty"`
}

// CalibrationEvidence associates a run with sanitized local reference evidence.
// It carries no target URL, credential, route or response body values.
type CalibrationEvidence struct {
	Record   calibration.Record   `json:"record"`
	Coverage calibration.Coverage `json:"coverage"`
}

// ControlledReferenceEvidence records independent server counters collected
// by the calibrator for its own child-process loopback soak. It is not endpoint
// attestation for ordinary run commands or external applications.
type ControlledReferenceEvidence struct {
	Kind               string `json:"kind"`
	FixtureFingerprint string `json:"fixture_fingerprint"`
	ServerReceived     int64  `json:"server_received"`
	ServerCompleted    int64  `json:"server_completed"`
	ServerSuccess      int64  `json:"server_success"`
	ServerFailures     int64  `json:"server_failures"`
	ServerCancelled    int64  `json:"server_cancelled"`
	ServerRejected     int64  `json:"server_rejected"`
}

const maxReportBytes = 16 << 20

// WindowSeconds returns the configured workload window when the report records
// it, falling back to wall-clock elapsed time for older report schemas.
func (r Result) WindowSeconds() float64 {
	if r.ObservationSeconds > 0 {
		return r.ObservationSeconds
	}
	return r.ElapsedSeconds
}

func WriteJSON(path string, r Result) error { return WriteAnyJSON(path, r) }

// WriteAnyJSON safely writes redacted report values to a restricted local file.
func WriteAnyJSON(path string, r any) error {
	bytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if len(bytes) > maxReportBytes {
		return errors.New("report exceeds the 16 MiB output limit")
	}
	bytes = append(bytes, '\n')
	d := filepath.Dir(path)
	if err := os.MkdirAll(d, 0700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
