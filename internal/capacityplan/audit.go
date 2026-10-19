package capacityplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// CampaignAudit is a conservative, entirely offline replay of persisted
// campaign evidence. It is not an attestation of target identity, a
// confidence interval, or a sustainable-capacity certificate.
type CampaignAudit struct {
	SchemaVersion       int          `json:"schema_version"`
	EvidenceID          string       `json:"evidence_id"`
	ScenarioFingerprint string       `json:"scenario_fingerprint,omitempty"`
	Scenario            string       `json:"scenario"`
	Target              string       `json:"target,omitempty"`
	StartedAt           string       `json:"started_at,omitempty"`
	DemandTargetUsers   int          `json:"demand_target_users,omitempty"`
	TestedBaseUsers     int          `json:"tested_base_users"`
	HostType            string       `json:"host_type"`
	PassingLevels       []int        `json:"observed_passing_levels"`
	FailingLevels       []int        `json:"observed_failing_levels"`
	TestedDuration      string       `json:"tested_duration"`
	Confidence          string       `json:"confidence"`
	LimitingWorkflows   []string     `json:"limiting_workflows"`
	Unknowns            []string     `json:"known_unknowns"`
	State               string       `json:"state"`
	MeasurementQuality  string       `json:"measurement_quality"`
	ObservedOutcome     string       `json:"observed_outcome"`
	RequiredPhases      int          `json:"required_phases"`
	Findings            []string     `json:"findings"`
	Phases              []PhaseAudit `json:"phases"`
}

// PhaseAudit recomputes stage classification, drift, and measurement quality
// from the saved run instead of trusting the recorded stage labels.
type PhaseAudit struct {
	Trial           int     `json:"trial"`
	Name            string  `json:"name"`
	EvidenceID      string  `json:"evidence_id"`
	StartedAt       string  `json:"started_at,omitempty"`
	ObservationSecs float64 `json:"observation_seconds,omitempty"`
	RunFingerprint  string  `json:"scenario_fingerprint,omitempty"`
	EvidenceState   string  `json:"evidence_state"`
	Measurement     string  `json:"measurement_quality"`
	Stability       string  `json:"stability,omitempty"`
	Reason          string  `json:"reason"`
}

// Assessment binds a saved campaign audit to an optional demand calculation.
// Confidence is deliberately qualitative until a dependence-aware method is
// implemented and validated for the workload.
type Assessment struct {
	SchemaVersion int           `json:"schema_version"`
	Audit         CampaignAudit `json:"audit"`
	Demand        *Result       `json:"demand,omitempty"`
	State         string        `json:"state"`
}

func BuildAssessment(e campaign.Result, goal *capacitygoal.Goal) (Assessment, error) {
	audit := AuditCampaign(e)
	assessment := Assessment{SchemaVersion: 1, Audit: audit, State: audit.State}
	if goal != nil {
		plan, err := Evaluate(e, *goal)
		if err != nil {
			return Assessment{}, err
		}
		assessment.Demand = &plan
		assessment.Audit.DemandTargetUsers = plan.TargetUsers
		if audit.State == "INVALID_EVIDENCE" {
			assessment.State = "INVALID_EVIDENCE"
		} else {
			assessment.State = plan.State
		}
	}
	return assessment, nil
}

func evidenceDigest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func AuditCampaign(e campaign.Result) CampaignAudit {
	a := CampaignAudit{
		SchemaVersion:   e.SchemaVersion,
		EvidenceID:      evidenceDigest(e),
		Scenario:        e.Scenario,
		TestedBaseUsers: e.BaseUsers,
		HostType:        "NOT_RECORDED",
		Confidence:      "NOT_ESTABLISHED",
		PassingLevels:   []int{}, FailingLevels: []int{}, LimitingWorkflows: []string{},
		Unknowns:           []string{"Dependence-aware confidence and stationarity are not established.", "Physical host type and equivalence to independent users are not verified."},
		State:              "INCONCLUSIVE",
		MeasurementQuality: report.QualityLimited,
		ObservedOutcome:    e.Outcome,
		Findings: []string{
			"Observed HTTP SLO results are not a validated capacity ceiling or a count of independent human users.",
			"Ordinary external-target reports cannot certify generator/endpoint equivalence from loopback calibration alone.",
		},
	}
	var duration time.Duration
	profiles := map[string]bool{}
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			if d, err := time.ParseDuration(phase.Run.Config.Duration); err == nil {
				duration += d
			}
			if phase.Name == "observation" {
				for _, p := range phase.Run.Profiles {
					if p.Journeys.Failures > 0 || p.Journeys.P95MS > phase.Run.Config.P95LimitMS || (p.Journeys.Total > 0 && float64(p.Journeys.Failures)/float64(p.Journeys.Total) > phase.Run.Config.MaxErrorRate) {
						profiles[p.Name] = true
					}
				}
				if len(phase.Run.Profiles) == 0 && (phase.Run.Journeys.Failures > 0 || phase.Run.Journeys.P95MS > phase.Run.Config.P95LimitMS || (phase.Run.Journeys.Total > 0 && float64(phase.Run.Journeys.Failures)/float64(phase.Run.Journeys.Total) > phase.Run.Config.MaxErrorRate)) {
					profiles["aggregate journey"] = true
				}
			}
		}
		if trial.Outcome == "PASS" && e.Outcome == "PASS" {
			a.PassingLevels = appendUniqueLevel(a.PassingLevels, e.BaseUsers)
		}
		if trial.Outcome == "FAIL" && e.Outcome == "FAIL" {
			a.FailingLevels = appendUniqueLevel(a.FailingLevels, e.BaseUsers)
		}
	}
	if e.Outcome != "PASS" && e.Outcome != "FAIL" {
		a.PassingLevels, a.FailingLevels = []int{}, []int{}
	}
	a.TestedDuration = duration.String()
	for name := range profiles {
		a.LimitingWorkflows = append(a.LimitingWorkflows, name)
	}
	sort.Strings(a.LimitingWorkflows)
	if len(e.Trials) > 0 && len(e.Trials[0].Phases) > 0 {
		a.Target = e.Trials[0].Phases[0].Run.Target
	}
	if len(e.Trials) > 0 {
		for _, phase := range e.Trials[0].Phases {
			if phase.Name != "warmup" {
				a.ScenarioFingerprint = phase.Run.Config.WorkloadFingerprint
				break
			}
		}
	}
	a.StartedAt = e.StartedAt
	if e.SchemaVersion != 1 && e.SchemaVersion != campaign.CurrentSchemaVersion {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "Unsupported campaign evidence schema.")
		return a
	}
	if err := validateEvidence(e); err != nil {
		a.State = "INVALID_EVIDENCE"
		a.PassingLevels, a.FailingLevels = []int{}, []int{}
		a.Findings = append(a.Findings, "Campaign consistency check: "+err.Error())
		return a
	}
	if len(e.Trials) == 0 {
		a.State = "INVALID_EVIDENCE"
		a.PassingLevels, a.FailingLevels = []int{}, []int{}
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
				EvidenceID: evidenceDigest(phase.Run), StartedAt: phase.Run.StartedAt,
				ObservationSecs: phase.Run.ObservationSeconds, RunFingerprint: phase.Run.Config.WorkloadFingerprint,
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
		limited = true
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

func appendUniqueLevel(levels []int, level int) []int {
	for _, existing := range levels {
		if existing == level {
			return levels
		}
	}
	return append(levels, level)
}
