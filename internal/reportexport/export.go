// Package reportexport creates a deliberately reduced, privacy-oriented view
// of saved evidence. The exported schema is descriptive and cannot be used as
// a native Damocline assessment artifact.
package reportexport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const Format = "damocline-redacted-evidence-v1"

type Evidence struct {
	Format   string   `json:"format"`
	Kind     string   `json:"kind"`
	Outcome  string   `json:"outcome"`
	Workload Workload `json:"workload"`
	Trials   []Trial  `json:"trials,omitempty"`
	Probes   []Probe  `json:"probes,omitempty"`
	Run      *Run     `json:"run,omitempty"`
}

type Workload struct {
	Mode               string  `json:"mode,omitempty"`
	VirtualUsers       int     `json:"virtual_users,omitempty"`
	JourneyStartsPS    int     `json:"scheduled_journey_starts_per_second,omitempty"`
	WorkerLimit        int     `json:"journey_worker_limit,omitempty"`
	RequestCeiling     int     `json:"max_request_starts_per_second,omitempty"`
	P95LimitMS         float64 `json:"p95_limit_ms,omitempty"`
	MaxErrorRate       float64 `json:"max_error_rate,omitempty"`
	Observation        string  `json:"observation_duration,omitempty"`
	Warmup             string  `json:"warmup_duration,omitempty"`
	SpikeDuration      string  `json:"spike_duration,omitempty"`
	Recovery           string  `json:"recovery_duration,omitempty"`
	Soak               string  `json:"soak_duration,omitempty"`
	Cooldown           string  `json:"cooldown_duration,omitempty"`
	Repeats            int     `json:"repeats,omitempty"`
	WindowDuration     string  `json:"observation_window,omitempty"`
	ConnectionPolicy   string  `json:"connection_policy,omitempty"`
	LoadUnit           string  `json:"load_unit,omitempty"`
	MinimumLoad        int     `json:"minimum_tested_load,omitempty"`
	MaximumLoad        int     `json:"maximum_tested_load,omitempty"`
	TargetLoad         int     `json:"exact_target_load,omitempty"`
	TargetTested       bool    `json:"exact_target_tested,omitempty"`
	HighestPassingLoad *int    `json:"highest_passing_tested_load,omitempty"`
	FirstFailingLoad   *int    `json:"first_failing_tested_load,omitempty"`
}

type Probe struct {
	TestedLoad int    `json:"tested_load"`
	Status     string `json:"status"`
	Runs       []Run  `json:"runs"`
}

type Trial struct {
	Number  int     `json:"number"`
	Outcome string  `json:"outcome"`
	Phases  []Phase `json:"phases"`
}

type Phase struct {
	Name          string `json:"phase"`
	EvidenceState string `json:"evidence_state,omitempty"`
	Stability     string `json:"stability,omitempty"`
	Run           Run    `json:"run"`
}

type Run struct {
	Outcome            string      `json:"outcome"`
	ElapsedSeconds     float64     `json:"elapsed_seconds"`
	ObservationSeconds float64     `json:"observation_seconds,omitempty"`
	Mode               string      `json:"mode"`
	VirtualUsers       int         `json:"virtual_users"`
	JourneyStartsPS    int         `json:"scheduled_journey_starts_per_second,omitempty"`
	RequestCeiling     int         `json:"max_request_starts_per_second"`
	P95LimitMS         float64     `json:"p95_limit_ms"`
	MaxErrorRate       float64     `json:"max_error_rate"`
	ConnectionPolicy   string      `json:"connection_policy,omitempty"`
	Metrics            Metrics     `json:"requests"`
	Journeys           Metrics     `json:"completed_journeys"`
	Steps              []Step      `json:"steps,omitempty"`
	Profiles           []Profile   `json:"workflows,omitempty"`
	Windows            []Window    `json:"windows,omitempty"`
	Arrivals           *Arrivals   `json:"arrival_accounting,omitempty"`
	Scheduler          *Scheduler  `json:"scheduler,omitempty"`
	Generator          *Generator  `json:"generator,omitempty"`
	ApplicationMetrics *AppMetrics `json:"application_metrics,omitempty"`
	MeasurementQuality string      `json:"measurement_quality,omitempty"`
}

