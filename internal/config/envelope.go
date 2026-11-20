package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

// Envelope modes recorded with every validated plan (ADR 0155).
const (
	EnvelopeStandard = "standard"
	EnvelopeExtended = "extended"
)

// Target classes accepted for an extended envelope. Production is never an
// accepted class: the extended envelope is for an isolated lab or a staging
// environment the operator controls.
const (
	TargetClassLab     = "lab"
	TargetClassStaging = "staging"
)

// EnvelopeLimits are inclusive per-run ceilings.
type EnvelopeLimits struct {
	MaxUsers       int           `json:"max_users"`
	MaxRPS         int           `json:"max_rps"`
	MaxArrivalRate int           `json:"max_arrival_rate"`
	MaxDuration    time.Duration `json:"-"`
}

// StandardEnvelopeLimits are the default G0-11 ceilings. They must not change
// without a new ADR; TestStandardRunSafetyCeilings pins them.
var StandardEnvelopeLimits = EnvelopeLimits{MaxUsers: 50, MaxRPS: 100, MaxArrivalRate: 100, MaxDuration: 3 * time.Minute}

// ExtendedEnvelopeHardLimits is the absolute maximum any opt-in may reach.
// There is no flag or configuration field that raises it further.
var ExtendedEnvelopeHardLimits = EnvelopeLimits{MaxUsers: 500, MaxRPS: 1000, MaxArrivalRate: 1000, MaxDuration: 15 * time.Minute}

// Default generator guardrails applied in extended mode when the operator
// does not configure stricter ones.
const (
	ExtendedDefaultMaxCPUPercent      = 85.0
	ExtendedDefaultMaxRSSBytes        = uint64(2 << 30)
	ExtendedDefaultConsecutiveSamples = 3
)

// ExtendedEnvelopeRequest is the explicit operator opt-in. Every field is
// required; the CLI maps flags onto it.
type ExtendedEnvelopeRequest struct {
	// Acknowledged records the separate acknowledgement that the target owner
	// authorized load above the standard ceilings.
	Acknowledged bool
	// TargetClass must be TargetClassLab or TargetClassStaging.
	TargetClass string
	// Guardrails may tighten the defaults; zero fields inherit defaults.
	Guardrails generatorhealth.Guardrails
}

// EnvelopeRecord is the validated envelope attached to a plan and copied into
// reports so readers can tell which ceilings applied.
type EnvelopeRecord struct {
	Mode        string
	TargetClass string
	Limits      EnvelopeLimits
	// Acknowledged mirrors the explicit extended-load acknowledgement.
	Acknowledged bool
	// CalibrationCoverage is set by the caller after the calibration gate
	// (READY is required before traffic in extended mode).
	CalibrationCoverage string
}

// ExceedsStandard reports whether the scenario uses any dimension above the
// standard ceilings.
func (v Validated) ExceedsStandard() bool {
	l := StandardEnvelopeLimits
	return v.Scenario.Users > l.MaxUsers || v.Scenario.MaxRPS > l.MaxRPS || v.Scenario.ArrivalRate > l.MaxArrivalRate || v.Duration > l.MaxDuration
}

func envelopeLimits(o Options) (EnvelopeLimits, EnvelopeRecord, error) {
	if o.ExtendedEnvelope == nil {
		return StandardEnvelopeLimits, EnvelopeRecord{Mode: EnvelopeStandard, Limits: StandardEnvelopeLimits}, nil
	}
	req := o.ExtendedEnvelope
	if !req.Acknowledged {
		return EnvelopeLimits{}, EnvelopeRecord{}, errors.New("extended envelope requires the explicit extended-load acknowledgement")
	}
	if req.TargetClass != TargetClassLab && req.TargetClass != TargetClassStaging {
		return EnvelopeLimits{}, EnvelopeRecord{}, fmt.Errorf("extended envelope target class must be %q or %q", TargetClassLab, TargetClassStaging)
	}
	return ExtendedEnvelopeHardLimits, EnvelopeRecord{Mode: EnvelopeExtended, TargetClass: req.TargetClass, Limits: ExtendedEnvelopeHardLimits, Acknowledged: true}, nil
}

// ExtendedGuardrails resolves the guardrails enforced in extended mode.
// Operators may tighten but never disable them.
func ExtendedGuardrails(requested generatorhealth.Guardrails) (generatorhealth.Guardrails, error) {
	g := requested
	if g.MaxProcessCPUPercent == 0 {
		g.MaxProcessCPUPercent = ExtendedDefaultMaxCPUPercent
	}
	if g.MaxProcessRSSBytes == 0 {
		g.MaxProcessRSSBytes = ExtendedDefaultMaxRSSBytes
	}
	if g.ConsecutiveSamples == 0 {
		g.ConsecutiveSamples = ExtendedDefaultConsecutiveSamples
	}
	if g.MaxProcessCPUPercent > ExtendedDefaultMaxCPUPercent || g.MaxProcessRSSBytes > ExtendedDefaultMaxRSSBytes || !g.Validate() {
		return generatorhealth.Guardrails{}, errors.New("extended envelope guardrails may only tighten the defaults (CPU <= 85%, RSS <= 2 GiB, 2-20 consecutive samples)")
	}
	return g, nil
}
