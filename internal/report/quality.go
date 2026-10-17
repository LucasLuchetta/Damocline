package report

import "math"

// Quality describes whether arithmetic delivery/accounting evidence can be
// trusted. It is orthogonal to SLO Outcome. Positive calibration equivalence
// is not yet available, so this first slice intentionally never produces
// VALID_WITHIN_ENVELOPE for an arbitrary application run.
type Quality struct {
	State    string   `json:"state"`
	Findings []string `json:"findings"`
}

const (
	QualityValid   = "VALID_WITHIN_ENVELOPE"
	QualityLimited = "LIMITED"
	QualityInvalid = "INVALID"
)

// AssessQuality uses only aggregate invariants and reported generator
// observations. It does NOT infer network quality or target saturation.
// No secrets, hostnames, routes or individual response content are reported.
func AssessQuality(r Result) *Quality {
	q := &Quality{State: QualityLimited}
	invalidate := func(reason string) {
		q.State = QualityInvalid
		q.Findings = append(q.Findings, reason)
	}
	limit := func(reason string) { q.Findings = append(q.Findings, reason) }
	if r.Metrics.Total < 0 || r.Metrics.Success < 0 || r.Metrics.Failures < 0 ||
		r.Metrics.Cancelled < 0 || r.Metrics.Success > r.Metrics.Total || r.Metrics.Failures != r.Metrics.Total-r.Metrics.Success {
		invalidate("Request aggregate accounting is inconsistent.")
	}
	if r.Journeys.Total < 0 || r.Journeys.Success < 0 || r.Journeys.Failures < 0 ||
		r.Journeys.Cancelled < 0 || r.Journeys.Success > r.Journeys.Total || r.Journeys.Failures != r.Journeys.Total-r.Journeys.Success {
		invalidate("Completed-journey accounting is inconsistent.")
	}
	if r.IterationsStarted < 0 || r.IterationsCompleted < 0 ||
		r.IterationsCompleted > r.IterationsStarted || r.ArrivalsScheduled < 0 ||
		r.ArrivalsDropped < 0 || r.ArrivalsDroppedLate < 0 || r.ArrivalsDroppedBusy < 0 {
		invalidate("Negative or nonconservative iteration/arrival counters.")
	}
	if r.Config.Mode == "arrival" {
		if r.IterationsStarted > r.ArrivalsScheduled || r.ArrivalsDropped != r.ArrivalsScheduled-r.IterationsStarted ||
			r.ArrivalsDroppedLate > r.ArrivalsDropped || r.ArrivalsDroppedBusy != r.ArrivalsDropped-r.ArrivalsDroppedLate {
			invalidate("Scheduled, started and dropped arrivals do not reconcile.")
		}
	}
	if math.IsNaN(r.ObservationSeconds) || math.IsInf(r.ObservationSeconds, 0) || math.IsNaN(r.ElapsedSeconds) || math.IsInf(r.ElapsedSeconds, 0) || r.ObservationSeconds < 0 || r.ElapsedSeconds <= 0 {
		invalidate("Observation or elapsed time is invalid.")
	}
	if r.Metrics.Cancelled > 0 || r.Journeys.Cancelled > 0 || r.IterationsStarted > r.IterationsCompleted ||
		r.ArrivalsDropped > 0 || r.Outcome == "ABORTED" {
		limit("Work was cancelled or offered demand was not completely delivered.")
	}
	if r.Generator == nil || r.Generator.Samples < 2 || !r.Generator.GoCPUValid {
		limit("Generator runtime sampling is unavailable or inadequate.")
	} else if r.Generator.GoCPUUtilizationRatio >= 0.9 {
		limit("Generator runtime CPU may be saturated.")
	}
	if r.Generator == nil || r.Generator.ProcessSamples < 2 || !r.Generator.ProcessCPUValid {
		limit("Generator process measurements are missing or unsupported.")
	}
	if r.Config.MaxRPS > 0 && r.WindowSeconds() > 0 &&
		float64(r.Metrics.Total)/r.WindowSeconds() > 0.90*float64(r.Config.MaxRPS) {
		limit("Observed request throughput is near the configured client rate limit.")
	}
	if r.Config.Users < 1 || r.Config.MaxRPS < 1 || r.Config.P95LimitMS < 0 || math.IsNaN(r.Config.P95LimitMS) || math.IsInf(r.Config.P95LimitMS, 0) || r.Config.MaxErrorRate < 0 || r.Config.MaxErrorRate > 1 || math.IsNaN(r.Config.MaxErrorRate) || math.IsInf(r.Config.MaxErrorRate, 0) {
		invalidate("Required bounded actor/rate configuration is absent.")
	}
	if math.IsNaN(r.ErrorsRate) || math.IsInf(r.ErrorsRate, 0) || r.ErrorsRate < 0 || r.ErrorsRate > 1 ||
		math.IsNaN(r.SchedulerLagP50MS) || math.IsInf(r.SchedulerLagP50MS, 0) ||
		math.IsNaN(r.SchedulerLagP95MS) || math.IsInf(r.SchedulerLagP95MS, 0) ||
		math.IsNaN(r.SchedulerLagMaxMS) || math.IsInf(r.SchedulerLagMaxMS, 0) ||
		r.SchedulerLagP50MS < 0 || r.SchedulerLagP95MS < 0 || r.SchedulerLagMaxMS < 0 ||
		r.SchedulerLagP50MS > r.SchedulerLagP95MS || r.SchedulerLagP95MS > r.SchedulerLagMaxMS || r.SchedulerLagSamples < 0 {
		invalidate("Non-finite or inconsistent observed rate or scheduler lag.")
	}
	if r.Config.Mode == "arrival" {
		if r.SchedulerLagSamples > 0 && r.SchedulerLagSamples != r.ArrivalsScheduled {
			invalidate("Scheduler lag samples do not cover every planned arrival.")
		} else if r.ArrivalsScheduled > 0 && r.SchedulerLagSamples == 0 {
			limit("Scheduler lag sample count is absent; arrival timing coverage is unknown.")
		}
		if r.SchedulerLagSamples > 0 && r.SchedulerLagMethod == "" {
			limit("Scheduler lag timing method is absent.")
		}
	}
	for _, s := range r.Steps {
		if s.Metrics.Total < 0 || s.Metrics.Success < 0 || s.Metrics.Failures < 0 || s.Metrics.Cancelled < 0 || s.Metrics.Success > s.Metrics.Total || s.Metrics.Failures != s.Metrics.Total-s.Metrics.Success {
			invalidate("Named-step accounting is inconsistent (step identity intentionally omitted).")
			break
		}
	}
	// Check each independent ledger against the aggregate when its coverage is
	// complete. A missing ledger is LIMITED (not invented), while a present
	// contradictory ledger is INVALID. All subtraction avoids integer overflow.
	if len(r.Steps) > 0 {
		var total, success, failures, cancelled int
		for _, step := range r.Steps {
			m := step.Metrics
			if m.Total < 0 || m.Success < 0 || m.Failures < 0 || m.Cancelled < 0 ||
				m.Total > r.Metrics.Total-total || m.Success > r.Metrics.Success-success ||
				m.Failures > r.Metrics.Failures-failures || m.Cancelled > r.Metrics.Cancelled-cancelled {
				invalidate("Named-step ledger exceeds aggregate request counters.")
				break
			}
			total += m.Total
			success += m.Success
			failures += m.Failures
			cancelled += m.Cancelled
		}
		if total != r.Metrics.Total || success != r.Metrics.Success ||
			failures != r.Metrics.Failures || cancelled != r.Metrics.Cancelled {
			invalidate("Named-step ledger does not reconcile with aggregate requests.")
		}
	} else if r.Metrics.Total > 0 {
		limit("Named-step ledger is absent; per-step reconciliation is unavailable.")
	}
	if len(r.Profiles) > 0 {
		var total, success, failures, cancelled int
		for _, profile := range r.Profiles {
			m := profile.Journeys
			if m.Total < 0 || m.Success < 0 || m.Failures < 0 || m.Cancelled < 0 ||
				m.Total > r.Journeys.Total-total || m.Success > r.Journeys.Success-success ||
				m.Failures > r.Journeys.Failures-failures || m.Cancelled > r.Journeys.Cancelled-cancelled {
				invalidate("Profile journey ledger exceeds aggregate counters.")
				break
			}
			total += m.Total
			success += m.Success
			failures += m.Failures
			cancelled += m.Cancelled
		}
		if total != r.Journeys.Total || success != r.Journeys.Success ||
			failures != r.Journeys.Failures || cancelled != r.Journeys.Cancelled {
			invalidate("Profile journey ledger does not reconcile with aggregate journeys.")
		}
	}
	limit("No equivalent, fresh, host-validated generator preflight was attached; workload-specific capacity is not certified.")
	return q
}
