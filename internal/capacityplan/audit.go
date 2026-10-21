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
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

// CampaignAudit is a conservative, entirely offline replay of persisted
// campaign evidence. It is not an attestation of target identity, a
// confidence interval, or a sustainable-capacity certificate.
type CampaignAudit struct {
	SchemaVersion           int                         `json:"schema_version"`
	ExecutionState          string                      `json:"execution_state,omitempty"`
	EvidenceID              string                      `json:"evidence_id"`
	SourceFileSHA256        string                      `json:"source_file_sha256,omitempty"`
	ScenarioFingerprint     string                      `json:"scenario_fingerprint,omitempty"`
	WorkloadSource          *campaign.WorkloadSource    `json:"workload_source,omitempty"`
	Scenario                string                      `json:"scenario"`
	Target                  string                      `json:"target,omitempty"`
	StartedAt               string                      `json:"started_at,omitempty"`
	DemandTargetUsers       int                         `json:"demand_target_users,omitempty"`
	TestedBaseUsers         int                         `json:"tested_base_users"`
	HostType                string                      `json:"host_type"`
	GeneratorRuntimeStatus  string                      `json:"generator_runtime_identity_status"`
	GeneratorCapacityStatus string                      `json:"generator_capacity_status"`
	GeneratorEnvironments   []GeneratorEnvironment      `json:"generator_environments"`
	PassingLevels           []int                       `json:"observed_passing_levels"`
	FailingLevels           []int                       `json:"observed_failing_levels"`
	TestedDuration          string                      `json:"tested_duration"`
	Confidence              string                      `json:"confidence"` // Legacy campaign-level marker; use Assessment.Confidence for dimensions.
	LimitingWorkflows       []string                    `json:"limiting_workflows"`
	WorkflowBreaches        []WorkflowBreach            `json:"workflow_breaches,omitempty"`
	ObservationTrials       []ObservationTrial          `json:"observation_trials,omitempty"`
	ObservationSpread       *campaign.Variability       `json:"recomputed_observation_spread,omitempty"`
	SpreadConsistency       string                      `json:"observation_spread_consistency"`
	Repeatability           RepeatabilityEvidence       `json:"repeatability_evidence"`
	Unknowns                []string                    `json:"known_unknowns"`
	State                   string                      `json:"state"`
	MeasurementQuality      string                      `json:"measurement_quality"`
	ObservedOutcome         string                      `json:"observed_outcome"`
	RequiredPhases          int                         `json:"required_phases"`
	Objectives              *campaign.ObjectiveContract `json:"predeclared_objectives,omitempty"`
	Findings                []string                    `json:"findings"`
	Phases                  []PhaseAudit                `json:"phases"`
}

// GeneratorEnvironment records path-free runtime identity and effective CPU
// capacity observed by one campaign phase. Host model and physical identity
// are intentionally not inferred.
type GeneratorEnvironment struct {
	Trial             int                       `json:"trial"`
	Phase             string                    `json:"phase"`
	Identity          *report.GeneratorIdentity `json:"identity,omitempty"`
	LogicalCPUs       int                       `json:"logical_cpus,omitempty"`
	GOMAXPROCS        int                       `json:"gomaxprocs,omitempty"`
	CPUCapacityCores  float64                   `json:"cpu_capacity_cores,omitempty"`
	CPUCapacitySource string                    `json:"cpu_capacity_source,omitempty"`
	CPUCapacityKnown  bool                      `json:"cpu_capacity_known"`
}

// RepeatabilityEvidence summarizes the observed fraction of terminal campaign
// trials that passed. Its exact interval is conditional on independent,
// identically distributed repeat trials and is not a capacity confidence.
type RepeatabilityEvidence struct {
	Status          string  `json:"status"`
	EvaluatedTrials int     `json:"evaluated_trials"`
	PassingTrials   int     `json:"passing_trials"`
	ExcludedTrials  int     `json:"excluded_trials"`
	Lower95         float64 `json:"pass_probability_lower_95"`
	Upper95         float64 `json:"pass_probability_upper_95"`
	Method          string  `json:"method"`
	Assumption      string  `json:"assumption"`
}

