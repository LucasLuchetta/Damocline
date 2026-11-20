package report

import (
	"fmt"
	"math"
	"regexp"
)

// NetworkBaselineSchemaVersion is the first run schema that may carry a
// network-path baseline (ADR 0153).
const NetworkBaselineSchemaVersion = 37

const (
	NetworkBaselineMeasured = "MEASURED"
	NetworkBaselineSkipped  = "SKIPPED"
	// NetworkBaselineObjectiveShare: a baseline p95 above this share of the
	// configured request p95 objective leaves too little budget to attribute
	// a latency failure to the application.
	NetworkBaselineObjectiveShare = 0.5
	// NetworkBaselineDispersionRatio and NetworkBaselineDispersionShare
	// screen an unstable path: max/p50 above the ratio AND max above the
	// share of the p95 objective. The absolute floor keeps sub-millisecond
	// loopback jitter from tripping the ratio alone.
	NetworkBaselineDispersionRatio = 10.0
	NetworkBaselineDispersionShare = 0.25
	maxNetworkBaselineRequests     = 50
)

var networkBaselineLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// NetworkBaseline is a small sequential low-rate probe of one same-origin
// GET/HEAD path sent before load (ADR 0153). Latency is client-observed time
// to response headers and therefore includes the generator's network path,
// TLS/connection setup on the first request, and server processing for that
// path. It is evidence about the measurement path, not application capacity.
type NetworkBaseline struct {
	Status               string  `json:"status"`
	SkipReason           string  `json:"skip_reason,omitempty"`
	Phase                string  `json:"phase,omitempty"`
	Source               string  `json:"source,omitempty"`
	Method               string  `json:"method,omitempty"`
	Path                 string  `json:"path,omitempty"`
	Planned              int     `json:"planned"`
	Completed            int     `json:"completed"`
	Failures             int     `json:"failures"`
	NotAttempted         int     `json:"not_attempted"`
	UnexpectedHTTPStatus int     `json:"unexpected_http_status"`
	IntervalMS           float64 `json:"interval_ms,omitempty"`
	TimeoutMS            float64 `json:"request_timeout_ms,omitempty"`
	MinMS                float64 `json:"min_ms"`
	P50MS                float64 `json:"p50_ms"`
	P95MS                float64 `json:"p95_ms"`
	MaxMS                float64 `json:"max_ms"`
	MaxToP50Ratio        float64 `json:"max_to_p50_ratio"`
}

// ValidNetworkBaseline checks schema gating and internal consistency. It does
// not judge whether the baseline is acceptable; see NetworkBaselineIssue.
func ValidNetworkBaseline(r Result) bool {
	b := r.NetworkBaseline
	if b == nil {
		return true
	}
	if r.SchemaVersion < NetworkBaselineSchemaVersion {
		return false
	}
	numbers := []float64{b.IntervalMS, b.TimeoutMS, b.MinMS, b.P50MS, b.P95MS, b.MaxMS, b.MaxToP50Ratio}
	for _, n := range numbers {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return false
		}
	}
	switch b.Status {
	case NetworkBaselineSkipped:
		zero := NetworkBaseline{Status: b.Status, SkipReason: b.SkipReason}
		return networkBaselineLabel.MatchString(b.SkipReason) && *b == zero
	case NetworkBaselineMeasured:
	default:
		return false
	}
	if b.SkipReason != "" || b.Phase != "before_load" || (b.Source != "first_get_step" && b.Source != "baseline_path") ||
		(b.Method != "GET" && b.Method != "HEAD") || len(b.Path) == 0 || b.Path[0] != '/' ||
		b.Planned < 1 || b.Planned > maxNetworkBaselineRequests || b.Completed < 0 || b.Failures < 0 || b.NotAttempted < 0 ||
		b.Completed+b.Failures+b.NotAttempted != b.Planned || b.UnexpectedHTTPStatus < 0 || b.UnexpectedHTTPStatus > b.Completed ||
		b.IntervalMS <= 0 || b.TimeoutMS <= 0 {
		return false
	}
	if b.Completed == 0 {
		return b.MinMS == 0 && b.P50MS == 0 && b.P95MS == 0 && b.MaxMS == 0 && b.MaxToP50Ratio == 0
	}
	if b.MinMS > b.P50MS || b.P50MS > b.P95MS || b.P95MS > b.MaxMS {
		return false
	}
	if b.P50MS > 0 {
		return math.Abs(b.MaxToP50Ratio-math.Round(1000*b.MaxMS/b.P50MS)/1000) <= 0.001
	}
	return b.MaxToP50Ratio == 0
}

// NetworkBaselineInvalid reports a measured baseline in which no request
// completed: the measurement path itself did not work.
func NetworkBaselineInvalid(r Result) bool {
	b := r.NetworkBaseline
	return b != nil && b.Status == NetworkBaselineMeasured && b.Completed == 0
}

// NetworkBaselineIssue returns a non-empty explanation when the recorded
// baseline is failed, incomplete, unstable, or large relative to the request
// p95 objective. In that case a latency failure cannot be attributed to the
// application from this report.
func NetworkBaselineIssue(r Result) string {
	b := r.NetworkBaseline
	if b == nil || b.Status != NetworkBaselineMeasured {
		return ""
	}
	const caveat = " The generator network path may dominate measured latency; no application-capacity conclusion is supported by this run."
	if b.Completed == 0 {
		return fmt.Sprintf("Network-path baseline failed entirely: 0 of %d sequential baseline requests completed.%s", b.Planned, caveat)
	}
	if b.Failures > 0 || b.NotAttempted > 0 {
		return fmt.Sprintf("Network-path baseline was incomplete: %d of %d requests completed (%d failed, %d not attempted within the bounded budget).%s", b.Completed, b.Planned, b.Failures, b.NotAttempted, caveat)
	}
	limit := r.Config.P95LimitMS
	if limit > 0 && b.P95MS > NetworkBaselineObjectiveShare*limit {
		return fmt.Sprintf("Network-path baseline p95 %.1f ms exceeds %.0f%% of the %.0f ms request p95 objective before any load was applied.%s", b.P95MS, 100*NetworkBaselineObjectiveShare, limit, caveat)
	}
	if limit > 0 && b.MaxToP50Ratio > NetworkBaselineDispersionRatio && b.MaxMS > NetworkBaselineDispersionShare*limit {
		return fmt.Sprintf("Network-path baseline is unstable: max %.1f ms is %.1fx the p50 of %.1f ms and exceeds %.0f%% of the %.0f ms request p95 objective.%s", b.MaxMS, b.MaxToP50Ratio, b.P50MS, 100*NetworkBaselineDispersionShare, limit, caveat)
	}
	return ""
}
