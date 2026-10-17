package report

import "fmt"

// Quality describes whether arithmetic delivery/accounting evidence can be
// trusted. It is orthogonal to SLO Outcome. Positive calibration equivalence
// is not yet available, so this first slice intentionally never produces
// VALID_WITHIN_ENVELOPE for an arbitrary application run.
type Quality struct {
	State string `json:"state"`
	Findings []string `json:"findings"`
}

const (
	QualityValid = "VALID_WITHIN_ENVELOPE"
	QualityLimited = "LIMITED"
	QualityInvalid = "INVALID"
)

// AssessQuality uses only aggregate invariants and reported generator
// observations. It does NOT infer network quality or target saturation.
// No secrets, hostnames, routes or individual response content are reported.
func AssessQuality(r Result) *Quality {
	q:=&Quality{State:QualityLimited}
	invalidate:=func(reason string) {
		q.State=QualityInvalid
		q.Findings=append(q.Findings,reason)
	}
	limit:=func(reason string) {q.Findings=append(q.Findings,reason)}
	if r.Metrics.Total<0 || r.Metrics.Success<0 || r.Metrics.Failures<0 ||
		r.Metrics.Cancelled<0 || r.Metrics.Success+r.Metrics.Failures!=r.Metrics.Total {
		invalidate("Request aggregate accounting is inconsistent.")
	}
	if r.Journeys.Total<0 || r.Journeys.Success<0 || r.Journeys.Failures<0 ||
		r.Journeys.Cancelled<0 || r.Journeys.Success+r.Journeys.Failures!=r.Journeys.Total {
		invalidate("Completed-journey accounting is inconsistent.")
	}
	if r.IterationsStarted<0 || r.IterationsCompleted<0 ||
		r.IterationsCompleted>r.IterationsStarted || r.ArrivalsScheduled<0 ||
		r.ArrivalsDropped<0 || r.ArrivalsDroppedLate<0 || r.ArrivalsDroppedBusy<0 {
		invalidate("Negative or nonconservative iteration/arrival counters.")
	}
	if r.Config.Mode=="arrival" {
		if r.IterationsStarted+r.ArrivalsDropped!=r.ArrivalsScheduled ||
			r.ArrivalsDropped!=r.ArrivalsDroppedLate+r.ArrivalsDroppedBusy {
			invalidate("Scheduled, started and dropped arrivals do not reconcile.")
		}
	}
	if r.ObservationSeconds<0 || r.ElapsedSeconds<=0 {
		invalidate("Observation or elapsed time is invalid.")
	}
	if r.Metrics.Cancelled>0 || r.Journeys.Cancelled>0 ||
		r.ArrivalsDropped>0 || r.Outcome=="ABORTED" {
		limit("Work was cancelled or offered demand was not completely delivered.")
	}
	if r.Generator==nil || r.Generator.Samples<2 || !r.Generator.GoCPUValid {
		limit("Generator runtime sampling is unavailable or inadequate.")
	} else if r.Generator.GoCPUUtilizationRatio>=0.9 {
		limit("Generator runtime CPU may be saturated.")
	}
	if r.Generator==nil || r.Generator.ProcessSamples<2 || !r.Generator.ProcessCPUValid {
		limit("Generator process measurements are missing or unsupported.")
	}
	if r.Config.MaxRPS>0 && r.WindowSeconds()>0 &&
		float64(r.Metrics.Total)/r.WindowSeconds()>0.90*float64(r.Config.MaxRPS) {
		limit("Observed request throughput is near the configured client rate limit.")
	}
	if r.Config.Users<1 || r.Config.MaxRPS<1 {
		invalidate("Required bounded actor/rate configuration is absent.")
	}
	for _,s:=range r.Steps {
		if s.Metrics.Total<0 || s.Metrics.Success+s.Metrics.Failures!=s.Metrics.Total {
			invalidate(fmt.Sprintf("Named-step accounting is inconsistent (step index checked without route disclosure)."))
			break
		}
	}
	limit("No equivalent, fresh, host-validated generator preflight was attached; workload-specific capacity is not certified.")
	return q
}
