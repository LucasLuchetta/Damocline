package report

import "time"

// EnvelopeSchemaVersion is the first run schema that may carry the
// load-envelope record (ADR 0155).
const EnvelopeSchemaVersion = 38

const (
	EnvelopeModeStandard = "standard"
	EnvelopeModeExtended = "extended"
	// EnvelopeStopGeneratorLimited marks an extended run stopped by a
	// generator CPU or RSS guardrail. The result describes the generator, not
	// the target.
	EnvelopeStopGeneratorLimited = "GENERATOR_LIMITED"
)

// Standard ceilings mirrored from internal/config so report readers can
// validate without importing the scenario parser.
const (
	standardMaxUsers       = 50
	standardMaxRPS         = 100
	standardMaxArrivalRate = 100
	standardMaxDurationSec = 180
	extendedMaxUsers       = 500
	extendedMaxRPS         = 1000
	extendedMaxArrivalRate = 1000
	extendedMaxDurationSec = 900
)

// Envelope records which per-run ceilings applied and, for the extended
// envelope, the explicit authorization and calibration gate that allowed it.
type Envelope struct {
	Mode                string `json:"mode"`
	MaxUsers            int    `json:"max_users"`
	MaxRPS              int    `json:"max_rps"`
	MaxArrivalRate      int    `json:"max_arrival_rate"`
	MaxDurationSeconds  int    `json:"max_duration_seconds"`
	TargetClass         string `json:"target_class,omitempty"`
	Acknowledged        bool   `json:"extended_load_acknowledged,omitempty"`
	CalibrationCoverage string `json:"calibration_coverage,omitempty"`
	ExceedsStandard     bool   `json:"exceeds_standard"`
	StopReason          string `json:"stop_reason,omitempty"`
}

// ValidEnvelope checks schema gating and internal consistency.
func ValidEnvelope(r Result) bool {
	e := r.Envelope
	if e == nil {
		return true
	}
	if r.SchemaVersion < EnvelopeSchemaVersion {
		return false
	}
	duration, err := time.ParseDuration(r.Config.Duration)
	if err != nil {
		return false
	}
	exceeds := r.Config.Users > standardMaxUsers || r.Config.MaxRPS > standardMaxRPS || r.Config.ArrivalRate > standardMaxArrivalRate || duration > standardMaxDurationSec*time.Second
	if duration > extendedMaxDurationSec*time.Second {
		return false
	}
	switch e.Mode {
	case EnvelopeModeStandard:
		if e.MaxUsers != standardMaxUsers || e.MaxRPS != standardMaxRPS || e.MaxArrivalRate != standardMaxArrivalRate || e.MaxDurationSeconds != standardMaxDurationSec ||
			e.TargetClass != "" || e.Acknowledged || e.CalibrationCoverage != "" || e.ExceedsStandard || e.StopReason != "" || exceeds {
			return false
		}
		return true
	case EnvelopeModeExtended:
		if e.MaxUsers != extendedMaxUsers || e.MaxRPS != extendedMaxRPS || e.MaxArrivalRate != extendedMaxArrivalRate || e.MaxDurationSeconds != extendedMaxDurationSec ||
			(e.TargetClass != "lab" && e.TargetClass != "staging") || !e.Acknowledged || e.CalibrationCoverage != "READY" ||
			r.Config.Users > extendedMaxUsers || r.Config.MaxRPS > extendedMaxRPS || r.Config.ArrivalRate > extendedMaxArrivalRate {
			return false
		}
		if exceeds && !e.ExceedsStandard {
			return false
		}
		if r.Generator == nil || r.Generator.Guardrails == nil || !r.Generator.Guardrails.Enabled() {
			return false
		}
		if (e.StopReason == EnvelopeStopGeneratorLimited) != (r.Generator.GuardrailTrigger != nil) {
			return false
		}
		return e.StopReason == "" || e.StopReason == EnvelopeStopGeneratorLimited
	}
	return false
}

// EnvelopeNote returns a reader-facing statement for extended-envelope runs.
func EnvelopeNote(r Result) string {
	e := r.Envelope
	if e == nil || e.Mode != EnvelopeModeExtended {
		return ""
	}
	note := "This run used the opt-in extended load envelope (target class " + e.TargetClass + ", calibration coverage " + e.CalibrationCoverage + ")"
	if e.ExceedsStandard {
		note += "; results above the standard 50 VU / 100 starts-per-second / 3 minute ceiling were produced under the extended envelope"
	}
	if e.StopReason == EnvelopeStopGeneratorLimited {
		note += "; the run was stopped as GENERATOR_LIMITED by a generator resource guardrail and describes the load generator, not the target"
	}
	return note + "."
}

