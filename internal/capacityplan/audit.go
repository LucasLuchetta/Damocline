package capacityplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// CampaignAudit is a conservative, entirely offline replay of persisted
// campaign evidence. It is not an attestation of target identity, a
// confidence interval, or a sustainable-capacity certificate.
type CampaignAudit struct {
	SchemaVersion       int              `json:"schema_version"`
	EvidenceID          string           `json:"evidence_id"`
	ScenarioFingerprint string           `json:"scenario_fingerprint,omitempty"`
	Scenario            string           `json:"scenario"`
	Target              string           `json:"target,omitempty"`
	StartedAt           string           `json:"started_at,omitempty"`
	DemandTargetUsers   int              `json:"demand_target_users,omitempty"`
	TestedBaseUsers     int              `json:"tested_base_users"`
	HostType            string           `json:"host_type"`
	PassingLevels       []int            `json:"observed_passing_levels"`
	FailingLevels       []int            `json:"observed_failing_levels"`
	TestedDuration      string           `json:"tested_duration"`
	Confidence          string           `json:"confidence"` // Legacy campaign-level marker; use Assessment.Confidence for dimensions.
	LimitingWorkflows   []string         `json:"limiting_workflows"`
	WorkflowBreaches    []WorkflowBreach `json:"workflow_breaches,omitempty"`
	Unknowns            []string         `json:"known_unknowns"`
	State               string           `json:"state"`
	MeasurementQuality  string           `json:"measurement_quality"`
	ObservedOutcome     string           `json:"observed_outcome"`
	RequiredPhases      int              `json:"required_phases"`
	Findings            []string         `json:"findings"`
	Phases              []PhaseAudit     `json:"phases"`
}

// WorkflowBreach identifies the required campaign phase and observed SLO
// dimensions that caused a named workflow to breach an objective.
type WorkflowBreach struct {
	Trial        int      `json:"trial"`
	Phase        string   `json:"phase"`
	TestedUsers  int      `json:"tested_users"`
	Workflow     string   `json:"workflow"`
	Reasons      []string `json:"reasons"`
	P95MS        float64  `json:"p95_ms"`
	P95LimitMS   float64  `json:"p95_limit_ms"`
	Failures     int      `json:"failures"`
	Total        int      `json:"total"`
	ErrorRate    float64  `json:"error_rate"`
	MaxErrorRate float64  `json:"max_error_rate"`
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
	SchemaVersion int                  `json:"schema_version"`
	Audit         CampaignAudit        `json:"audit"`
	Confidence    ConfidenceDimensions `json:"confidence_dimensions"`
	Demand        *Result              `json:"demand,omitempty"`
	State         string               `json:"state"`
}

// ConfidenceDimensions keeps measurement evidence, workload representativeness,
// and decision confidence separate. A favorable dimension never upgrades
// another dimension or the assessment state.
type ConfidenceDimensions struct {
	Measurement                string   `json:"measurement_confidence"`
	WorkloadRepresentativeness string   `json:"workload_representativeness"`
	Decision                   string   `json:"decision_confidence"`
	Findings                   []string `json:"findings"`
}

