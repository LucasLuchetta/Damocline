package capacityplan

import (
	"fmt"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// CampaignAudit is a conservative, entirely offline replay of persisted
// campaign evidence. It is not an attestation of target identity, a
// confidence interval, or a sustainable-capacity certificate.
type CampaignAudit struct {
	SchemaVersion      int          `json:"schema_version"`
	State              string       `json:"state"`
	MeasurementQuality string       `json:"measurement_quality"`
	ObservedOutcome    string       `json:"observed_outcome"`
	RequiredPhases     int          `json:"required_phases"`
	Findings           []string     `json:"findings"`
	Phases             []PhaseAudit `json:"phases"`
}

// PhaseAudit recomputes stage classification, drift, and measurement quality
// from the saved run instead of trusting the recorded stage labels.
type PhaseAudit struct {
	Trial           int    `json:"trial"`
	Name            string `json:"name"`
	EvidenceState   string `json:"evidence_state"`
	Measurement     string `json:"measurement_quality"`
	Stability       string `json:"stability,omitempty"`
	Reason          string `json:"reason"`
}

func AuditCampaign(e campaign.Result) CampaignAudit {
	a := CampaignAudit{
		SchemaVersion: e.SchemaVersion,
		State: "INCONCLUSIVE",
		MeasurementQuality: report.QualityLimited,
		ObservedOutcome: e.Outcome,
		Findings: []string{
			"Observed HTTP SLO results are not a validated capacity ceiling or a count of independent human users.",
			"Ordinary external-target reports cannot certify generator/endpoint equivalence from loopback calibration alone.",
		},
	}
	if e.SchemaVersion != 1 && e.SchemaVersion != campaign.CurrentSchemaVersion {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "Unsupported campaign evidence schema.")
		return a
	}
	if err := validateEvidence(e); err != nil {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "Campaign consistency check: "+err.Error())
		return a
	}
	if len(e.Trials) == 0 {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "No campaign trials were recorded.")
		return a
	}

	invalid, inconclusive, limited := false, false, false
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			a.RequiredPhases++
			state, reason := campaign.ClassifyEvidence(phase.Run)
			quality := report.AssessQuality(phase.Run)
			stability := ""
			if phase.Name == "observation" {
				stability = campaign.AssessStability(phase.Run)
			}
			a.Phases = append(a.Phases, PhaseAudit{
				Trial: trial.Number, Name: phase.Name, EvidenceState: state,
				Measurement: quality.State, Stability: stability, Reason: reason,
			})
			if quality.State == report.QualityInvalid {
				invalid = true
				a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s has INVALID recomputed measurement accounting.", trial.Number, phase.Name))
			}
			if quality.State != report.QualityValid {
				limited = true
			}
			if phase.Run.Quality == nil {
				limited = true
				a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s has no persisted measurement-quality declaration.", trial.Number, phase.Name))
			} else if phase.Run.Quality.State != quality.State {
				invalid = true
				a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s claims %s measurement quality but recomputes to %s.", trial.Number, phase.Name, phase.Run.Quality.State, quality.State))
			}
			if e.SchemaVersion == campaign.CurrentSchemaVersion {
				if phase.EvidenceState != state || (phase.Name == "observation" && phase.Stability != stability) {
					invalid = true
					a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s has stale or altered stage evidence.", trial.Number, phase.Name))
				}
			}
			if state == "INVALID_DELIVERY" || state == "INSUFFICIENT_EVIDENCE" || stability == "NON_STATIONARY" || stability == "INSUFFICIENT_WINDOWS" {
				inconclusive = true
			}
		}
		if trial.Outcome != "PASS" && trial.Outcome != "FAIL" {
			inconclusive = true
		}
	}
	if a.RequiredPhases == 0 {
		inconclusive = true
		a.Findings = append(a.Findings, "No completed required phases are available for review.")
	}
	if invalid {
		a.State = "INVALID_EVIDENCE"
		a.MeasurementQuality = report.QualityInvalid
	} else {
		if !limited {
			a.MeasurementQuality = report.QualityValid
		}
		switch {
		case inconclusive || e.Outcome == "INCONCLUSIVE" || e.Outcome == "ABORTED":
			a.State = "INCONCLUSIVE"
		case e.Outcome == "FAIL":
			a.State = "OBSERVED_FAIL"
		case e.Outcome == "PASS":
			a.State = "OBSERVED_PASS_ONLY"
		default:
			a.State = "INCONCLUSIVE"
		}
	}
	if a.MeasurementQuality == report.QualityLimited {
		a.Findings = append(a.Findings, "Measurement quality is LIMITED; this is not a positive generator-capacity qualification.")
	}
	if a.State == "OBSERVED_PASS_ONLY" {
		a.Findings = append(a.Findings, "All recorded mandatory stages met their observed objectives; statistical and workload-specific certification remain separate decisions.")
	}
	return a
}
