package report

import "strings"

// Signal describes an observed, narrowly scoped symptom, NOT a cause. There
// is no connection-pool attribution without direct instrumentation.
type Signal struct {
	Code     string `json:"code"`
	Origin   string `json:"origin"`
	Evidence string `json:"evidence"`
}

// DetectSignals never copies untrusted failure text, response bodies, target
// addresses, customer route names or credentials into diagnosis output.
func DetectSignals(r Result) []Signal {
	var signals []Signal
	add := func(code, origin, evidence string) {
		signals = append(signals, Signal{Code: code, Origin: origin, Evidence: evidence})
	}
	if r.Generator == nil || r.Generator.Samples < 2 || !r.Generator.GoCPUValid ||
		!r.Generator.ProcessCPUValid {
		add("GENERATOR_TELEMETRY_UNAVAILABLE", "client", "Generator CPU/process evidence is missing or unsupported; resource attribution is unknown.")
	} else if r.Generator.GoCPUUtilizationRatio >= 0.9 ||
		r.Generator.PeakProcessCPUUtilizationRatio >= 0.9 {
		add("GENERATOR_CPU_PRESSURE", "client", "Sampled local generator CPU is high; this is not proof of a target bottleneck.")
	}
	if r.Config.MaxRPS > 0 && r.WindowSeconds() > 0 &&
		float64(r.Metrics.Total)/r.WindowSeconds() >= 0.90*float64(r.Config.MaxRPS) {
		add("CLIENT_RATE_CEILING_NEAR", "client", "Observed completions approach max_rps; configured client throttle may mask target headroom.")
	}
	if r.ArrivalsDroppedBusy > 0 {
		add("CLIENT_WORKERS_BUSY", "client", "Synthetic journey arrivals were dropped because no load worker was available.")
	}
	if r.ArrivalsDroppedLate > 0 {
		add("CLIENT_SCHEDULER_LATE", "client", "Planned arrivals missed their start deadlines; intended load was not delivered.")
	}
	if r.SchemaVersion >= 28 {
		var waitCount int
		for _, step := range r.Steps {
			waitCount += step.HTTPTrace.ConnectionWaitCount
		}
		if waitCount > 0 {
			add("CLIENT_CONNECTION_ACQUISITION_WAIT", "client_transport", "At least one reused-connection acquisition exceeded 10 ms; client-side transport queueing is possible, but pool exhaustion is not proven.")
		}
	}
	if r.ArrivalsCancelledBeforeAdmission != nil && *r.ArrivalsCancelledBeforeAdmission > 0 {
		add("CLIENT_ARRIVALS_CANCELLED", "run_control", "Due arrivals were not admitted because the run was externally cancelled before its schedule ended.")
	}
	if r.SchemaVersion >= 17 && r.Config.Mode == "arrival" && r.ArrivalsAdmitted != nil && *r.ArrivalsAdmitted != r.IterationsStarted {
		add("CLIENT_ADMISSION_MISMATCH", "arrival_ledger", "Scheduler admissions do not reconcile with worker-started iterations.")
	}
	foundServerStatus := false
	for reason, n := range r.Metrics.Reasons {
		if n <= 0 || !strings.HasPrefix(reason, "http_") || len(reason) != 8 {
			continue
		}
		code := reason[5:]
		if len(code) != 3 {
			continue
		}
		if (code[0] == '4' || code[0] == '5') &&
			code[1] >= '0' && code[1] <= '9' &&
			code[2] >= '0' && code[2] <= '9' {
			foundServerStatus = true
			break
		}
	}
	if foundServerStatus {
		add("HTTP_ERROR_RESPONSE", "http_observation", "The target returned non-success HTTP status codes; this alone does not identify CPU, database, pool or infrastructure limits.")
	}
	if len(signals) == 0 {
		add("BOTTLENECK_NOT_ESTABLISHED", "unknown", "HTTP observations alone do not identify resource limits or root cause; connection-pool and database metrics were not measured.")
	}
	return signals
}
