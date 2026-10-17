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
	if math.IsNaN(r.ErrorsRate) || math.IsInf(r.ErrorsRate, 0) || r.ErrorsRate < 0 || r.ErrorsRate > 1 || math.IsNaN(r.SchedulerLagP95MS) || math.IsInf(r.SchedulerLagP95MS, 0) || math.IsNaN(r.SchedulerLagMaxMS) || math.IsInf(r.SchedulerLagMaxMS, 0) || r.SchedulerLagP95MS < 0 || r.SchedulerLagMaxMS < 0 || r.SchedulerLagP95MS > r.SchedulerLagMaxMS {
		invalidate("Non-finite or inconsistent observed rate or scheduler lag.")
	}
	if r.SchemaVersion >= 7 && (math.IsNaN(r.ObservedThinkTimeMS) || math.IsInf(r.ObservedThinkTimeMS, 0) || r.ObservedThinkTimeMS < 0 || r.ThinkTimeObservations < 0) {
		invalidate("Observed think-time totals or counts are invalid.")
	}
	for _, s := range r.Steps {
		if s.Metrics.Total < 0 || s.Metrics.Success < 0 || s.Metrics.Failures < 0 || s.Metrics.Cancelled < 0 || s.Metrics.Success > s.Metrics.Total || s.Metrics.Failures != s.Metrics.Total-s.Metrics.Success {
			invalidate("Named-step accounting is inconsistent (step identity intentionally omitted).")
			break
		}
		if r.SchemaVersion < 7 {
			continue
		}
		t := s.HTTPTrace
		if !validHTTPTrace(t) {
			invalidate("HTTP client phase timing counters or totals are invalid.")
			break
		}
		if t.CompleteCount != t.RequestAttemptCount {
			invalidate("HTTP client full-duration observations do not reconcile with started step requests.")
			break
		}
		if t.RequestAttemptCount > s.Metrics.Total && t.RequestAttemptCount-s.Metrics.Total > s.Metrics.Cancelled || t.FirstByteCount > t.CompleteCount || t.BodyCount > t.FirstByteCount || t.BodyIncompleteCount != t.FirstByteCount-t.BodyCount || t.WriteCount > t.CompleteCount || t.WriteFailedCount > t.CompleteCount-t.WriteCount || t.DNSCount > t.DNSStartCount || t.ConnectCount > t.ConnectStartCount || t.TLSCount > t.TLSStartCount {
			invalidate("HTTP client phase callback counts are inconsistent.")
			break
		}
		admittedCancelled := t.RateWaitCount - s.Metrics.Total
		if t.RateWaitCount < s.Metrics.Total || admittedCancelled > s.Metrics.Cancelled || t.RateWaitCancelledCount > s.Metrics.Cancelled-admittedCancelled || t.RateWaitCount < t.RequestAttemptCount {
			invalidate("HTTP rate-wait observations do not reconcile with started step requests.")
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
			if r.SchemaVersion >= 7 && (math.IsNaN(profile.ObservedThinkTimeMS) || math.IsInf(profile.ObservedThinkTimeMS, 0) || profile.ObservedThinkTimeMS < 0 || profile.ThinkTimeObservations < 0) {
				invalidate("Profile think-time totals or counts are invalid.")
			}
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

func validHTTPTrace(t HTTPTrace) bool {
	counts := []int{t.RequestAttemptCount, t.WriteCount, t.WriteFailedCount, t.DNSStartCount, t.RateWaitCount, t.RateWaitCancelledCount, t.DNSCount, t.ConnectCount, t.ConnectStartCount, t.TLSCount, t.TLSStartCount, t.FirstByteCount, t.BodyCount, t.BodyIncompleteCount, t.CompleteCount}
	for _, count := range counts {
		if count < 0 {
			return false
		}
	}
	values := []float64{t.WriteMS, t.RateWaitMS, t.DNSMS, t.ConnectMS, t.TLSMS, t.FirstByteMS, t.BodyMS, t.CompleteMS}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	return t.RequestAttemptCount == t.CompleteCount && t.WriteCount <= t.CompleteCount && t.WriteFailedCount <= t.CompleteCount-t.WriteCount && t.FirstByteCount <= t.CompleteCount && t.BodyCount <= t.FirstByteCount && t.BodyIncompleteCount == t.FirstByteCount-t.BodyCount && t.DNSCount <= t.DNSStartCount && t.ConnectCount <= t.ConnectStartCount && t.TLSCount <= t.TLSStartCount
}
