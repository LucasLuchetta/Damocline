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