type Metrics struct {
	Total             int     `json:"total"`
	Success           int     `json:"success"`
	Failures          int     `json:"failures"`
	Cancelled         int     `json:"cancelled"`
	P50MS             float64 `json:"p50_ms"`
	MeanMS            float64 `json:"mean_ms"`
	P95MS             float64 `json:"p95_ms"`
	P99MS             float64 `json:"p99_ms"`
	Status1xx         int     `json:"status_1xx"`
	Status2xx         int     `json:"status_2xx"`
	Status3xx         int     `json:"status_3xx"`
	Status4xx         int     `json:"status_4xx"`
	Status5xx         int     `json:"status_5xx"`
	Timeouts          int     `json:"timeouts"`
	DNSFailures       int     `json:"dns_failures"`
	ConnectFailures   int     `json:"connection_failures"`
	TLSFailures       int     `json:"tls_failures"`
	TransportFailures int     `json:"transport_failures"`
}

type Step struct {
	ID      string  `json:"id"`
	Method  string  `json:"method"`
	Cleanup bool    `json:"cleanup,omitempty"`
	Metrics Metrics `json:"metrics"`
}

type Profile struct {
	ID       string  `json:"id"`
	Weight   int     `json:"weight"`
	Journeys Metrics `json:"journeys"`
}

type NamedMetrics struct {
	ID      string  `json:"id"`
	Metrics Metrics `json:"metrics"`
}

type Window struct {
	StartSeconds float64        `json:"start_seconds"`
	EndSeconds   float64        `json:"end_seconds"`
	Requests     Metrics        `json:"requests"`
	Journeys     Metrics        `json:"completed_journeys"`
	Steps        []NamedMetrics `json:"steps,omitempty"`
	Profiles     []NamedMetrics `json:"workflows,omitempty"`
}

type Arrivals struct {
	Scheduled                int64  `json:"scheduled"`
	Admitted                 *int64 `json:"admitted,omitempty"`
	Started                  int64  `json:"started"`
	Completed                int64  `json:"completed"`
	Dropped                  int64  `json:"dropped"`
	DroppedBusy              int64  `json:"dropped_busy"`
	DroppedLate              int64  `json:"dropped_late"`
	CancelledBeforeAdmission *int64 `json:"cancelled_before_admission,omitempty"`
}

type Scheduler struct {
	Samples int64   `json:"lag_samples"`
	P50MS   float64 `json:"lag_p50_ms"`
	P95MS   float64 `json:"lag_p95_ms"`
	MaxMS   float64 `json:"lag_max_ms"`
}

type Generator struct {
	Samples          int     `json:"samples"`
	ObservedSeconds  float64 `json:"observed_seconds"`
	GoCPUUtilization float64 `json:"go_cpu_utilization_ratio"`
	MeanProcessCPU   float64 `json:"mean_process_cpu_utilization_ratio"`
	PeakProcessCPU   float64 `json:"peak_process_cpu_utilization_ratio"`
	MaxSampleGapMS   float64 `json:"max_sample_gap_ms"`
}

type AppMetrics struct {
	Interval          string            `json:"interval"`
	FailedCollections int               `json:"failed_collections"`
	Samples           []AppMetricSample `json:"samples"`
}

type AppMetricSample struct {
	ElapsedMS   float64 `json:"elapsed_ms"`
	DurationMS  float64 `json:"duration_ms"`
	MetricCount int     `json:"metric_count"`
}

func FromRun(r report.Result) Evidence {
	return Evidence{Format: Format, Kind: "run", Outcome: safeOutcome(r.Outcome), Workload: Workload{
		Mode: safeMode(r.Config.Mode), VirtualUsers: r.Config.Users, JourneyStartsPS: r.Config.ArrivalRate,
		RequestCeiling: r.Config.MaxRPS, P95LimitMS: r.Config.P95LimitMS, MaxErrorRate: r.Config.MaxErrorRate,
		Observation: safeDuration(r.Config.Duration), WindowDuration: safeDuration(r.Config.WindowDuration), ConnectionPolicy: safeConnectionPolicy(r.Config.ConnectionPolicy),
		LoadUnit: safeLoadUnit(r.Config.Mode),
	}, Run: redactRun(r)}
}

func FromClosedExplorer(e explorer.Result) Evidence {
	out := Evidence{Format: Format, Kind: "closed_loop_exploration", Outcome: safeOutcome(e.Outcome), Workload: Workload{
		Mode: "closed", LoadUnit: "virtual_users", MinimumLoad: e.MinUsers, MaximumLoad: e.MaxUsers,
		Observation: safeDuration(e.StageDuration), Repeats: e.Repeats,
		HighestPassingLoad: cloneInt(e.HighestPassingUsers), FirstFailingLoad: cloneInt(e.FirstFailingUsers),
	}}
	for _, probe := range e.Probes {
		item := Probe{TestedLoad: probe.Users, Status: safeOutcome(probe.Status), Runs: make([]Run, 0, len(probe.Runs))}
		for _, run := range probe.Runs {
			item.Runs = append(item.Runs, *redactRun(run))
		}
		out.Probes = append(out.Probes, item)
	}
	return out
}

