package capacityplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const MaxCampaignCollectionSize = 20

// CampaignCollectionAssessment summarizes independently recorded campaign
// levels. Its verdict is descriptive: it never interpolates an untested level
// or interprets the observations as a sustainable-capacity curve.
type CampaignCollectionAssessment struct {
	SchemaVersion       int                       `json:"schema_version"`
	AssessmentType      string                    `json:"assessment_type"`
	State               string                    `json:"state"`
	Scenario            string                    `json:"scenario,omitempty"`
	Target              string                    `json:"target,omitempty"`
	WorkloadFingerprint string                    `json:"workload_fingerprint,omitempty"`
	Levels              []CampaignLevelAssessment `json:"levels"`
	PassingLevels       []int                     `json:"observed_passing_levels"`
	FailingLevels       []int                     `json:"observed_failing_levels"`
	InconclusiveLevels  []int                     `json:"inconclusive_levels"`
	ObservationPattern  string                    `json:"observation_pattern"`
	Demand              *Result                   `json:"demand,omitempty"`
	DemandEvidenceID    string                    `json:"demand_evidence_id,omitempty"`
	Findings            []string                  `json:"findings"`
}

type CampaignLevelAssessment struct {
	TestedUsers int        `json:"tested_users"`
	Outcome     string     `json:"outcome"`
	Assessment  Assessment `json:"assessment"`
}

type campaignLevelShape struct {
	SchemaVersion       int                         `json:"schema_version"`
	Scenario            string                      `json:"scenario"`
	Target              string                      `json:"target"`
	Warmup              string                      `json:"warmup"`
	Observation         string                      `json:"observation"`
	SpikeDuration       string                      `json:"spike_duration"`
	SpikeRatio          string                      `json:"spike_ratio"`
	Recovery            string                      `json:"recovery"`
	Soak                string                      `json:"soak,omitempty"`
	Cooldown            string                      `json:"cooldown,omitempty"`
	LongRun             bool                        `json:"long_run,omitempty"`
	Environment         string                      `json:"declared_environment,omitempty"`
	StabilityPolicy     campaign.StabilityPolicy    `json:"stability_policy"`
	GeneratorGuardrails *generatorhealth.Guardrails `json:"generator_guardrails,omitempty"`
	Budget              string                      `json:"budget,omitempty"`
	Repeats             int                         `json:"repeats"`
	Controls            *campaignRunShape           `json:"controls,omitempty"`
}

type campaignRunShape struct {
	RunSchemaVersion    int                    `json:"run_schema_version"`
	WorkloadRevision    string                 `json:"workload_revision,omitempty"`
	ApplicationRevision string                 `json:"application_revision,omitempty"`
	WorkloadFingerprint string                 `json:"workload_fingerprint"`
	Environment         string                 `json:"environment,omitempty"`
	ProfileMixBasis     string                 `json:"profile_mix_basis,omitempty"`
	ProfileMixReference string                 `json:"profile_mix_reference,omitempty"`
	RampUp              string                 `json:"ramp_up,omitempty"`
	ThinkTime           string                 `json:"think_time,omitempty"`
	ThinkTimeRange      *report.ThinkTimeRange `json:"think_time_range,omitempty"`
	ThinkTimeSamples    []string               `json:"think_time_samples,omitempty"`
	WorkloadSeed        uint64                 `json:"workload_seed,omitempty"`
	RequestTimeout      string                 `json:"request_timeout"`
	ConnectionPolicy    string                 `json:"connection_policy,omitempty"`
	MaxRPS              int                    `json:"max_rps"`
	P95LimitMS          float64                `json:"p95_limit_ms"`
	MaxErrorRate        float64                `json:"max_error_rate"`
	Mode                string                 `json:"mode"`
	ArrivalRate         int                    `json:"arrival_rate,omitempty"`
	SeparateSessions    bool                   `json:"separate_sessions"`
	MinSamplesPerStep   int                    `json:"min_samples_per_step"`
	Profiles            []report.ProfileConfig `json:"profiles,omitempty"`
	WindowDuration      string                 `json:"window_duration,omitempty"`
}

