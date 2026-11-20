package calibration

import (
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

// ExtendedCoverage is the calibration verdict for one exact extended-envelope
// level (ADR 0155). READY means a fresh, matching generator calibration
// covers the exact planned level; it is not a target-capacity claim.
type ExtendedCoverage struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// ExtendedCoverageAssessor assesses whether one calibration record covers the
// exact planned workload. Assessors return Status "READY" only for an exact
// match; any other status blocks the extended run before traffic.
type ExtendedCoverageAssessor func(s config.Scenario, rec Record, local Runtime, now time.Time) ExtendedCoverage

// ExtendedCoverageAssessors is the composition point for calibration artifact
// types. The built-in assessor uses the exact arrival-rate and closed-loop VU
// envelopes. A future workload-specific calibration artifact registers an
// additional assessor here; the first READY verdict wins, and an empty or
// all-blocked list refuses the run.
var ExtendedCoverageAssessors = []ExtendedCoverageAssessor{assessExactEnvelope}

// AssessExtendedEnvelope runs every registered assessor for the exact level.
func AssessExtendedEnvelope(s config.Scenario, rec Record, local Runtime, now time.Time) ExtendedCoverage {
	result := ExtendedCoverage{Status: "BLOCKED", Reason: "No registered calibration assessor covers this exact workload level."}
	for _, assess := range ExtendedCoverageAssessors {
		got := assess(s, rec, local, now)
		if got.Status == "READY" {
			return got
		}
		if got.Reason != "" {
			result.Reason = got.Reason
		}
	}
	return result
}

func assessExactEnvelope(s config.Scenario, rec Record, local Runtime, now time.Time) ExtendedCoverage {
	if s.Mode == "arrival" {
		envelope := AssessArrivalEnvelope(s, []int{s.ArrivalRate}, rec, local, now)
		out := ExtendedCoverage{Status: envelope.Status, Reason: "Exact arrival-rate calibration assessment."}
		if len(envelope.Rates) > 0 {
			out.Reason = envelope.Rates[0].Reason
		}
		return out
	}
	envelope := AssessClosedLoopEnvelope(s, []int{s.Users}, rec, local, now)
	out := ExtendedCoverage{Status: envelope.Status, Reason: "Exact closed-loop VU calibration assessment."}
	if len(envelope.Levels) > 0 && envelope.Levels[0].Reason != "" {
		out.Reason = envelope.Levels[0].Reason
	}
	return out
}