func FromArrivalExplorer(e arrivalexplorer.Result) Evidence {
	out := Evidence{Format: Format, Kind: "open_arrival_exploration", Outcome: safeOutcome(e.Outcome), Workload: Workload{
		Mode: "arrival", LoadUnit: "scheduled_journey_starts_per_second", MinimumLoad: e.MinJourneysPerSecond,
		MaximumLoad: e.MaxJourneysPerSecond, TargetLoad: e.TargetJourneysPerSecond, WorkerLimit: e.WorkerLimit,
		TargetTested: e.TargetTested, Observation: safeDuration(e.StageDuration), Repeats: e.Repeats,
		HighestPassingLoad: cloneInt(e.HighestPassingJourneysPerSecond), FirstFailingLoad: cloneInt(e.FirstFailingJourneysPerSecond),
	}}
	for _, probe := range e.Probes {
		item := Probe{TestedLoad: probe.JourneysPerSecond, Status: safeOutcome(probe.Status), Runs: make([]Run, 0, len(probe.Runs))}
		for _, run := range probe.Runs {
			item.Runs = append(item.Runs, *redactRun(run))
		}
		out.Probes = append(out.Probes, item)
	}
	return out
}

func FromClosedCampaign(c campaign.Result) Evidence {
	e := Evidence{Format: Format, Kind: "closed_loop_campaign", Outcome: safeOutcome(c.Outcome), Workload: Workload{
		Mode: "closed", LoadUnit: "virtual_users", VirtualUsers: c.BaseUsers, Observation: safeDuration(c.Observation), Warmup: safeDuration(c.Warmup),
		SpikeDuration: safeDuration(c.SpikeDuration), Recovery: safeDuration(c.Recovery), Soak: safeDuration(c.Soak), Cooldown: safeDuration(c.Cooldown), Repeats: c.Repeats,
	}}
	e.Trials = make([]Trial, 0, len(c.Trials)+1)
	for _, trial := range c.Trials {
		e.Trials = append(e.Trials, redactClosedTrial(trial))
	}
	if c.PendingTrial != nil {
		e.Trials = append(e.Trials, redactClosedTrial(*c.PendingTrial))
	}
	return e
}

func FromArrivalCampaign(c arrivalcampaign.Result) Evidence {
	e := Evidence{Format: Format, Kind: "open_arrival_campaign", Outcome: safeOutcome(c.Outcome), Workload: Workload{
		Mode: "arrival", LoadUnit: "scheduled_journey_starts_per_second", JourneyStartsPS: c.BaseStartsPerSecond, WorkerLimit: c.WorkerLimit,
		RequestCeiling: c.RequestRateCeiling, P95LimitMS: c.P95LimitMS, MaxErrorRate: c.MaxErrorRate,
		Observation: safeDuration(c.Observation), Warmup: safeDuration(c.Warmup), SpikeDuration: safeDuration(c.SpikeDuration), Recovery: safeDuration(c.Recovery),
		Soak: safeDuration(c.Soak), Cooldown: safeDuration(c.Cooldown), Repeats: c.Repeats, WindowDuration: safeDuration(c.WindowDuration),
	}}
	e.Trials = make([]Trial, 0, len(c.Trials)+1)
	for _, trial := range c.Trials {
		e.Trials = append(e.Trials, redactArrivalTrial(trial))
	}
	if c.PendingTrial != nil {
		e.Trials = append(e.Trials, redactArrivalTrial(*c.PendingTrial))
	}
	return e
}

func Marshal(e Evidence) ([]byte, error) {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode redacted evidence: %w", err)
	}
	return append(data, '\n'), nil
}

func redactClosedTrial(t campaign.Trial) Trial {
	out := Trial{Number: t.Number, Outcome: safeOutcome(t.Outcome), Phases: make([]Phase, 0, len(t.Phases))}
	for _, p := range t.Phases {
		out.Phases = append(out.Phases, Phase{Name: safePhase(p.Name), EvidenceState: safeEvidenceState(p.EvidenceState), Stability: safeStability(p.Stability), Run: *redactRun(p.Run)})
	}
	return out
}