// BuildCampaignCollectionAssessment aggregates two or more saved campaigns
// while retaining a complete offline assessment for every tested level.
func BuildCampaignCollectionAssessment(evidence []campaign.Result, goal *capacitygoal.Goal) (CampaignCollectionAssessment, error) {
	if len(evidence) < 2 || len(evidence) > MaxCampaignCollectionSize {
		return CampaignCollectionAssessment{}, fmt.Errorf("campaign collection must contain 2-%d reports", MaxCampaignCollectionSize)
	}
	result := CampaignCollectionAssessment{
		SchemaVersion:  1,
		AssessmentType: "CAMPAIGN_LEVEL_COLLECTION",
		State:          "OBSERVED_LEVELS_ONLY",
		Levels:         make([]CampaignLevelAssessment, 0, len(evidence)),
		PassingLevels:  []int{}, FailingLevels: []int{}, InconclusiveLevels: []int{},
		ObservationPattern: "UNAVAILABLE",
		Findings: []string{
			"Each level is audited independently; no result is interpolated or extrapolated to an untested workload.",
			"Observed passing and failing levels do not establish a monotone capacity boundary or sustainable human-user capacity.",
		},
	}
	type levelEvidence struct {
		campaign campaign.Result
		shape    string
		reason   string
	}
	levels := make([]levelEvidence, 0, len(evidence))
	seen := make(map[int]bool, len(evidence))
	for _, item := range evidence {
		if seen[item.BaseUsers] {
			return CampaignCollectionAssessment{}, fmt.Errorf("campaign collection has duplicate tested level %d; combine repeated trials into one campaign", item.BaseUsers)
		}
		seen[item.BaseUsers] = true
		shape, err := campaignCollectionShape(item)
		if err != nil {
			shape = ""
		}
		levels = append(levels, levelEvidence{campaign: item, shape: shape, reason: errorText(err)})
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i].campaign.BaseUsers < levels[j].campaign.BaseUsers })
	comparable := true
	firstShape := ""
	invalid := false
	for i, item := range levels {
		assessment, err := BuildAssessment(item.campaign, nil)
		if err != nil {
			return CampaignCollectionAssessment{}, err
		}
		if assessment.Audit.State == "INVALID_EVIDENCE" {
			invalid = true
		}
		result.Levels = append(result.Levels, CampaignLevelAssessment{
			TestedUsers: item.campaign.BaseUsers, Outcome: item.campaign.Outcome, Assessment: assessment,
		})
		if item.shape == "" {
			comparable = false
			result.Findings = append(result.Findings, fmt.Sprintf("Level %d VUs cannot be compared: %s.", item.campaign.BaseUsers, item.reason))
		} else if i == 0 || firstShape == "" {
			firstShape = item.shape
		} else if item.shape != firstShape {
			comparable = false
			result.Findings = append(result.Findings, fmt.Sprintf("Level %d VUs has a different declared workload or control signature.", item.campaign.BaseUsers))
		}
	}
	if invalid {
		result.State = "INVALID_EVIDENCE"
		result.Findings = append(result.Findings, "At least one campaign contains invalid evidence; combined level conclusions are not reported.")
	} else if !comparable {
		result.State = "NOT_COMPARABLE"
		result.Findings = append(result.Findings, "Campaigns do not share a complete matching declared workload and control signature; per-level audits are retained, but levels are not aggregated.")
	} else {
		result.Scenario = levels[0].campaign.Scenario
		result.Target = levels[0].campaign.Target
		result.WorkloadFingerprint = firstWorkloadFingerprint(levels[0].campaign)
		for _, level := range result.Levels {
			switch level.Assessment.Audit.State {
			case "OBSERVED_PASS_ONLY":
				result.PassingLevels = append(result.PassingLevels, level.TestedUsers)
			case "OBSERVED_FAIL":
				result.FailingLevels = append(result.FailingLevels, level.TestedUsers)
			default:
				result.InconclusiveLevels = append(result.InconclusiveLevels, level.TestedUsers)
			}
		}
		result.ObservationPattern = observationPattern(result.PassingLevels, result.FailingLevels)
		if len(result.PassingLevels)+len(result.FailingLevels) == 0 {
			result.State = "INCONCLUSIVE"
			result.Findings = append(result.Findings, "No campaign level has a decisive observed pass or fail outcome.")
		}
		if result.ObservationPattern == "NON_MONOTONIC" {
			result.Findings = append(result.Findings, "Observed pass/fail outcomes are non-monotonic across tested levels; investigate workload or environment variation instead of inferring a threshold.")
		}
		result.Findings = append(result.Findings, "Declared workload signatures match, but physical generator identity, actual demand equivalence, dependence, and stationarity are not independently established.")
	}
	if goal != nil {
		target, err := capacitygoal.EvaluatedUsers(*goal)
		if err != nil {
			return CampaignCollectionAssessment{}, err
		}
		if result.State == "INVALID_EVIDENCE" {
			result.Demand = &Result{State: "INSUFFICIENT_EVIDENCE", TargetUsers: target, Findings: []string{
				"At least one campaign in the collection has invalid evidence; no combined demand conclusion is reported.",
			}}
		} else {
			result.Demand = &Result{State: "INSUFFICIENT_EVIDENCE", TargetUsers: target, Findings: []string{
				"A demand decision requires a campaign at the exact calculated workload level; nearby levels are not interpolated.",
			}}
			for _, item := range levels {
				if item.campaign.BaseUsers != target {
					continue
				}
				demand, err := Evaluate(item.campaign, *goal)
				if err != nil {
					return CampaignCollectionAssessment{}, err
				}
				result.Demand = &demand
				result.DemandEvidenceID = evidenceDigest(item.campaign)
				break
			}
		}
	}
	return result, nil
}