func BuildAssessment(e campaign.Result, goal *capacitygoal.Goal) (Assessment, error) {
	audit := AuditCampaign(e)
	assessment := Assessment{SchemaVersion: 1, Audit: audit, Confidence: assessConfidence(e, audit), State: audit.State}
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

func assessConfidence(e campaign.Result, audit CampaignAudit) ConfidenceDimensions {
	dimensions := ConfidenceDimensions{
		Measurement: "NOT_ASSESSED", WorkloadRepresentativeness: "NOT_ASSESSED",
		Decision: "NOT_ESTABLISHED", Findings: []string{},
	}
	if audit.State == "INVALID_EVIDENCE" {
		dimensions.Measurement = "INVALID"
	} else {
		switch audit.MeasurementQuality {
		case report.QualityValid:
			dimensions.Measurement = "CONTROLLED_ENVELOPE_ONLY"
			dimensions.Findings = append(dimensions.Findings, "Measurement accounting is within a validated envelope; this does not establish statistical confidence or target equivalence.")
		case report.QualityLimited:
			dimensions.Measurement = "LIMITED"
			dimensions.Findings = append(dimensions.Findings, "Measurement confidence is limited because at least one phase is outside the validated measurement envelope.")
		case report.QualityInvalid:
			dimensions.Measurement = "INVALID"
		}
	}

	basis, reference := "", ""
	declared, missing, inconsistent := false, false, false
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			currentBasis := strings.TrimSpace(phase.Run.Config.ProfileMixBasis)
			currentReference := strings.TrimSpace(phase.Run.Config.ProfileMixReference)
			if currentBasis == "" || currentReference == "" {
				missing = true
				continue
			}
			if !declared {
				basis, reference, declared = currentBasis, currentReference, true
				continue
			}
			if currentBasis != basis || currentReference != reference {
				inconsistent = true
			}
		}
	}
	if inconsistent {
		dimensions.WorkloadRepresentativeness = "INCONSISTENT_DECLARATION"
		dimensions.Findings = append(dimensions.Findings, "Profile-mix basis or reference changes between required phases; workload representativeness is not assessed.")
	} else if declared && !missing {
		dimensions.WorkloadRepresentativeness = "DECLARED_UNVERIFIED"
		dimensions.Findings = append(dimensions.Findings, fmt.Sprintf("Profile-mix basis %q and reference %q are declared, but no independent evidence verifies that they represent actual demand.", basis, reference))
	} else {
		dimensions.Findings = append(dimensions.Findings, "Workload representativeness is not assessed because a complete profile-mix basis and reference are not recorded in every required phase.")
	}
	if audit.State == "INVALID_EVIDENCE" {
		dimensions.WorkloadRepresentativeness = "NOT_ASSESSED"
		dimensions.Decision = "INVALID_EVIDENCE"
		dimensions.Findings = append(dimensions.Findings, "Source campaign evidence is invalid; workload representativeness cannot be assessed from it.")
	}
	dimensions.Findings = append(dimensions.Findings, "Decision confidence is not established: dependence, stationarity, workload equivalence, and host identity are not validated together.")
	return dimensions
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
			for _, breach := range workflowBreaches(trial.Number, phase.Name, phase.Run.Config.Users, phase.Run.Profiles, phase.Run.Journeys, phase.Run.Config.P95LimitMS, phase.Run.Config.MaxErrorRate) {
				a.WorkflowBreaches = append(a.WorkflowBreaches, breach)
				profiles[breach.Workflow] = true
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
	if e.SchemaVersion < 1 || e.SchemaVersion > campaign.CurrentSchemaVersion {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "Unsupported campaign evidence schema.")
		clearWorkflowBreaches(&a)
		return a
	}
	if err := validateEvidence(e); err != nil {
		a.State = "INVALID_EVIDENCE"
		a.PassingLevels, a.FailingLevels = []int{}, []int{}
		a.Findings = append(a.Findings, "Campaign consistency check: "+err.Error())
		clearWorkflowBreaches(&a)
		return a
	}
	if len(e.Trials) == 0 {
		a.State = "INVALID_EVIDENCE"
		a.PassingLevels, a.FailingLevels = []int{}, []int{}
		a.Findings = append(a.Findings, "No campaign trials were recorded.")
		clearWorkflowBreaches(&a)
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
			if phase.Name == "observation" || phase.Name == "soak" {
				stability = campaign.AssessStabilityWithPolicy(phase.Run, e.EffectiveStabilityPolicy())
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
			if e.SchemaVersion >= 2 {
				if phase.EvidenceState != state || ((phase.Name == "observation" || phase.Name == "soak") && phase.Stability != stability) {
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
		clearWorkflowBreaches(&a)
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

func workflowBreaches(trial int, phase string, users int, profiles []report.ProfileResult, aggregate report.MetricSet, p95LimitMS, maxErrorRate float64) []WorkflowBreach {
	if len(profiles) == 0 {
		return workflowBreach(trial, phase, users, "aggregate journey", aggregate, p95LimitMS, maxErrorRate)
	}
	breaches := make([]WorkflowBreach, 0)
	for _, profile := range profiles {
		breaches = append(breaches, workflowBreach(trial, phase, users, profile.Name, profile.Journeys, p95LimitMS, maxErrorRate)...)
	}
	return breaches
}

func workflowBreach(trial int, phase string, users int, name string, metrics report.MetricSet, p95LimitMS, maxErrorRate float64) []WorkflowBreach {
	if name == "" {
		name = "unnamed workflow"
	}
	reasons := make([]string, 0, 2)
	if p95LimitMS > 0 && !math.IsNaN(metrics.P95MS) && !math.IsInf(metrics.P95MS, 0) && metrics.P95MS > p95LimitMS {
		reasons = append(reasons, "P95_ABOVE_LIMIT")
	}
	errorRate := 0.0
	validCounts := metrics.Total > 0 && metrics.Failures >= 0 && metrics.Failures <= metrics.Total && metrics.Success == metrics.Total-metrics.Failures
	if validCounts {
		errorRate = float64(metrics.Failures) / float64(metrics.Total)
		if maxErrorRate >= 0 && maxErrorRate <= 1 && !math.IsNaN(maxErrorRate) && !math.IsInf(maxErrorRate, 0) && errorRate > maxErrorRate {
			reasons = append(reasons, "ERROR_RATE_ABOVE_LIMIT")
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	return []WorkflowBreach{{
		Trial: trial, Phase: phase, TestedUsers: users, Workflow: name, Reasons: reasons,
		P95MS: metrics.P95MS, P95LimitMS: p95LimitMS,
		Failures: metrics.Failures, Total: metrics.Total, ErrorRate: errorRate, MaxErrorRate: maxErrorRate,
	}}
}

func clearWorkflowBreaches(a *CampaignAudit) {
	a.LimitingWorkflows = []string{}
	a.WorkflowBreaches = nil
}

func appendUniqueLevel(levels []int, level int) []int {
	for _, existing := range levels {
		if existing == level {
			return levels
		}
	}
	return append(levels, level)
}