func redactArrivalTrial(t arrivalcampaign.Trial) Trial {
	out := Trial{Number: t.Number, Outcome: safeOutcome(t.Outcome), Phases: make([]Phase, 0, len(t.Phases))}
	for _, p := range t.Phases {
		out.Phases = append(out.Phases, Phase{Name: safePhase(p.Name), EvidenceState: safeEvidenceState(p.EvidenceState), Stability: safeStability(p.Stability), Run: *redactRun(p.Run)})
	}
	return out
}

func redactRun(r report.Result) *Run {
	stepIDs, profileIDs := aliases(r)
	out := &Run{Outcome: safeOutcome(r.Outcome), ElapsedSeconds: r.ElapsedSeconds, ObservationSeconds: r.ObservationSeconds,
		Mode: safeMode(r.Config.Mode), VirtualUsers: r.Config.Users, JourneyStartsPS: r.Config.ArrivalRate,
		RequestCeiling: r.Config.MaxRPS, P95LimitMS: r.Config.P95LimitMS, MaxErrorRate: r.Config.MaxErrorRate,
		ConnectionPolicy: safeConnectionPolicy(r.Config.ConnectionPolicy), Metrics: safeMetrics(r.Metrics), Journeys: safeMetrics(r.Journeys)}
	for _, s := range r.Steps {
		key := stepKey(s.Profile, s.Name)
		out.Steps = append(out.Steps, Step{ID: stepIDs[key], Method: safeMethod(s.Method), Cleanup: s.Cleanup, Metrics: safeMetrics(s.Metrics)})
	}
	for _, p := range r.Profiles {
		out.Profiles = append(out.Profiles, Profile{ID: profileIDs[p.Name], Weight: p.Weight, Journeys: safeMetrics(p.Journeys)})
	}
	for _, w := range r.Windows {
		window := Window{StartSeconds: w.StartSeconds, EndSeconds: w.EndSeconds, Requests: safeMetrics(w.Metrics), Journeys: safeMetrics(w.Journeys)}
		for _, s := range w.Steps {
			window.Steps = append(window.Steps, NamedMetrics{ID: stepIDs[stepKey("", s.Name)], Metrics: safeMetrics(s.Metrics)})
		}
		for _, p := range w.Profiles {
			window.Profiles = append(window.Profiles, NamedMetrics{ID: profileIDs[p.Name], Metrics: safeMetrics(p.Metrics)})
		}
		out.Windows = append(out.Windows, window)
	}
	if r.Config.Mode == "arrival" || r.ArrivalsScheduled > 0 {
		out.Arrivals = &Arrivals{Scheduled: r.ArrivalsScheduled, Admitted: cloneInt64(r.ArrivalsAdmitted), Started: r.IterationsStarted,
			Completed: r.IterationsCompleted, Dropped: r.ArrivalsDropped, DroppedBusy: r.ArrivalsDroppedBusy, DroppedLate: r.ArrivalsDroppedLate,
			CancelledBeforeAdmission: cloneInt64(r.ArrivalsCancelledBeforeAdmission)}
	}
	if r.SchedulerLagSamples > 0 {
		out.Scheduler = &Scheduler{Samples: r.SchedulerLagSamples, P50MS: r.SchedulerLagP50MS, P95MS: r.SchedulerLagP95MS, MaxMS: r.SchedulerLagMaxMS}
	}
	if r.Generator != nil {
		out.Generator = &Generator{Samples: r.Generator.Samples, ObservedSeconds: r.Generator.ObservedSeconds,
			GoCPUUtilization: r.Generator.GoCPUUtilizationRatio, MeanProcessCPU: r.Generator.MeanProcessCPUUtilizationRatio,
			PeakProcessCPU: r.Generator.PeakProcessCPUUtilizationRatio, MaxSampleGapMS: r.Generator.MaxSampleGapMS}
	}
	if r.ApplicationMetrics != nil {
		out.ApplicationMetrics = redactAppMetrics(*r.ApplicationMetrics, r.StartedAt)
	}
	if r.Quality != nil {
		out.MeasurementQuality = safeQuality(r.Quality.State)
	}
	return out
}