// ObservationTrial exposes one completed base-load observation as descriptive
// evidence. It is not a confidence interval and does not treat requests as
// independent experiments.
type ObservationTrial struct {
	Trial                    int      `json:"trial"`
	Requests                 int      `json:"requests"`
	RequestP95MS             float64  `json:"request_p95_ms"`
	RequestP99MS             float64  `json:"request_p99_ms"`
	RequestP99IntervalState  string   `json:"request_p99_interval_state"`
	RequestErrorRate         *float64 `json:"request_error_rate,omitempty"`
	CompletedJourneys        int      `json:"completed_journeys"`
	JourneyP95MS             float64  `json:"journey_p95_ms"`
	JourneyP99MS             float64  `json:"journey_p99_ms"`
	JourneyP99IntervalState  string   `json:"journey_p99_interval_state"`
	JourneyErrorRate         *float64 `json:"journey_error_rate,omitempty"`
	SuccessfulJourneysPerSec float64  `json:"successful_journeys_per_second"`
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
// Decision confidence remains qualitative until dependence and representativeness
// are validated for the workload; repeatability evidence is a separate conditional
// interval and does not establish decision confidence.
type Assessment struct {
	SchemaVersion int                  `json:"schema_version"`
	Auditor       AuditorIdentity      `json:"auditor"`
	Audit         CampaignAudit        `json:"audit"`
	Confidence    ConfidenceDimensions `json:"confidence_dimensions"`
	DemandInputs  *capacitygoal.Goal   `json:"demand_inputs,omitempty"`
	Demand        *Result              `json:"demand,omitempty"`
	State         string               `json:"state"`
}

// AuditorIdentity identifies the tool build that interpreted the campaign.
// The executable digest identifies exact bytes, while the version is a
// human-readable label; neither attests who built or distributed the binary.
type AuditorIdentity struct {
	Version          string `json:"version"`
	ExecutableSHA256 string `json:"executable_sha256"`
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
	assessment := Assessment{SchemaVersion: 6, Audit: audit, Confidence: assessConfidence(e, audit), State: audit.State}
	if goal != nil {
		plan, err := Evaluate(e, *goal)
		if err != nil {
			return Assessment{}, err
		}
		assessment.Demand = &plan
		goalCopy := *goal
		assessment.DemandInputs = &goalCopy
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
	original := e
	inProgress := e.ExecutionState == "RUNNING"
	if inProgress && e.PendingTrial != nil {
		e = checkpointAsAbortedEvidence(e)
	}
	a := CampaignAudit{
		SchemaVersion:          e.SchemaVersion,
		Objectives:             e.Objectives,
		ExecutionState:         original.ExecutionState,
		EvidenceID:             evidenceDigest(original),
		Scenario:               e.Scenario,
		WorkloadSource:         e.WorkloadSource,
		TestedBaseUsers:        e.BaseUsers,
		HostType:               "NOT_RECORDED",
		GeneratorRuntimeStatus: "UNKNOWN", GeneratorCapacityStatus: "UNKNOWN", GeneratorEnvironments: []GeneratorEnvironment{},
		Confidence:    "NOT_ESTABLISHED",
		PassingLevels: []int{}, FailingLevels: []int{}, LimitingWorkflows: []string{},
		Unknowns:           []string{"Dependence-aware confidence and stationarity are not established.", "Physical host type and equivalence to independent users are not verified."},
		State:              "INCONCLUSIVE",
		MeasurementQuality: report.QualityLimited,
		ObservedOutcome:    original.Outcome,
		Findings: []string{
			"Observed HTTP SLO results are not a validated capacity ceiling or a count of independent human users.",
			"Ordinary external-target reports cannot certify generator/endpoint equivalence from loopback calibration alone.",
		},
	}
	a.GeneratorEnvironments, a.GeneratorRuntimeStatus, a.GeneratorCapacityStatus = summarizeGeneratorEnvironments(e)
	a.ObservationTrials, a.ObservationSpread = recomputeObservationEvidence(e)
	if a.ObservationSpread != nil && e.SchemaVersion >= 14 {
		// Recompute the robust repeat-level summary from saved observation
		// phases; persisted aggregate fields are never treated as authority.
		var p95s []float64
		for _, observation := range a.ObservationTrials {
			p95s = append(p95s, observation.RequestP95MS)
		}
		if median, mad, ok := campaign.SummarizeRobustRepeatSpread(p95s); ok {
			a.ObservationSpread.RequestP95MedianMS = median
			a.ObservationSpread.RequestP95MADMS = mad
			a.ObservationSpread.RequestP95RobustLowerMS = math.Max(0, median-3*mad)
			a.ObservationSpread.RequestP95RobustUpperMS = median + 3*mad
			a.ObservationSpread.RequestP95RobustMethod = "MEDIAN_PLUS_MINUS_3_MAD"
			a.ObservationSpread.RequestP95RobustAssumption = "INDEPENDENT_OBSERVATION_PHASES_DESCRIPTIVE_ONLY"
		}
	}
	a.SpreadConsistency = observationSpreadConsistency(e, a.ObservationSpread)
	a.Repeatability = summarizeRepeatability(e.Trials)
	if a.SpreadConsistency == "MISMATCHED" {
		a.State = "INVALID_EVIDENCE"
		a.Findings = append(a.Findings, "Persisted observation variability does not match the saved independent observation phases.")
	}
	if a.GeneratorRuntimeStatus == "CHANGED" || a.GeneratorCapacityStatus == "CHANGED" {
		a.Findings = append(a.Findings, "Generator runtime identity or effective CPU capacity changed between campaign phases; results are not a same-generator-environment repeat.")
	}
	if a.GeneratorRuntimeStatus == "UNKNOWN" || a.GeneratorCapacityStatus == "UNKNOWN" {
		a.Findings = append(a.Findings, "Generator runtime identity or effective CPU capacity is not recorded for every campaign phase; environment equivalence is not established.")
	}
	if inProgress {
		a.Findings = append(a.Findings, "This is a RUNNING checkpoint. Its final campaign and pending-trial outcomes are unknown; the saved phases are retained for review only.")
	}
	if a.Repeatability.Status == "CONDITIONAL_INTERVAL" {
		a.Findings = append(a.Findings, fmt.Sprintf("Across %d evaluable repeat trials, %d passed; the exact 95%% interval for the same-condition trial pass probability is %.1f%%–%.1f%%, conditional on independent stationary trials.",
			a.Repeatability.EvaluatedTrials, a.Repeatability.PassingTrials, a.Repeatability.Lower95*100, a.Repeatability.Upper95*100))
	} else {
		a.Findings = append(a.Findings, "Repeat pass probability interval is unavailable because there are no evaluable terminal PASS/FAIL trials.")
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

	invalid, inconclusive, limited := a.State == "INVALID_EVIDENCE", a.GeneratorRuntimeStatus == "CHANGED" || a.GeneratorCapacityStatus == "CHANGED", false
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			a.RequiredPhases++
			state, reason := campaign.ClassifyPhase(e, phase.Run, phase.Name)
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
				legacyClassification := e.SchemaVersion == 2 || e.SchemaVersion == 4
				legacyStability := legacyClassification
				if (!legacyClassification && phase.EvidenceState != state) || ((phase.Name == "observation" || phase.Name == "soak") && !legacyStability && phase.Stability != stability) {
					invalid = true
					a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s has stale or altered stage evidence.", trial.Number, phase.Name))
				}
				if legacyClassification && !validHistoricalPhaseState(phase.EvidenceState) {
					invalid = true
					a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s has an unsupported historical phase classification.", trial.Number, phase.Name))
				} else if legacyClassification && phase.EvidenceState != state {
					inconclusive = true
					a.Findings = append(a.Findings, fmt.Sprintf("Trial %d %s historical %s classification conflicts with the current recomputed %s result.", trial.Number, phase.Name, phase.EvidenceState, state))
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

func validHistoricalPhaseState(value string) bool {
	switch value {
	case "INVALID_DELIVERY", "PASS_OBSERVED", "FAIL_OBSERVED", "INSUFFICIENT_EVIDENCE":
		return true
	default:
		return false
	}
}

func summarizeGeneratorEnvironments(e campaign.Result) ([]GeneratorEnvironment, string, string) {
	rows := make([]GeneratorEnvironment, 0)
	identityStatus, capacityStatus := "CONSISTENT", "CONSISTENT"
	identitySeen, capacitySeen := false, false
	var firstIdentity report.GeneratorIdentity
	var firstCapacity struct {
		logical, gomax int
		cores          float64
		source         string
		known          bool
	}
	identityUnknown, capacityUnknown := false, false
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			run := phase.Run
			row := GeneratorEnvironment{Trial: trial.Number, Phase: phase.Name}
			if run.SchemaVersion >= 31 && report.HasValidGeneratorIdentity(run) && run.GeneratorIdentity.RunnerIdentityMethod != "unavailable" {
				identity := run.GeneratorIdentity
				row.Identity = &identity
				if identitySeen && identity != firstIdentity {
					identityStatus = "CHANGED"
				}
				firstIdentity, identitySeen = identity, true
			} else {
				identityUnknown = true
			}
			if run.Generator != nil {
				row.LogicalCPUs = run.Generator.LogicalCPUs
				row.GOMAXPROCS = run.Generator.GOMAXPROCS
				row.CPUCapacityCores = run.Generator.CPUCapacityCores
				row.CPUCapacitySource = run.Generator.CPUCapacitySource
				row.CPUCapacityKnown = run.Generator.CPUCapacityKnown
				current := struct {
					logical, gomax int
					cores          float64
					source         string
					known          bool
				}{row.LogicalCPUs, row.GOMAXPROCS, row.CPUCapacityCores, row.CPUCapacitySource, row.CPUCapacityKnown}
				if capacitySeen && current != firstCapacity {
					capacityStatus = "CHANGED"
				}
				firstCapacity, capacitySeen = current, true
				if !current.known || current.logical < 1 || current.gomax < 1 || current.cores <= 0 || current.source == "" {
					capacityUnknown = true
				}
			} else {
				capacityUnknown = true
			}
			rows = append(rows, row)
		}
	}
	if identityStatus != "CHANGED" && (!identitySeen || identityUnknown) {
		identityStatus = "UNKNOWN"
	}
	if capacityStatus != "CHANGED" && (!capacitySeen || capacityUnknown) {
		capacityStatus = "UNKNOWN"
	}
	return rows, identityStatus, capacityStatus
}

func summarizeRepeatability(trials []campaign.Trial) RepeatabilityEvidence {
	evidence := RepeatabilityEvidence{
		Status: "INSUFFICIENT_EVIDENCE", Method: "EXACT_CLOPPER_PEARSON_BINOMIAL",
		Assumption: "INDEPENDENT_STATIONARY_REPEAT_TRIALS",
	}
	for _, trial := range trials {
		switch trial.Outcome {
		case "PASS":
			evidence.EvaluatedTrials++
			evidence.PassingTrials++
		case "FAIL":
			evidence.EvaluatedTrials++
		default:
			evidence.ExcludedTrials++
		}
	}
	if evidence.EvaluatedTrials == 0 {
		return evidence
	}
	lower, upper, err := statistics.BinomialProportionInterval95(evidence.PassingTrials, evidence.EvaluatedTrials)
	if err != nil {
		return evidence
	}
	evidence.Status, evidence.Lower95, evidence.Upper95 = "CONDITIONAL_INTERVAL", lower, upper
	return evidence
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

func recomputeObservationEvidence(e campaign.Result) ([]ObservationTrial, *campaign.Variability) {
	observations := make([]ObservationTrial, 0, e.Repeats)
	spread := &campaign.Variability{
		P95MinMS: math.Inf(1), ErrorRateMin: math.Inf(1), ThroughputMinRPS: math.Inf(1),
		SuccessfulJourneysMinPerSec: math.Inf(1),
	}
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "observation" || phase.Run.WindowSeconds() <= 0 {
				continue
			}
			run := phase.Run
			observation := ObservationTrial{
				Trial: trial.Number, Requests: run.Metrics.Total, RequestP95MS: run.Metrics.P95MS,
				RequestP99MS: run.Metrics.P99MS, RequestP99IntervalState: string(run.Metrics.P99RankIntervalState()),
				RequestErrorRate: observedErrorRate(run.Metrics), CompletedJourneys: run.Journeys.Total,
				JourneyP95MS: run.Journeys.P95MS, JourneyP99MS: run.Journeys.P99MS,
				JourneyP99IntervalState: string(run.Journeys.P99RankIntervalState()), JourneyErrorRate: observedErrorRate(run.Journeys),
				SuccessfulJourneysPerSec: report.SuccessfulJourneysPerSecond(run),
			}
			observations = append(observations, observation)
			spread.Runs++
			spread.P95MinMS = math.Min(spread.P95MinMS, run.Metrics.P95MS)
			spread.P95MaxMS = math.Max(spread.P95MaxMS, run.Metrics.P95MS)
			spread.ErrorRateMin = math.Min(spread.ErrorRateMin, run.ErrorsRate)
			spread.ErrorRateMax = math.Max(spread.ErrorRateMax, run.ErrorsRate)
			throughput := float64(run.Metrics.Total) / run.WindowSeconds()
			spread.ThroughputMinRPS = math.Min(spread.ThroughputMinRPS, throughput)
			spread.ThroughputMaxRPS = math.Max(spread.ThroughputMaxRPS, throughput)
			journeyThroughput := report.SuccessfulJourneysPerSecond(run)
			spread.SuccessfulJourneysMinPerSec = math.Min(spread.SuccessfulJourneysMinPerSec, journeyThroughput)
			spread.SuccessfulJourneysMaxPerSec = math.Max(spread.SuccessfulJourneysMaxPerSec, journeyThroughput)
		}
	}
	if spread.Runs == 0 {
		return observations, nil
	}
	return observations, spread
}

func observedErrorRate(metrics report.MetricSet) *float64 {
	if metrics.Total <= 0 || metrics.Failures < 0 || metrics.Failures > metrics.Total {
		return nil
	}
	rate := float64(metrics.Failures) / float64(metrics.Total)
	return &rate
}

func observationSpreadConsistency(e campaign.Result, recomputed *campaign.Variability) string {
	if e.SchemaVersion < 3 && e.ObservationVariability != nil {
		// Schema v2 predates journey-throughput fields. Its decoded zero values
		// are absent evidence, so the full modern summary cannot be compared.
		return "NOT_RECORDED"
	}
	if recomputed == nil {
		if e.ObservationVariability == nil {
			return "NO_OBSERVATIONS"
		}
		return "MISMATCHED"
	}
	if e.ObservationVariability == nil {
		return "NOT_RECORDED"
	}
	want := *recomputed
	got := *e.ObservationVariability
	// Campaign schemas added fields over time. Compare only values that the
	// artifact version actually recorded; zero-valued fields introduced later
	// are missing evidence, not evidence of an observed zero.
	if e.SchemaVersion < 14 {
		want.RequestP95MedianMS, want.RequestP95MADMS = got.RequestP95MedianMS, got.RequestP95MADMS
		want.RequestP95RobustLowerMS, want.RequestP95RobustUpperMS = got.RequestP95RobustLowerMS, got.RequestP95RobustUpperMS
		want.RequestP95RobustMethod, want.RequestP95RobustAssumption = got.RequestP95RobustMethod, got.RequestP95RobustAssumption
	}
	if got != want {
		return "MISMATCHED"
	}
	return "MATCHED"
}

func appendUniqueLevel(levels []int, level int) []int {
	for _, existing := range levels {
		if existing == level {
			return levels
		}
	}
	return append(levels, level)
}