func campaignCollectionShape(e campaign.Result) (string, error) {
	if e.BaseUsers < 1 || strings.TrimSpace(e.Scenario) == "" || strings.TrimSpace(e.Target) == "" {
		return "", errors.New("campaign level is missing a positive VU level, scenario name, or target")
	}
	shape := campaignLevelShape{
		SchemaVersion: e.SchemaVersion, Scenario: e.Scenario, Target: e.Target,
		Warmup: e.Warmup, Observation: e.Observation,
		SpikeDuration: e.SpikeDuration, Recovery: e.Recovery, Soak: e.Soak, Cooldown: e.Cooldown, StabilityPolicy: e.EffectiveStabilityPolicy(), Budget: e.Budget, Repeats: e.Repeats,
		LongRun: e.LongRun, Environment: e.DeclaredEnvironment,
		GeneratorGuardrails: e.GeneratorGuardrails,
	}
	if e.SpikeUsers == 0 {
		shape.SpikeRatio = "none"
	} else if e.BaseUsers < 1 {
		return "", errors.New("invalid base workload level")
	} else {
		gcd := greatestCommonDivisor(e.SpikeUsers, e.BaseUsers)
		shape.SpikeRatio = fmt.Sprintf("%d/%d", e.SpikeUsers/gcd, e.BaseUsers/gcd)
	}
	options, _, err := campaignLevelSchedule(e)
	if err != nil {
		return "", err
	}
	workloadFingerprint := ""
	controlSet := false
	seenRequiredPhases := 0
	for _, trial := range e.Trials {
		trialPlanned := campaign.PlannedPhases(options, e.BaseUsers)
		if e.SchemaVersion >= 4 {
			trialPlanned = campaign.PlannedTrialPhases(options, e.BaseUsers, trial.Number)
		}
		if trial.Number < 1 || trial.Number > e.Repeats || len(trial.Phases) > len(trialPlanned) || trial.Outcome == "PASS" && len(trial.Phases) != len(trialPlanned) {
			return "", errors.New("campaign trial phases do not match the declared schedule")
		}
		for phaseIndex, phase := range trial.Phases {
			want := trialPlanned[phaseIndex]
			if phase.Name != want.Name || phase.Run.Config.Users != want.Users || phase.Run.Config.Duration != want.Duration.String() || phase.Run.Target != e.Target {
				return "", errors.New("campaign phase does not match its declared workload schedule")
			}
			if phase.Name == "warmup" {
				continue
			}
			seenRequiredPhases++
			r := phase.Run
			if r.SchemaVersion < 14 {
				return "", errors.New("run schema predates comparable connection-policy evidence")
			}
			if strings.TrimSpace(r.Config.WorkloadRevision) == "" || strings.TrimSpace(r.Config.ApplicationRevision) == "" || strings.TrimSpace(r.Config.Environment) == "" {
				return "", errors.New("required workload, application, or environment revision is missing")
			}
			fingerprint := strings.TrimSpace(r.Config.WorkloadFingerprint)
			decoded, err := hex.DecodeString(strings.TrimPrefix(fingerprint, "sha256:"))
			if !strings.HasPrefix(fingerprint, "sha256:") || err != nil || len(decoded) != sha256.Size {
				return "", errors.New("missing or malformed workload fingerprint")
			}
			if workloadFingerprint == "" {
				workloadFingerprint = fingerprint
			} else if fingerprint != workloadFingerprint {
				return "", errors.New("required phase workload fingerprints differ")
			}
			if r.Config.P95LimitMS < 0 || r.Config.MaxErrorRate < 0 || math.IsNaN(r.Config.P95LimitMS) || math.IsInf(r.Config.P95LimitMS, 0) || math.IsNaN(r.Config.MaxErrorRate) || math.IsInf(r.Config.MaxErrorRate, 0) {
				return "", errors.New("invalid control signature")
			}
			candidate := campaignRunShape{
				RunSchemaVersion: r.SchemaVersion,
				WorkloadRevision: r.Config.WorkloadRevision, ApplicationRevision: r.Config.ApplicationRevision,
				WorkloadFingerprint: fingerprint, Environment: r.Config.Environment,
				ProfileMixBasis: r.Config.ProfileMixBasis, ProfileMixReference: r.Config.ProfileMixReference,
				RampUp: r.Config.RampUp, ThinkTime: r.Config.ThinkTime,
				ThinkTimeRange: r.Config.ThinkTimeRange, ThinkTimeSamples: r.Config.ThinkTimeSamples,
				WorkloadSeed: r.Config.WorkloadSeed, RequestTimeout: r.Config.RequestTimeout,
				ConnectionPolicy: r.Config.ConnectionPolicy, MaxRPS: r.Config.MaxRPS,
				P95LimitMS: r.Config.P95LimitMS, MaxErrorRate: r.Config.MaxErrorRate,
				Mode: r.Config.Mode, ArrivalRate: r.Config.ArrivalRate,
				SeparateSessions: r.Config.SeparateSessions, MinSamplesPerStep: r.Config.MinSamplesPerStep,
				Profiles: r.Config.Profiles, WindowDuration: r.Config.WindowDuration,
			}
			if !controlSet {
				shape.Controls = &candidate
				controlSet = true
			} else if !sameCampaignRunShape(*shape.Controls, candidate) {
				return "", errors.New("required phase workload or control signatures differ")
			}
		}
	}
	if seenRequiredPhases > 0 && !controlSet {
		return "", errors.New("no required phase controls")
	}
	return digestCampaignShape(shape)
}

