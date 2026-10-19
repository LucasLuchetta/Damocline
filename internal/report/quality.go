package report

import (
	"math"
	"net"
	"net/url"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

// Quality describes whether arithmetic delivery/accounting evidence can be
// trusted. It is orthogonal to SLO Outcome. Positive calibration equivalence
// is available only for the calibrator-owned loopback soak with independently
// reconciled server counters.
type Quality struct {
	State    string   `json:"state"`
	Findings []string `json:"findings"`
}

const (
	QualityValid   = "VALID_WITHIN_ENVELOPE"
	QualityLimited = "LIMITED"
	QualityInvalid = "INVALID"
	// SamplerCollectionWallLimit is a conservative screening threshold, not a
	// calibrated estimate of CPU overhead. Collection wall time at or above 5%
	// of the observation window is material enough to limit the measurement.
	SamplerCollectionWallLimit = 0.05
)

// AssessQuality uses only aggregate invariants and reported generator
// observations. It does NOT infer network quality or target saturation.
// No secrets, hostnames, routes or individual response content are reported.
func AssessQuality(r Result) *Quality {
	q := &Quality{State: QualityLimited}
	controlledReference := false
	invalidate := func(reason string) {
		q.State = QualityInvalid
		q.Findings = append(q.Findings, reason)
	}
	limit := func(reason string) { q.Findings = append(q.Findings, reason) }
	if r.SchemaVersion >= 20 && !validFailureClassificationEvidence(r) {
		invalidate("HTTP failure-classification evidence is missing or inconsistent for schema 20.")
	}
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
	arrivalsAdmitted := int64(0)
	if r.ArrivalsAdmitted != nil {
		arrivalsAdmitted = *r.ArrivalsAdmitted
	}
	if arrivalsAdmitted < 0 {
		invalidate("Admitted-arrival counter is negative.")
	}
	if r.SchemaVersion >= 17 && r.ArrivalsAdmitted == nil {
		invalidate("Current report omits admitted-arrival evidence.")
	}
	if r.SchemaVersion < 17 && r.ArrivalsAdmitted != nil {
		invalidate("Pre-v17 reports cannot carry the admitted-arrival counter.")
	}
	cancelledBeforeAdmission := int64(0)
	if r.ArrivalsCancelledBeforeAdmission != nil {
		cancelledBeforeAdmission = *r.ArrivalsCancelledBeforeAdmission
	}
	if cancelledBeforeAdmission < 0 {
		invalidate("Cancelled-before-admission arrival counter is negative.")
	}
	if r.SchemaVersion >= 16 && r.ArrivalsCancelledBeforeAdmission == nil {
		invalidate("Current report omits cancelled-before-admission arrival evidence.")
	}
	if r.SchemaVersion < 16 && r.ArrivalsCancelledBeforeAdmission != nil {
		invalidate("Pre-v16 reports cannot carry the cancelled-before-admission counter.")
	}
	if r.SchemaVersion >= 16 && r.Config.Mode != "arrival" && cancelledBeforeAdmission != 0 {
		invalidate("Cancelled-before-admission arrivals are present for a non-arrival workload.")
	}
	if r.SchemaVersion >= 17 && r.Config.Mode != "arrival" && arrivalsAdmitted != 0 {
		invalidate("Admitted arrivals are present for a non-arrival workload.")
	}
	if r.SchemaVersion >= 15 &&
		(r.IterationsCompleted != int64(r.Journeys.Total) ||
			r.IterationsStarted < r.IterationsCompleted ||
			r.IterationsStarted-r.IterationsCompleted != int64(r.Journeys.Cancelled)) {
		invalidate("Started, completed and cancelled journeys do not reconcile with iteration counters.")
	}
	if r.Config.Mode == "arrival" {
		breakdownInvalid := false
		if r.SchemaVersion >= 16 {
			breakdownInvalid = r.ArrivalsDroppedLate > r.ArrivalsDropped ||
				r.ArrivalsDroppedBusy > r.ArrivalsDropped-r.ArrivalsDroppedLate ||
				r.ArrivalsCancelledBeforeAdmission == nil || cancelledBeforeAdmission != r.ArrivalsDropped-r.ArrivalsDroppedLate-r.ArrivalsDroppedBusy
		} else {
			breakdownInvalid = r.ArrivalsDroppedLate > r.ArrivalsDropped ||
				r.ArrivalsDroppedBusy != r.ArrivalsDropped-r.ArrivalsDroppedLate
		}
		conservationInvalid := r.IterationsStarted > r.ArrivalsScheduled ||
			r.ArrivalsDropped != r.ArrivalsScheduled-r.IterationsStarted
		if r.SchemaVersion >= 17 {
			conservationInvalid = r.ArrivalsAdmitted == nil || arrivalsAdmitted > r.ArrivalsScheduled ||
				arrivalsAdmitted != r.IterationsStarted ||
				r.ArrivalsDropped != r.ArrivalsScheduled-arrivalsAdmitted
		}
		if conservationInvalid || breakdownInvalid {
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
		if r.SchemaVersion >= 18 {
			if g.CPUCapacityCores <= 0 || math.IsNaN(g.CPUCapacityCores) || math.IsInf(g.CPUCapacityCores, 0) ||
				g.CPUCapacitySource == "" || g.CPUCapacityCores > float64(g.GOMAXPROCS) {
				invalidate("Generator CPU capacity evidence is inconsistent.")
			} else if !g.CPUCapacityKnown {
				limit("Effective generator CPU capacity could not be established from runtime and cgroup evidence.")
			}
		}
		if r.SchemaVersion >= 19 && (g.LogicalCPUs < 1 || g.CPUCapacityCores > float64(g.LogicalCPUs)) {
			invalidate("Generator logical CPU and effective-capacity evidence is inconsistent.")
		}
		if g.ProcessCPUValid && g.MeanProcessCPUUtilizationRatio >= 0.9 {
			limit("Mean generator process CPU reached at least 90% of detected CPU capacity; sustained generator headroom is not established.")
		}
		if g.SamplingIntervalMS <= 0 || g.Samples < 2 {
			limit("Generator sampling interval or sample coverage is unavailable.")
		} else if g.MaxSampleGapMS > 2*g.SamplingIntervalMS {
			limit("Generator health sampling has a gap longer than two configured intervals.")
		}
		if g.SamplingIntervalMS > 0 && g.SampleCollectionMaxMS > g.SamplingIntervalMS {
			limit("A generator health sample took longer than the configured sampling interval.")
		}
		if g.ObservedSeconds > 0 && g.SampleCollectionTotalMS/(g.ObservedSeconds*1000) >= SamplerCollectionWallLimit {
			limit("Generator health collection occupied at least 5% of the observation window; collection wall time is a screening signal, not a CPU-overhead estimate.")
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
	if r.SchemaVersion >= 14 && r.Config.ConnectionPolicy != "reuse" && r.Config.ConnectionPolicy != "new_per_request" {
		invalidate("Current report connection policy is absent or unsupported.")
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
		if r.SchemaVersion >= 12 && !validResponseBodyDiagnostics(
			s.Metrics.ResponseBody,
			responseStatusCount(s.Metrics.HTTP),
			s.Metrics.HTTP.UnexpectedResponseSizes,
		) {
			invalidate("Named-step response-body observations do not reconcile with HTTP responses.")
			break
		}
	}
	if r.SchemaVersion >= 8 && !validHTTPDiagnostics(r.Metrics.HTTP, r.Metrics.Total, r.Metrics.Cancelled) {
		invalidate("Aggregate HTTP outcome counters are inconsistent.")
	}
	if r.SchemaVersion >= 12 && !validResponseBodyDiagnostics(
		r.Metrics.ResponseBody,
		responseStatusCount(r.Metrics.HTTP),
		r.Metrics.HTTP.UnexpectedResponseSizes,
	) {
		invalidate("Aggregate response-body observations do not reconcile with HTTP responses.")
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
		if r.SchemaVersion >= 12 {
			var summedBodies ResponseBodyDiagnostics
			for _, step := range r.Steps {
				if !addResponseBodyDiagnostics(&summedBodies, step.Metrics.ResponseBody) {
					invalidate("Named-step response-body observations overflow their aggregate.")
					break
				}
			}
			if !equalResponseBodyDiagnostics(summedBodies, r.Metrics.ResponseBody) {
				invalidate("Named-step response-body observations do not reconcile with aggregate measurements.")
			}
		}
	}
	if r.SchemaVersion >= 8 {
		if r.SchemaVersion >= 13 && r.Config.WindowDuration != "" && !validObservationWindowBounds(r) {
			invalidate("Configured observation windows are missing, overlapping, or outside the workload horizon.")
		}
		for _, window := range r.Windows {
			if !validHTTPDiagnostics(window.Metrics.HTTP, window.Metrics.Total, window.Metrics.Cancelled) {
				invalidate("Observation-window HTTP outcome counters are inconsistent.")
				break
			}
			if r.SchemaVersion >= 12 && !validResponseBodyDiagnostics(
				window.Metrics.ResponseBody,
				responseStatusCount(window.Metrics.HTTP),
				window.Metrics.HTTP.UnexpectedResponseSizes,
			) {
				invalidate("Observation-window response-body observations do not reconcile with HTTP responses.")
				break
			}
			if r.SchemaVersion >= 13 && !validWindowNamedMetrics(r, window) {
				invalidate("Observation-window named step or profile ledgers are missing, duplicated, or inconsistent.")
				break
			}
			if r.SchemaVersion >= 22 && !validWindowJourneyMetrics(window.Journeys) {
				invalidate("Observation-window completed-journey metrics are missing or inconsistent.")
				break
			}
		}
		if r.SchemaVersion >= 13 && !validWindowProfileTotals(r) {
			invalidate("Observation-window profile journeys exceed or do not match the persisted profile ledgers.")
		}
		if r.SchemaVersion >= 22 && !validWindowJourneyTotals(r) {
			invalidate("Observation-window completed journeys exceed or conflict with the aggregate journey ledger.")
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
	if r.Calibration == nil {
		limit("No local generator preflight is attached; workload-specific generator equivalence is unverified.")
	} else if err := calibration.ValidateRecord(r.Calibration.Record); err != nil {
		invalidate("Attached generator calibration record is malformed or inconsistent.")
	} else {
		controlledReference = controlledReferenceEvidenceValid(r)
		unverified := make(map[string]bool)
		observedSizeDimensions := 0
		observedLatencyDimensions := 0
		workloadShapeDimensions := 0
		responseShapeDimensions := 0
		fixtureIdentityDimensions := 0
		for _, dimension := range r.Calibration.Coverage.Dimensions {
			if dimension.Name == "" || (dimension.Status != "MATCHED" && dimension.Status != "OUTSIDE_REFERENCE" && dimension.Status != "UNVERIFIED") {
				invalidate("Attached calibration coverage contains an invalid dimension.")
				break
			}
			if dimension.Status != "MATCHED" {
				unverified[dimension.Name] = true
			}
			if dimension.Name == "response_shape" {
				responseShapeDimensions++
				expected := "UNVERIFIED"
				if controlledReference {
					expected = "MATCHED"
				}
				if r.SchemaVersion >= 15 && dimension.Status != expected {
					invalidate("Attached response-shape status disagrees with controlled fixture observations.")
				}
			}
			if dimension.Name == "fixture_identity" {
				fixtureIdentityDimensions++
				expected := "UNVERIFIED"
				if controlledReference {
					expected = "MATCHED"
				}
				if r.SchemaVersion >= 15 && dimension.Status != expected {
					invalidate("Attached fixture-identity status disagrees with controlled server evidence.")
				}
			}
			if dimension.Name == "observed_response_size" {
				observedSizeDimensions++
				if r.SchemaVersion >= 12 && dimension != AssessObservedResponseSize(r, int64(r.Calibration.Record.Fixture.HealthyResponseBytes)) {
					invalidate("Attached observed response-size status disagrees with run measurements.")
				}
			}
			if dimension.Name == "observed_response_latency" {
				observedLatencyDimensions++
				if r.SchemaVersion >= 12 && dimension != AssessObservedResponseLatency(r) {
					invalidate("Attached observed response-latency status disagrees with run measurements.")
				}
			}
			if dimension.Name == "declared_workload_shape" {
				workloadShapeDimensions++
				if r.SchemaVersion >= 12 && dimension != AssessDeclaredWorkloadShape(r, r.Calibration.Record) {
					invalidate("Attached workload-shape status disagrees with run measurements.")
				}
			}
		}
		if controlledReference {
			if r.Calibration.Coverage.Status != "MATCHED" || unverified["response_shape"] || unverified["fixture_identity"] {
				invalidate("Controlled reference coverage status is inconsistent with its observed evidence.")
			}
			if responseShapeDimensions != 1 || fixtureIdentityDimensions != 1 || observedSizeDimensions != 1 ||
				observedLatencyDimensions != 1 || workloadShapeDimensions != 1 {
				invalidate("Controlled reference coverage omits or duplicates a required evidence dimension.")
			}
			for _, dimension := range r.Calibration.Coverage.Dimensions {
				if dimension.Status != "MATCHED" {
					invalidate("A controlled reference qualification contains an unmatched required calibration dimension.")
					break
				}
			}
		} else if r.Calibration.Coverage.Status != "INCONCLUSIVE" || !unverified["response_shape"] || !unverified["fixture_identity"] {
			invalidate("Attached calibration coverage omits required response-shape or fixture-identity uncertainty.")
		} else {
			if r.SchemaVersion >= 12 && observedSizeDimensions != 1 {
				invalidate("Attached calibration coverage omits unique observed response-size evidence.")
			}
			if r.SchemaVersion >= 12 && observedLatencyDimensions != 1 {
				invalidate("Attached calibration coverage omits unique observed response-latency evidence.")
			}
			if r.SchemaVersion >= 12 && workloadShapeDimensions != 1 {
				invalidate("Attached calibration coverage omits unique declared workload-shape evidence.")
			}
			if r.SchemaVersion >= 15 && (responseShapeDimensions != 1 || fixtureIdentityDimensions != 1) {
				invalidate("Current calibration coverage omits unique response-shape or fixture-identity evidence.")
			}
			if observedSizeDimensions == 1 {
				for _, dimension := range r.Calibration.Coverage.Dimensions {
					if dimension.Name != "observed_response_size" {
						continue
					}
					switch dimension.Status {
					case "MATCHED":
						if !controlledReference {
							limit("Observed response sizes match the fixed fixture size, but response timing distribution and endpoint identity remain unverified.")
						}
					case "OUTSIDE_REFERENCE":
						limit("One or more observed response sizes are outside the fixed fixture size; generator equivalence is not established.")
					default:
						limit("Observed response sizes cannot be compared because complete per-step evidence is insufficient.")
					}
				}
			} else {
				limit("Calibration is attached, but response shape and controlled fixture identity remain unverified.")
			}
			if observedLatencyDimensions == 1 {
				for _, dimension := range r.Calibration.Coverage.Dimensions {
					if dimension.Name != "observed_response_latency" {
						continue
					}
					switch dimension.Status {
					case "MATCHED":
						if !controlledReference {
							limit("Observed request p50 and p95 fall within the local fixture guardrail; " +
								"percentile dependence and observation-window representativeness remain unverified.")
						}
					case "OUTSIDE_REFERENCE":
						limit("One or more observed request percentiles fall outside the local fixture guardrail; " +
							"this does not identify the cause.")
					default:
						limit("Observed request latency cannot be compared because per-step sample coverage is insufficient.")
					}
				}
			}
			if workloadShapeDimensions == 1 {
				for _, dimension := range r.Calibration.Coverage.Dimensions {
					if dimension.Name != "declared_workload_shape" {
						continue
					}
					switch dimension.Status {
					case "MATCHED":
						if !controlledReference {
							limit("The declared request/session shape matches the fixed calibration; runtime headroom, endpoint identity, and application behavior are separate evidence gates.")
						}
					case "OUTSIDE_REFERENCE":
						limit("The declared request/session shape is outside the fixed calibration workload.")
					default:
						limit("Declared workload-shape equivalence is unverified because a comparable fingerprint is unavailable.")
					}
				}
			}
		}
	}
	if controlledReference && q.State != QualityInvalid && len(q.Findings) == 0 {
		q.State = QualityValid
	}
	return q
}

func validFailureClassificationEvidence(r Result) bool {
	validMetrics := func(metrics MetricSet) bool {
		return metrics.HTTP.FailureClassificationVersion == 1 &&
			metrics.HTTP.DNSFailures == metrics.Reasons["dns_failure"] &&
			metrics.HTTP.ConnectionFailures == metrics.Reasons["connection_failure"]
	}
	if !validMetrics(r.Metrics) {
		return false
	}
	for _, step := range r.Steps {
		if !validMetrics(step.Metrics) {
			return false
		}
	}
	for _, window := range r.Windows {
		if !validMetrics(window.Metrics) {
			return false
		}
		for _, step := range window.Steps {
			if !validMetrics(step.Metrics) {
				return false
			}
		}
	}
	return true
}

// HasValidFailureClassification reports whether a schema-v20 run contains
// versioned and internally consistent DNS and connection failure counters.
func HasValidFailureClassification(r Result) bool {
	return r.SchemaVersion < 20 || validFailureClassificationEvidence(r)
}

func controlledReferenceEvidenceValid(r Result) bool {
	e := r.ReferenceEvidence
	if r.SchemaVersion < 17 || e == nil || e.Kind != "calibrator_child_process_loopback_soak_v1" ||
		r.Calibration == nil || calibration.ValidateRecord(r.Calibration.Record) != nil ||
		r.Calibration.Record.SchemaVersion < 7 ||
		r.Calibration.Record.Status != "REFERENCE_SMOKE_PASSED" ||
		e.FixtureFingerprint != r.Calibration.Record.ScenarioFingerprintSHA256 {
		return false
	}
	if r.Generator == nil || !r.Generator.CPUCapacityKnown ||
		r.Generator.CPUCapacityCores != r.Calibration.Record.Generator.CPUCapacityCores ||
		r.Generator.CPUCapacitySource != r.Calibration.Record.Generator.CPUCapacitySource ||
		r.Generator.GOMAXPROCS != r.Calibration.Record.Generator.GOMAXPROCS {
		return false
	}
	u, err := url.Parse(r.Target)
	if err != nil || u == nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).Equal(net.ParseIP("127.0.0.1")) {
		return false
	}
	f := r.Calibration.Record.Fixture
	if f.NetworkMode != "loopback_same_host_separate_process" || f.HealthyResponseBytes != 4096 || f.HealthyDelay != "20ms" ||
		r.Config.ConnectionPolicy != "reuse" || r.Config.Users != f.StageUsers || r.Config.Mode != "arrival" ||
		r.Config.ArrivalRate != 10 || r.Config.Duration != f.SoakDuration || r.Config.RequestTimeout != "1s" ||
		r.Config.MaxRPS < r.Config.ArrivalRate || r.Config.MaxRPS > f.MaximumRPS || r.Config.RampUp != "" ||
		(r.Config.ThinkTime != "" && r.Config.ThinkTime != "0s") || r.Config.SeparateSessions || len(r.Config.Profiles) != 0 ||
		len(r.Profiles) != 0 || len(r.Steps) != 1 {
		return false
	}
	step := r.Steps[0]
	if (step.Profile != "" && step.Profile != "default") || step.Name != "work" || step.Method != "GET" || step.Path != "/work" {
		return false
	}
	if r.Metrics.Total <= 0 || r.Metrics.Success != r.Metrics.Total || r.Metrics.Failures != 0 || r.Metrics.Cancelled != 0 ||
		r.ArrivalsScheduled != int64(r.Metrics.Total) || r.ArrivalsAdmitted == nil || *r.ArrivalsAdmitted != r.ArrivalsScheduled || r.IterationsStarted != *r.ArrivalsAdmitted ||
		r.IterationsCompleted != r.IterationsStarted || r.ArrivalsDropped != 0 ||
		e.ServerReceived != int64(r.Metrics.Total) || e.ServerCompleted != e.ServerReceived ||
		e.ServerSuccess != e.ServerReceived || e.ServerFailures != 0 || e.ServerCancelled != 0 || e.ServerRejected != 0 {
		return false
	}
	if !controlledReferenceSchedulerLagValid(r) {
		return false
	}
	if step.Metrics.Total != r.Metrics.Total || step.Metrics.Success != r.Metrics.Success || step.Metrics.Failures != 0 ||
		step.Metrics.ResponseBody.CompleteObservations != int64(r.Metrics.Total) ||
		step.Metrics.ResponseBody.IncompleteObservations != 0 || step.Metrics.ResponseBody.OversizedObservations != 0 ||
		step.Metrics.ResponseBody.MinBytes != int64(f.HealthyResponseBytes) ||
		step.Metrics.ResponseBody.MaxBytes != int64(f.HealthyResponseBytes) ||
		step.Metrics.ResponseBody.TotalBytes != int64(r.Metrics.Total*f.HealthyResponseBytes) {
		return false
	}
	return AssessObservedResponseSize(r, int64(f.HealthyResponseBytes)).Status == "MATCHED" &&
		AssessObservedResponseLatency(r).Status == "MATCHED" &&
		AssessDeclaredWorkloadShape(r, r.Calibration.Record).Status == "MATCHED"
}

func controlledReferenceSchedulerLagValid(r Result) bool {
	return r.SchedulerLagMethod == calibration.SchedulerDispatchLagMethod &&
		r.SchedulerLagSamples == r.ArrivalsScheduled &&
		!math.IsNaN(r.SchedulerLagP50MS) && !math.IsInf(r.SchedulerLagP50MS, 0) &&
		!math.IsNaN(r.SchedulerLagP95MS) && !math.IsInf(r.SchedulerLagP95MS, 0) &&
		!math.IsNaN(r.SchedulerLagMaxMS) && !math.IsInf(r.SchedulerLagMaxMS, 0) &&
		r.SchedulerLagP50MS >= 0 && r.SchedulerLagP50MS <= r.SchedulerLagP95MS &&
		r.SchedulerLagP95MS <= r.SchedulerLagMaxMS &&
		r.SchedulerLagP95MS <= calibration.ReferenceSoakSchedulerLagP95LimitMS &&
		r.SchedulerLagMaxMS < calibration.ReferenceSoakSchedulerLagMaxLimitMS
}

func validObservationWindowBounds(r Result) bool {
	duration, err := time.ParseDuration(r.Config.WindowDuration)
	if err != nil {
		return false
	}
	if duration == 0 {
		return len(r.Windows) == 0
	}
	if duration < 0 || r.ObservationSeconds <= 0 || math.IsNaN(r.ObservationSeconds) || math.IsInf(r.ObservationSeconds, 0) {
		return false
	}
	windowSeconds := duration.Seconds()
	expectedCount := int(math.Ceil(r.ObservationSeconds / windowSeconds))
	if expectedCount <= 0 || len(r.Windows) != expectedCount {
		return false
	}
	for index, window := range r.Windows {
		expectedStart := float64(index) * windowSeconds
		expectedEnd := math.Min(expectedStart+windowSeconds, r.ObservationSeconds)
		if math.IsNaN(window.StartSeconds) || math.IsInf(window.StartSeconds, 0) ||
			math.IsNaN(window.EndSeconds) || math.IsInf(window.EndSeconds, 0) ||
			math.Abs(window.StartSeconds-expectedStart) > 0.001 ||
			math.Abs(window.EndSeconds-expectedEnd) > 0.001 {
			return false
		}
	}
	return true
}

func validWindowNamedMetrics(r Result, window WindowResult) bool {
	if len(window.Steps) != len(r.Steps) || len(window.Profiles) != len(r.Config.Profiles) {
		return false
	}
	stepNames := make(map[string]bool, len(r.Steps))
	for _, step := range r.Steps {
		stepNames[step.Name] = true
	}
	profileNames := make(map[string]bool, len(r.Config.Profiles))
	for _, profile := range r.Config.Profiles {
		profileNames[profile.Name] = true
	}
	var stepTotal, stepSuccess, stepFailures, stepCancelled int
	var stepHTTP HTTPDiagnostics
	var stepBodies ResponseBodyDiagnostics
	for _, named := range window.Steps {
		if !stepNames[named.Name] || !validWindowMetricSet(named.Metrics) {
			return false
		}
		if named.Metrics.Total > window.Metrics.Total-stepTotal || named.Metrics.Success > window.Metrics.Success-stepSuccess ||
			named.Metrics.Failures > window.Metrics.Failures-stepFailures || named.Metrics.Cancelled > window.Metrics.Cancelled-stepCancelled ||
			!addHTTPDiagnostics(&stepHTTP, named.Metrics.HTTP) || !addResponseBodyDiagnostics(&stepBodies, named.Metrics.ResponseBody) {
			return false
		}
		stepTotal += named.Metrics.Total
		stepSuccess += named.Metrics.Success
		stepFailures += named.Metrics.Failures
		stepCancelled += named.Metrics.Cancelled
		delete(stepNames, named.Name)
	}
	if len(stepNames) != 0 || stepTotal != window.Metrics.Total || stepSuccess != window.Metrics.Success ||
		stepFailures != window.Metrics.Failures || stepCancelled != window.Metrics.Cancelled ||
		!equalHTTPDiagnostics(stepHTTP, window.Metrics.HTTP) || !equalResponseBodyDiagnostics(stepBodies, window.Metrics.ResponseBody) {
		return false
	}
	for _, named := range window.Profiles {
		if !profileNames[named.Name] || !validWindowMetricSet(named.Metrics) {
			return false
		}
		delete(profileNames, named.Name)
	}
	return len(stepNames) == 0 && len(profileNames) == 0
}

func validWindowMetricSet(metrics MetricSet) bool {
	if metrics.Total < 0 || metrics.Success < 0 || metrics.Failures < 0 || metrics.Cancelled < 0 ||
		metrics.Success > metrics.Total || metrics.Failures != metrics.Total-metrics.Success {
		return false
	}
	latencies := []float64{metrics.MeanMS, metrics.P50MS, metrics.P95MS, metrics.P99MS, metrics.P95IID95LowerMS, metrics.P95IID95UpperMS}
	for _, latency := range latencies {
		if math.IsNaN(latency) || math.IsInf(latency, 0) || latency < 0 {
			return false
		}
	}
	if metrics.P50MS > metrics.P95MS || metrics.P95MS > metrics.P99MS ||
		metrics.P95IID95UpperMS < metrics.P95IID95LowerMS ||
		(metrics.P95IID95Approximation && metrics.P95IID95UpperMS == 0 && metrics.P95MS > 0) {
		return false
	}
	return validHTTPDiagnostics(metrics.HTTP, metrics.Total, metrics.Cancelled) &&
		validResponseBodyDiagnostics(metrics.ResponseBody, responseStatusCount(metrics.HTTP), metrics.HTTP.UnexpectedResponseSizes)
}

func validWindowJourneyMetrics(metrics MetricSet) bool {
	return validWindowMetricSet(metrics)
}

func validWindowJourneyTotals(r Result) bool {
	var total, success, failures, cancelled int
	for _, window := range r.Windows {
		m := window.Journeys
		if m.Total < 0 || m.Success < 0 || m.Failures < 0 || m.Cancelled < 0 ||
			m.Total > r.Journeys.Total-total || m.Success > r.Journeys.Success-success ||
			m.Failures > r.Journeys.Failures-failures || m.Cancelled > r.Journeys.Cancelled-cancelled {
			return false
		}
		if len(r.Config.Profiles) > 0 {
			var profileTotal, profileSuccess, profileFailures, profileCancelled int
			for _, profile := range window.Profiles {
				if profile.Metrics.Total > m.Total-profileTotal || profile.Metrics.Success > m.Success-profileSuccess ||
					profile.Metrics.Failures > m.Failures-profileFailures || profile.Metrics.Cancelled > m.Cancelled-profileCancelled {
					return false
				}
				profileTotal += profile.Metrics.Total
				profileSuccess += profile.Metrics.Success
				profileFailures += profile.Metrics.Failures
				profileCancelled += profile.Metrics.Cancelled
			}
			if profileTotal != m.Total || profileSuccess != m.Success || profileFailures != m.Failures || profileCancelled != m.Cancelled {
				return false
			}
		}
		total += m.Total
		success += m.Success
		failures += m.Failures
		cancelled += m.Cancelled
	}
	if total > r.Journeys.Total || success > r.Journeys.Success || failures > r.Journeys.Failures || cancelled > r.Journeys.Cancelled {
		return false
	}
	if len(r.Profiles) > 0 {
		var profileTotal, profileSuccess, profileFailures, profileCancelled int
		for _, profile := range r.Profiles {
			m := profile.Journeys
			if m.Total > r.Journeys.Total-profileTotal || m.Success > r.Journeys.Success-profileSuccess ||
				m.Failures > r.Journeys.Failures-profileFailures || m.Cancelled > r.Journeys.Cancelled-profileCancelled {
				return false
			}
			profileTotal += m.Total
			profileSuccess += m.Success
			profileFailures += m.Failures
			profileCancelled += m.Cancelled
		}
		if profileTotal != r.Journeys.Total || profileSuccess != r.Journeys.Success ||
			profileFailures != r.Journeys.Failures || profileCancelled != r.Journeys.Cancelled {
			return false
		}
	}
	return true
}

func validWindowProfileTotals(r Result) bool {
	if len(r.Profiles) != len(r.Config.Profiles) {
		return false
	}
	configured := make(map[string]bool, len(r.Config.Profiles))
	for _, profile := range r.Config.Profiles {
		if configured[profile.Name] {
			return false
		}
		configured[profile.Name] = true
	}
	profileTotals := make(map[string]int, len(r.Profiles))
	for _, profile := range r.Profiles {
		if !configured[profile.Name] {
			return false
		}
		configured[profile.Name] = false
		profileTotals[profile.Name] = profile.Journeys.Total
	}
	for _, exists := range configured {
		if exists {
			return false
		}
	}
	windowTotals := make(map[string]int, len(profileTotals))
	for _, window := range r.Windows {
		for _, profile := range window.Profiles {
			if profile.Metrics.Total > profileTotals[profile.Name]-windowTotals[profile.Name] {
				return false
			}
			windowTotals[profile.Name] += profile.Metrics.Total
		}
	}
	return true
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
		d.FailureClassificationVersion < 0 || d.FailureClassificationVersion > 1 ||
		d.DNSFailures < 0 || d.ConnectionFailures < 0 ||
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
		d.NetworkTimeouts <= limit && d.DNSFailures <= d.TransportFailures &&
		d.ConnectionFailures <= d.TransportFailures && d.TLSFailures <= limit &&
		d.TransportFailures <= limit && d.UnexpectedResponseSizes <= limit
}

func addHTTPDiagnostics(dst *HTTPDiagnostics, src HTTPDiagnostics) bool {
	maximum := int(^uint(0) >> 1)
	add := func(a *int, b int) (int, bool) {
		if b < 0 || *a < 0 || b > maximum-*a {
			return 0, false
		}
		return *a + b, true
	}
	left := []*int{&dst.Status1xx, &dst.Status2xx, &dst.Status3xx, &dst.Status4xx, &dst.Status5xx, &dst.RedirectResponses, &dst.RateLimitedResponses, &dst.AssertionFailures, &dst.NetworkTimeouts, &dst.DNSFailures, &dst.ConnectionFailures, &dst.TLSFailures, &dst.TransportFailures, &dst.UnexpectedResponseSizes}
	right := []int{src.Status1xx, src.Status2xx, src.Status3xx, src.Status4xx, src.Status5xx, src.RedirectResponses, src.RateLimitedResponses, src.AssertionFailures, src.NetworkTimeouts, src.DNSFailures, src.ConnectionFailures, src.TLSFailures, src.TransportFailures, src.UnexpectedResponseSizes}
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
	if a.Status1xx != b.Status1xx || a.Status2xx != b.Status2xx || a.Status3xx != b.Status3xx || a.Status4xx != b.Status4xx || a.Status5xx != b.Status5xx || a.RedirectResponses != b.RedirectResponses || a.RateLimitedResponses != b.RateLimitedResponses || a.AssertionFailures != b.AssertionFailures || a.NetworkTimeouts != b.NetworkTimeouts || a.DNSFailures != b.DNSFailures || a.ConnectionFailures != b.ConnectionFailures || a.TLSFailures != b.TLSFailures || a.TransportFailures != b.TransportFailures || a.UnexpectedResponseSizes != b.UnexpectedResponseSizes || len(a.StatusCodes) != len(b.StatusCodes) {
		return false
	}
	for code, count := range a.StatusCodes {
		if b.StatusCodes[code] != count {
			return false
		}
	}
	return true
}

const maxMeasuredResponseBodyBytes int64 = 2 << 20

// AssessObservedResponseSize compares complete response bodies in every named
// step with the one fixed response size in the validated preflight fixture.
// It does not assess timing, endpoint identity, or representativeness.
func AssessObservedResponseSize(r Result, expectedBytes int64) calibration.Dimension {
	dimension := calibration.Dimension{
		Name:   "observed_response_size",
		Status: "UNVERIFIED",
		Reason: "Complete per-step response-body observations are unavailable for comparison.",
	}
	if r.SchemaVersion < 12 || expectedBytes < 0 || expectedBytes > maxMeasuredResponseBodyBytes || len(r.Steps) == 0 {
		return dimension
	}
	for _, step := range r.Steps {
		body := step.Metrics.ResponseBody
		responses := responseStatusCount(step.Metrics.HTTP)
		if !validResponseBodyDiagnostics(body, responses, step.Metrics.HTTP.UnexpectedResponseSizes) {
			return dimension
		}
		if body.OversizedObservations > 0 ||
			(body.CompleteObservations > 0 && (body.MinBytes != expectedBytes || body.MaxBytes != expectedBytes)) {
			dimension.Status = "OUTSIDE_REFERENCE"
			dimension.Reason = "One or more observed complete response bodies differ from the fixed calibration response size."
			return dimension
		}
		if body.CompleteObservations == 0 || body.IncompleteObservations > 0 {
			return dimension
		}
	}
	dimension.Status = "MATCHED"
	dimension.Reason = "Every named step returned complete bodies at the fixed calibration response size in this run."
	return dimension
}

const minObservedLatencySamples = 100

// AssessObservedResponseLatency compares descriptive per-step request p50/p95
// values with the coarse guardrail used by the local reference fixture. The
// sample floor is a screening rule, not a confidence or independence claim.
func AssessObservedResponseLatency(r Result) calibration.Dimension {
	dimension := calibration.Dimension{
		Name:   "observed_response_latency",
		Status: "UNVERIFIED",
		Reason: "Per-step latency samples are insufficient for the local fixture guardrail comparison.",
	}
	if r.SchemaVersion < 12 || len(r.Steps) == 0 {
		return dimension
	}
	for _, step := range r.Steps {
		outside, sufficient := latencyGuardrail(step.Metrics)
		if !sufficient {
			return dimension
		}
		if outside {
			dimension.Status = "OUTSIDE_REFERENCE"
			dimension.Reason = "One or more observed per-step request p50/p95 values fall outside the local fixture guardrail."
			return dimension
		}
		if r.SchemaVersion >= 13 && r.Config.WindowDuration != "" {
			for _, window := range r.Windows {
				var found bool
				for _, windowStep := range window.Steps {
					if windowStep.Name != step.Name {
						continue
					}
					found = true
					outside, sufficient = latencyGuardrail(windowStep.Metrics)
					break
				}
				if !found || !sufficient {
					return dimension
				}
				if outside {
					dimension.Status = "OUTSIDE_REFERENCE"
					dimension.Reason = "One or more observed per-step request p50/p95 values in an observation window fall outside the local fixture guardrail."
					return dimension
				}
			}
		}
	}
	dimension.Status = "MATCHED"
	dimension.Reason = "Every named step and configured observation window has at least 100 successful observations and descriptive p50/p95 values within the local fixture guardrail."
	return dimension
}

func latencyGuardrail(metrics MetricSet) (outside, sufficient bool) {
	if metrics.Total < minObservedLatencySamples || metrics.Success < minObservedLatencySamples {
		return false, false
	}
	p50Invalid := math.IsNaN(metrics.P50MS) || math.IsInf(metrics.P50MS, 0) || metrics.P50MS < 0
	p95Invalid := math.IsNaN(metrics.P95MS) || math.IsInf(metrics.P95MS, 0) || metrics.P95MS < metrics.P50MS
	if p50Invalid || p95Invalid {
		return false, false
	}
	p50Outside := metrics.P50MS < calibration.HealthyLatencyMinMS || metrics.P50MS > calibration.HealthyLatencyMaxMS
	p95Outside := metrics.P95MS < calibration.HealthyLatencyMinMS || metrics.P95MS > calibration.HealthyLatencyMaxMS
	return p50Outside || p95Outside, true
}

// AssessDeclaredWorkloadShape recomputes the workload fingerprint comparison
// from sanitized report metadata and the validated preflight record.
func AssessDeclaredWorkloadShape(r Result, record calibration.Record) calibration.Dimension {
	if r.SchemaVersion < 11 || len(r.Steps) != 1 || len(r.Profiles) != 0 ||
		len(r.Config.Profiles) != 0 || r.Config.SeparateSessions ||
		r.Config.ThinkTime != "" && r.Config.ThinkTime != "0s" {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "UNVERIFIED",
			Reason: "The saved report omits fields needed to recompute this workload shape, or the workload is outside the single-step reference subset.",
		}
	}
	step := r.Steps[0]
	if r.SchemaVersion < 14 {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "UNVERIFIED",
			Reason: "The saved report schema predates connection-policy recording, so transport reuse cannot be established.",
		}
	}
	connectionPolicy := r.Config.ConnectionPolicy
	if connectionPolicy != "reuse" {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The fixed calibration uses reusable HTTP connections; the saved connection policy differs or is unknown.",
		}
	}
	if (step.Profile != "" && step.Profile != "default") || step.Name != "work" || step.Method != "GET" || step.Path != "/work" {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The saved step metadata differs from the single GET /work reference workload.",
		}
	}
	if record.SchemaVersion >= 3 {
		return calibration.AssessDeclaredWorkloadShape(config.Scenario{
			Name: "Saved controlled reference workload", BaseURL: r.Target,
			Users: r.Config.Users, Duration: r.Config.Duration, Timeout: r.Config.RequestTimeout,
			MaxRPS: r.Config.MaxRPS, Mode: r.Config.Mode, ArrivalRate: r.Config.ArrivalRate,
			ConnectionPolicy: connectionPolicy,
			Steps:            []config.Step{{Name: step.Name, Method: step.Method, Path: step.Path}},
		}, record)
	}
	// The fingerprint covers fields not serialized in Run reports (headers,
	// assertions, body templates, extracts and session details). Only accept the
	// reference subset above, where those fields are known to be empty by the
	// scenario gate; the persisted digest must still match the reconstructed
	// safe subset and the preflight digest.
	scenario := config.Scenario{
		Name: "Saved reference workload", BaseURL: "http://127.0.0.1", Users: 10,
		Duration: "5s", Timeout: "1s", MaxRPS: 50, P95LimitMS: 500,
		Mode: "arrival", ArrivalRate: 2,
		ConnectionPolicy: connectionPolicy,
		Steps:            []config.Step{{Name: step.Name, Method: step.Method, Path: step.Path}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil || validated.WorkloadFingerprint == "" {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "UNVERIFIED",
			Reason: "The saved step metadata could not be safely normalized for fingerprint verification.",
		}
	}
	if r.Config.WorkloadFingerprint != validated.WorkloadFingerprint {
		return calibration.Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The saved workload fingerprint does not match the reconstructable step metadata.",
		}
	}
	return calibration.AssessWorkloadFingerprint(
		validated.WorkloadFingerprint,
		r.Config.ProfileMixBasis,
		r.Config.ProfileMixReference,
		record,
	)
}

func responseStatusCount(d HTTPDiagnostics) int64 {
	var total int64
	for _, count := range d.StatusCodes {
		if count < 0 || int64(count) > math.MaxInt64-total {
			return -1
		}
		total += int64(count)
	}
	return total
}

func validResponseBodyDiagnostics(d ResponseBodyDiagnostics, responses int64, oversizedHTTPCount int) bool {
	invalidCounts := responses < 0 || oversizedHTTPCount < 0 ||
		d.CompleteObservations < 0 || d.IncompleteObservations < 0 || d.OversizedObservations < 0
	invalidSizes := d.TotalBytes < 0 || d.MinBytes < 0 || d.MaxBytes < 0
	if invalidCounts || invalidSizes {
		return false
	}
	if d.CompleteObservations > math.MaxInt64-d.IncompleteObservations ||
		d.CompleteObservations+d.IncompleteObservations > math.MaxInt64-d.OversizedObservations {
		return false
	}
	if d.CompleteObservations+d.IncompleteObservations+d.OversizedObservations != responses ||
		d.OversizedObservations != int64(oversizedHTTPCount) {
		return false
	}
	if d.CompleteObservations == 0 {
		return d.MinBytes == 0 && d.MaxBytes == 0 && d.TotalBytes == 0
	}
	if d.MinBytes > d.MaxBytes || d.MaxBytes > maxMeasuredResponseBodyBytes ||
		d.MinBytes > math.MaxInt64/d.CompleteObservations || d.MaxBytes > math.MaxInt64/d.CompleteObservations {
		return false
	}
	return d.TotalBytes >= d.CompleteObservations*d.MinBytes &&
		d.TotalBytes <= d.CompleteObservations*d.MaxBytes
}

func addResponseBodyDiagnostics(dst *ResponseBodyDiagnostics, src ResponseBodyDiagnostics) bool {
	add := func(left, right int64) (int64, bool) {
		if left < 0 || right < 0 || right > math.MaxInt64-left {
			return 0, false
		}
		return left + right, true
	}
	var ok bool
	if dst.CompleteObservations, ok = add(dst.CompleteObservations, src.CompleteObservations); !ok {
		return false
	}
	if dst.IncompleteObservations, ok = add(dst.IncompleteObservations, src.IncompleteObservations); !ok {
		return false
	}
	if dst.OversizedObservations, ok = add(dst.OversizedObservations, src.OversizedObservations); !ok {
		return false
	}
	if dst.TotalBytes, ok = add(dst.TotalBytes, src.TotalBytes); !ok {
		return false
	}
	if src.CompleteObservations > 0 {
		if dst.CompleteObservations == src.CompleteObservations || src.MinBytes < dst.MinBytes {
			dst.MinBytes = src.MinBytes
		}
		if src.MaxBytes > dst.MaxBytes {
			dst.MaxBytes = src.MaxBytes
		}
	}
	return true
}

func equalResponseBodyDiagnostics(a, b ResponseBodyDiagnostics) bool {
	return a.CompleteObservations == b.CompleteObservations &&
		a.IncompleteObservations == b.IncompleteObservations &&
		a.OversizedObservations == b.OversizedObservations &&
		a.MinBytes == b.MinBytes && a.MaxBytes == b.MaxBytes && a.TotalBytes == b.TotalBytes
}
