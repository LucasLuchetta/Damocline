// Package report summarizes measured traffic and emits redacted run artifacts.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

type MetricSet struct {
	Total                 int            `json:"total"`
	Success               int            `json:"success"`
	Failures              int            `json:"failures"`
	Cancelled             int            `json:"cancelled"`
	P50MS                 float64        `json:"p50_ms"`
	MeanMS                float64        `json:"mean_ms"`
	P95MS                 float64        `json:"p95_ms"`
	P95IID95LowerMS       float64        `json:"p95_iid_95_lower_ms,omitempty"`
	P95IID95UpperMS       float64        `json:"p95_iid_95_upper_ms,omitempty"`
	P95IID95Approximation bool           `json:"p95_iid_95_approximation,omitempty"`
	P99MS                 float64        `json:"p99_ms"`
	Reasons               map[string]int `json:"failure_reasons,omitempty"`
	durations             []time.Duration
	totalDuration         time.Duration
}

func NewMetricSet() *MetricSet { return &MetricSet{Reasons: make(map[string]int)} }
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
func (m *MetricSet) Snapshot() MetricSet {
	v := *m
	v.Reasons = make(map[string]int, len(m.Reasons))
	for k, n := range m.Reasons {
		v.Reasons[k] = n
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
	WorkloadRevision  string          `json:"workload_revision,omitempty"`
	Environment       string          `json:"environment,omitempty"`
	Users             int             `json:"users"`
	Duration          string          `json:"duration"`
	RampUp            string          `json:"ramp_up,omitempty"`
	ThinkTime         string          `json:"think_time,omitempty"`
	RequestTimeout    string          `json:"request_timeout"`
	MaxRPS            int             `json:"max_rps"`
	P95LimitMS        float64         `json:"p95_limit_ms"`
	MaxErrorRate      float64         `json:"max_error_rate"`
	Mode              string          `json:"mode"`
	ArrivalRate       int             `json:"arrival_rate,omitempty"`
	SeparateSessions  bool            `json:"separate_sessions"`
	MinSamplesPerStep int             `json:"min_samples_per_step"`
	Profiles          []ProfileConfig `json:"profiles,omitempty"`
	WindowDuration    string          `json:"window_duration,omitempty"`
}

// ProfileConfig records the intended workflow mix used for a run.
type ProfileConfig struct {
	Name      string `json:"name"`
	Weight    int    `json:"weight"`
	ThinkTime string `json:"think_time"`
}
type StepResult struct {
	Profile string    `json:"profile,omitempty"`
	Name    string    `json:"name"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Metrics MetricSet `json:"metrics"`
}

// ProfileResult records completed and interrupted journeys by workflow. Its
// actual share is based on starts, because failures and cancellation can alter
// the share of completed journeys.
type ProfileResult struct {
	Name        string    `json:"name"`
	Weight      int       `json:"weight"`
	AssignedVUs int       `json:"assigned_vus,omitempty"`
	Journeys    MetricSet `json:"journeys"`
}
type WindowResult struct {
	StartSeconds float64   `json:"start_seconds"`
	EndSeconds   float64   `json:"end_seconds"`
	Metrics      MetricSet `json:"metrics"`
}
type Result struct {
	SchemaVersion       int                      `json:"schema_version"`
	Name                string                   `json:"name"`
	Target              string                   `json:"target"`
	StartedAt           string                   `json:"started_at"`
	ElapsedSeconds      float64                  `json:"elapsed_seconds"`
	ObservationSeconds  float64                  `json:"observation_seconds,omitempty"`
	Config              RunConfig                `json:"config"`
	Metrics             MetricSet                `json:"metrics"`
	Journeys            MetricSet                `json:"journeys"`
	Steps               []StepResult             `json:"steps"`
	Profiles            []ProfileResult          `json:"profiles,omitempty"`
	Windows             []WindowResult           `json:"windows,omitempty"`
	Outcome             string                   `json:"outcome"`
	ErrorsRate          float64                  `json:"error_rate"`
	Note                string                   `json:"note"`
	IterationsStarted   int64                    `json:"iterations_started"`
	IterationsCompleted int64                    `json:"iterations_completed"`
	ArrivalsScheduled   int64                    `json:"arrivals_scheduled"`
	ArrivalsDropped     int64                    `json:"arrivals_dropped"`
	ArrivalsDroppedLate int64                    `json:"arrivals_dropped_late,omitempty"`
	ArrivalsDroppedBusy int64                    `json:"arrivals_dropped_busy,omitempty"`
	SchedulerLagP95MS   float64                  `json:"scheduler_lag_p95_ms,omitempty"`
	SchedulerLagMaxMS   float64                  `json:"scheduler_lag_max_ms,omitempty"`
	AverageInFlight     float64                  `json:"average_in_flight_requests,omitempty"`
	LittleLawEstimate   float64                  `json:"little_law_in_flight_estimate,omitempty"`
	Warnings            []string                 `json:"warnings,omitempty"`
	Generator           *generatorhealth.Summary `json:"generator,omitempty"`
}

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
	d := filepath.Dir(path)
	if err := os.MkdirAll(d, 0700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	bytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	bytes = append(bytes, '\n')
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
