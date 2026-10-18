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
	if r.Generator != nil {
		if r.Generator.InvalidProcessSamples < 0 {
			invalidate("Generator process sample invalidation count is negative.")
		} else if r.Generator.InvalidProcessSamples > 0 {
			limit("One or more generator process samples were zero or implausible and were excluded.")
		}
	}
	if r.SchemaVersion >= 10 && r.Generator != nil {
		g := r.Generator
		values := []float64{g.ObservedSeconds, g.SamplingIntervalMS, g.SampleCollectionTotalMS, g.SampleCollectionMaxMS, g.MaxSampleGapMS, g.GoCPUBusySeconds, g.GoCPUCapacitySeconds, g.GoCPUUtilizationRatio, g.PeakProcessCPUUtilizationRatio, g.MeanProcessCPUUtilizationRatio}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				invalidate("Generator sampling observations contain invalid values.")
				break
			}
		}
		if g.Samples < 0 || g.ProcessSamples < 0 || g.ProcessCPUSamples < 0 || g.ProcessSamples > g.Samples || g.ProcessCPUSamples > g.ProcessSamples {
			invalidate("Generator sample counts are inconsistent.")
		}
		if (g.Samples > 0 && g.GOMAXPROCS < 1) ||
			(g.Samples >= 2 && (g.ObservedSeconds <= 0 || g.MaxSampleGapMS <= 0)) ||
			g.SampleCollectionMaxMS > g.SampleCollectionTotalMS ||
			(g.GoCPUValid && (g.Samples < 2 || g.GOMAXPROCS < 1 || g.GoCPUCapacitySeconds <= 0)) ||
			(g.ProcessCPUValid && (g.ProcessSamples < 2 || g.ProcessCPUSamples < 1)) ||
			(!g.ProcessCPUValid && g.ProcessCPUSamples > 0) {
			invalidate("Generator sampling fields contradict their sample coverage.")
		}
		if g.ProcessCPUValid && g.MeanProcessCPUUtilizationRatio >= 0.9 {
			limit("Mean generator process CPU reached at least 90% of recorded GOMAXPROCS capacity; sustained generator headroom is not established.")
		}
		if g.SamplingIntervalMS <= 0 || g.Samples < 2 {
			limit("Generator sampling interval or sample coverage is unavailable.")
		} else if g.MaxSampleGapMS > 2*g.SamplingIntervalMS {
			limit("Generator health sampling has a gap longer than two configured intervals.")
		}
		if g.SamplingIntervalMS > 0 && g.SampleCollectionMaxMS > g.SamplingIntervalMS {
			limit("A generator health sample took longer than the configured sampling interval.")
		}
		if g.ProcessSamples > 0 && g.PeakProcessRSSBytes == 0 {
			limit("Generator process sampling reported no resident-memory value.")
		}
		if g.Samples > 0 && (g.PeakHeapAllocBytes == 0 || g.PeakGoroutines == 0) {
			limit("Generator runtime sampling reported a zero heap or goroutine peak.")
		}
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
		if r.SchemaVersion >= 8 && !validHTTPDiagnostics(s.Metrics.HTTP, s.Metrics.Total, s.Metrics.Cancelled) {
			invalidate("Named-step HTTP outcome counters are inconsistent.")
			break
		}
	}
	if r.SchemaVersion >= 8 && !validHTTPDiagnostics(r.Metrics.HTTP, r.Metrics.Total, r.Metrics.Cancelled) {
		invalidate("Aggregate HTTP outcome counters are inconsistent.")
	}
	if r.SchemaVersion >= 8 && len(r.Steps) > 0 {
		var summed HTTPDiagnostics
		for _, step := range r.Steps {
			if !addHTTPDiagnostics(&summed, step.Metrics.HTTP) {
				invalidate("Named-step HTTP outcome counters overflow their aggregate.")
				break
			}
		}
		if !equalHTTPDiagnostics(summed, r.Metrics.HTTP) {
			invalidate("Named-step HTTP outcome counters do not reconcile with aggregate outcomes.")
		}
	}
	if r.SchemaVersion >= 8 {
		for _, window := range r.Windows {
			if !validHTTPDiagnostics(window.Metrics.HTTP, window.Metrics.Total, window.Metrics.Cancelled) {
				invalidate("Observation-window HTTP outcome counters are inconsistent.")
				break
			}
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

func validHTTPDiagnostics(d HTTPDiagnostics, completed, cancelled int) bool {
	maximum := int(^uint(0) >> 1)
	if completed < 0 || cancelled < 0 || completed > maximum-cancelled {
		return false
	}
	limit := completed + cancelled
	if d.Status1xx < 0 || d.Status2xx < 0 || d.Status3xx < 0 || d.Status4xx < 0 || d.Status5xx < 0 ||
		d.RedirectResponses < 0 || d.RateLimitedResponses < 0 || d.AssertionFailures < 0 || d.NetworkTimeouts < 0 ||
		d.TLSFailures < 0 || d.TransportFailures < 0 || d.UnexpectedResponseSizes < 0 {
		return false
	}
	counts := [5]int{}
	for code, count := range d.StatusCodes {
		if code < 100 || code > 599 || count < 0 || count > maximum-counts[code/100-1] {
			return false
		}
		counts[code/100-1] += count
	}
	if counts[0] != d.Status1xx || counts[1] != d.Status2xx || counts[2] != d.Status3xx || counts[3] != d.Status4xx || counts[4] != d.Status5xx {
		return false
	}
	var redirects, rateLimited, total int
	for code, count := range d.StatusCodes {
		if code == 300 || code == 301 || code == 302 || code == 303 || code == 305 || code == 307 || code == 308 {
			redirects += count
		}
		if code == 429 {
			rateLimited = count
		}
		if count > maximum-total {
			return false
		}
		total += count
	}
	return redirects == d.RedirectResponses && rateLimited == d.RateLimitedResponses && total <= limit &&
		d.RedirectResponses <= limit && d.RateLimitedResponses <= limit && d.AssertionFailures <= limit &&
		d.NetworkTimeouts <= limit && d.TLSFailures <= limit && d.TransportFailures <= limit && d.UnexpectedResponseSizes <= limit
}

func addHTTPDiagnostics(dst *HTTPDiagnostics, src HTTPDiagnostics) bool {
	maximum := int(^uint(0) >> 1)
	add := func(a *int, b int) (int, bool) {
		if b < 0 || *a < 0 || b > maximum-*a {
			return 0, false
		}
		return *a + b, true
	}
	left := []*int{&dst.Status1xx, &dst.Status2xx, &dst.Status3xx, &dst.Status4xx, &dst.Status5xx, &dst.RedirectResponses, &dst.RateLimitedResponses, &dst.AssertionFailures, &dst.NetworkTimeouts, &dst.TLSFailures, &dst.TransportFailures, &dst.UnexpectedResponseSizes}
	right := []int{src.Status1xx, src.Status2xx, src.Status3xx, src.Status4xx, src.Status5xx, src.RedirectResponses, src.RateLimitedResponses, src.AssertionFailures, src.NetworkTimeouts, src.TLSFailures, src.TransportFailures, src.UnexpectedResponseSizes}
	for i := range left {
		value, ok := add(left[i], right[i])
		if !ok {
			return false
		}
		*left[i] = value
	}
	if dst.StatusCodes == nil && len(src.StatusCodes) > 0 {
		dst.StatusCodes = make(map[int]int, len(src.StatusCodes))
	}
	for code, count := range src.StatusCodes {
		current := dst.StatusCodes[code]
		value, ok := add(&current, count)
		if !ok {
			return false
		}
		dst.StatusCodes[code] = value
	}
	return true
}

func equalHTTPDiagnostics(a, b HTTPDiagnostics) bool {
	if a.Status1xx != b.Status1xx || a.Status2xx != b.Status2xx || a.Status3xx != b.Status3xx || a.Status4xx != b.Status4xx || a.Status5xx != b.Status5xx || a.RedirectResponses != b.RedirectResponses || a.RateLimitedResponses != b.RateLimitedResponses || a.AssertionFailures != b.AssertionFailures || a.NetworkTimeouts != b.NetworkTimeouts || a.TLSFailures != b.TLSFailures || a.TransportFailures != b.TransportFailures || a.UnexpectedResponseSizes != b.UnexpectedResponseSizes || len(a.StatusCodes) != len(b.StatusCodes) {
		return false
	}
	for code, count := range a.StatusCodes {
		if b.StatusCodes[code] != count {
			return false
		}
	}
	return true
}