func safeMetrics(m report.MetricSet) Metrics {
	return Metrics{Total: m.Total, Success: m.Success, Failures: m.Failures, Cancelled: m.Cancelled,
		P50MS: m.P50MS, MeanMS: m.MeanMS, P95MS: m.P95MS, P99MS: m.P99MS,
		Status1xx: m.HTTP.Status1xx, Status2xx: m.HTTP.Status2xx, Status3xx: m.HTTP.Status3xx,
		Status4xx: m.HTTP.Status4xx, Status5xx: m.HTTP.Status5xx, Timeouts: m.HTTP.NetworkTimeouts,
		DNSFailures: m.HTTP.DNSFailures, ConnectFailures: m.HTTP.ConnectionFailures, TLSFailures: m.HTTP.TLSFailures,
		TransportFailures: m.HTTP.TransportFailures}
}

func aliases(r report.Result) (map[string]string, map[string]string) {
	stepSet, profileSet := map[string]bool{}, map[string]bool{}
	for _, s := range r.Steps {
		stepSet[stepKey(s.Profile, s.Name)] = true
	}
	for _, p := range r.Config.Profiles {
		profileSet[p.Name] = true
	}
	for _, p := range r.Profiles {
		profileSet[p.Name] = true
	}
	for _, w := range r.Windows {
		for _, p := range w.Profiles {
			profileSet[p.Name] = true
		}
		for _, s := range w.Steps {
			stepSet[stepKey("", s.Name)] = true
		}
	}
	return makeAliases(stepSet, "step"), makeAliases(profileSet, "workflow")
}

func makeAliases(values map[string]bool, prefix string) map[string]string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]string, len(names))
	for i, name := range names {
		out[name] = fmt.Sprintf("%s_%03d", prefix, i+1)
	}
	return out
}

func stepKey(_ string, name string) string { return name }

func redactAppMetrics(t appmetrics.Timeline, runStartedAt string) *AppMetrics {
	start, err := time.Parse(time.RFC3339Nano, runStartedAt)
	if err != nil {
		return nil
	}
	out := &AppMetrics{Interval: safeDuration(t.Interval), FailedCollections: t.FailedCollections, Samples: make([]AppMetricSample, 0, len(t.Samples))}
	for _, scrape := range t.Samples {
		at, parseErr := time.Parse(time.RFC3339Nano, scrape.FinishedAt)
		if parseErr != nil {
			continue
		}
		sample := AppMetricSample{ElapsedMS: float64(at.Sub(start).Microseconds()) / 1000, DurationMS: scrape.DurationMS, MetricCount: len(scrape.Samples)}
		out.Samples = append(out.Samples, sample)
	}
	return out
}

func cloneInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

func safeDuration(value string) string {
	if value == "" {
		return ""
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 || d > 24*time.Hour {
		return ""
	}
	return d.String()
}

func safeOutcome(v string) string {
	switch v {
	case "PASS", "FAIL", "INCONCLUSIVE", "ABORTED", "IN_PROGRESS", "NOT_TESTED":
		return v
	default:
		return "UNKNOWN"
	}
}
func safeMode(v string) string {
	if v == "arrival" {
		return "arrival"
	}
	if v == "closed" {
		return "closed"
	}
	return "unknown"
}
func safeLoadUnit(mode string) string {
	if mode == "arrival" {
		return "scheduled_journey_starts_per_second"
	}
	if mode == "closed" {
		return "virtual_users"
	}
	return "unknown"
}
func safeConnectionPolicy(v string) string {
	if v == "reuse" || v == "new_per_request" {
		return v
	}
	return "unknown"
}
func safePhase(v string) string {
	switch v {
	case "warmup", "observation", "spike", "recovery", "soak":
		return v
	default:
		return "phase"
	}
}
func safeEvidenceState(v string) string {
	switch v {
	case "PASS_OBSERVED", "FAIL_OBSERVED", "INVALID_DELIVERY", "INSUFFICIENT_EVIDENCE", "ABORTED", "IN_PROGRESS":
		return v
	default:
		return "UNKNOWN"
	}
}
func safeStability(v string) string {
	switch v {
	case "NO_LARGE_DRIFT_DETECTED", "NON_STATIONARY", "NOT_ASSESSED", "INCOMPLETE_WINDOWS", "INSUFFICIENT_WINDOWS":
		return v
	default:
		if v == "" {
			return ""
		}
		return "UNKNOWN"
	}
}
func safeQuality(v string) string {
	switch v {
	case report.QualityValid, report.QualityLimited, report.QualityInvalid:
		return v
	default:
		return "UNKNOWN"
	}
}
func safeMethod(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	switch v {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return v
	default:
		return "OTHER"
	}
}