// StandardMaxUsers is the standard closed-loop VU ceiling mirrored for
// multi-level artifact readers (explore and campaign, ADR 0157).
const StandardMaxUsers = standardMaxUsers

// ExtendedMaxUsers is the extended-envelope hard VU maximum.
const ExtendedMaxUsers = extendedMaxUsers

// NewAggregateEnvelope builds the envelope recorded by a multi-level
// artifact (explore or campaign). maxLevel is the highest planned VU level.
func NewAggregateEnvelope(extended bool, targetClass string, maxLevel int) *Envelope {
	if !extended {
		return &Envelope{Mode: EnvelopeModeStandard, MaxUsers: standardMaxUsers, MaxRPS: standardMaxRPS, MaxArrivalRate: standardMaxArrivalRate, MaxDurationSeconds: standardMaxDurationSec}
	}
	return &Envelope{Mode: EnvelopeModeExtended, MaxUsers: extendedMaxUsers, MaxRPS: extendedMaxRPS, MaxArrivalRate: extendedMaxArrivalRate, MaxDurationSeconds: extendedMaxDurationSec,
		TargetClass: targetClass, Acknowledged: true, CalibrationCoverage: "READY", ExceedsStandard: maxLevel > standardMaxUsers}
}

// ValidAggregateEnvelope checks a multi-level artifact envelope against the
// highest planned VU level. Aggregate envelopes never carry a stop reason;
// generator-limited stops are recorded on the affected run.
func ValidAggregateEnvelope(e *Envelope, maxLevel int) bool {
	if e == nil || e.StopReason != "" || maxLevel < 1 {
		return false
	}
	switch e.Mode {
	case EnvelopeModeStandard:
		return *e == *NewAggregateEnvelope(false, "", maxLevel) && maxLevel <= standardMaxUsers
	case EnvelopeModeExtended:
		return (e.TargetClass == "lab" || e.TargetClass == "staging") && *e == *NewAggregateEnvelope(true, e.TargetClass, maxLevel) && maxLevel <= extendedMaxUsers
	}
	return false
}

// RunMatchesAggregateEnvelope reports whether an embedded run carries the
// same envelope mode as its parent artifact. Runs that predate the envelope
// schema are accepted only by standard parents.
func RunMatchesAggregateEnvelope(parent *Envelope, run Result) bool {
	if run.Envelope == nil {
		return parent.Mode == EnvelopeModeStandard && run.SchemaVersion < EnvelopeSchemaVersion
	}
	return run.Envelope.Mode == parent.Mode && run.Envelope.TargetClass == parent.TargetClass && ValidEnvelope(run)
}

// ArrivalAggregateEnvelope records workers and scheduled rates separately.
func ArrivalAggregateEnvelope(targetClass string, workers, starts, requestCeiling int) *Envelope {
	level := workers
	if starts > standardMaxRPS || requestCeiling > standardMaxRPS {
		level = max(level, standardMaxUsers+1)
	}
	return NewAggregateEnvelope(true, targetClass, level)
}

func ValidArrivalAggregateEnvelope(e *Envelope, workers, starts, requestCeiling int) bool {
	return e != nil && e.Mode == EnvelopeModeExtended &&
		(e.TargetClass == "lab" || e.TargetClass == "staging") &&
		workers >= 1 && workers <= extendedMaxUsers &&
		starts >= 1 && starts <= extendedMaxRPS && requestCeiling >= starts && requestCeiling <= extendedMaxRPS &&
		*e == *ArrivalAggregateEnvelope(e.TargetClass, workers, starts, requestCeiling)
}

// AggregateEnvelopeNote is the reader-facing statement for an artifact that
// used the extended envelope.
func AggregateEnvelopeNote(e *Envelope) string {
	if e == nil || e.Mode != EnvelopeModeExtended {
		return ""
	}
	note := "This artifact used the opt-in extended load envelope (target class " + e.TargetClass + ", every planned level calibration coverage READY, generator guardrails enforced)"
	if e.ExceedsStandard {
		note += "; levels above the standard 50 VU ceiling were tested under the extended envelope and describe only this workload, generator and environment"
	}
	return note + "."
}