func digestCampaignShape(shape campaignLevelShape) (string, error) {
	data, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func campaignLevelSchedule(e campaign.Result) (campaign.Options, []campaign.PlannedPhase, error) {
	if e.SchemaVersion < 7 && e.GeneratorGuardrails != nil {
		return campaign.Options{}, nil, errors.New("generator resource guardrails are unsupported by this campaign schema")
	}
	if e.SchemaVersion < 6 && e.LongRun {
		return campaign.Options{}, nil, errors.New("long-run declaration is unsupported by this campaign schema")
	}
	if e.LongRun && e.DeclaredEnvironment != "staging" {
		return campaign.Options{}, nil, errors.New("long-run campaign is missing its declared staging environment")
	}
	warmup, err := time.ParseDuration(e.Warmup)
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign warmup schedule")
	}
	observation, err := time.ParseDuration(e.Observation)
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign observation schedule")
	}
	spike, err := time.ParseDuration(e.SpikeDuration)
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign spike schedule")
	}
	recovery, err := time.ParseDuration(e.Recovery)
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign recovery schedule")
	}
	soak := time.Duration(0)
	if e.SchemaVersion >= 4 && e.Soak != "" {
		soak, err = time.ParseDuration(e.Soak)
	}
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign soak schedule")
	}
	cooldown := time.Duration(0)
	if e.SchemaVersion >= 9 {
		cooldown, err = time.ParseDuration(e.Cooldown)
	} else if e.Cooldown != "" {
		return campaign.Options{}, nil, errors.New("cooldown is unsupported by this campaign schema")
	}
	if err != nil || cooldown < 0 || cooldown > 5*time.Minute {
		return campaign.Options{}, nil, errors.New("invalid campaign cooldown schedule")
	}
	budget := time.Duration(0)
	if e.Budget != "" {
		budget, err = time.ParseDuration(e.Budget)
	}
	if err != nil {
		return campaign.Options{}, nil, errors.New("invalid campaign budget")
	}
	options := campaign.Options{Warmup: warmup, Observation: observation, SpikeUsers: e.SpikeUsers,
		SpikeDuration: spike, Recovery: recovery, Soak: soak, Cooldown: cooldown, Repeats: e.Repeats, Budget: budget,
		Stability: e.EffectiveStabilityPolicy(), LongRun: e.LongRun, StagingConfirmed: e.LongRun}
	if e.SchemaVersion >= 7 && e.GeneratorGuardrails != nil {
		options.Guardrails = *e.GeneratorGuardrails
	}
	if err := campaign.Validate(options); err != nil {
		if !(e.SchemaVersion < 4 && options.Budget > 0 && options.Budget <= 15*time.Minute && options.Budget >= time.Duration(options.Repeats)*(options.Warmup+options.Observation+options.SpikeDuration+options.Recovery)) {
			return campaign.Options{}, nil, fmt.Errorf("invalid campaign schedule: %w", err)
		}
	}
	if options.Budget == 0 {
		options.Budget = campaign.PlannedBudget(options)
	}
	return options, campaign.PlannedPhases(options, e.BaseUsers), nil
}

func sameCampaignRunShape(left, right campaignRunShape) bool {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(right)
	return err == nil && string(leftJSON) == string(rightJSON)
}

func errorText(err error) string {
	if err == nil {
		return "declared controls differ between campaigns"
	}
	return err.Error()
}

func firstWorkloadFingerprint(e campaign.Result) string {
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "warmup" {
				return phase.Run.Config.WorkloadFingerprint
			}
		}
	}
	return ""
}

func greatestCommonDivisor(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func observationPattern(passing, failing []int) string {
	for _, pass := range passing {
		for _, fail := range failing {
			if fail < pass {
				return "NON_MONOTONIC"
			}
		}
	}
	switch {
	case len(passing) > 0 && len(failing) > 0:
		return "PASS_BELOW_FAIL"
	case len(passing) > 0:
		return "NO_FAILURE_OBSERVED"
	case len(failing) > 0:
		return "NO_PASS_OBSERVED"
	default:
		return "NO_DECISIVE_LEVEL"
	}
}
