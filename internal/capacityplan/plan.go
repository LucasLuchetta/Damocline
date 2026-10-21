// Package capacityplan compares a business demand target with campaign evidence.
package capacityplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/campaignprofile"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

const maxCampaignReportBytes = 16 << 20
const maxCampaignCollectionBytes = 64 << 20

type Result struct {
	State       string   `json:"state"`
	TargetUsers int      `json:"target_users"`
	TestedUsers int      `json:"tested_users"`
	Findings    []string `json:"findings"`
}

func Evaluate(evidence campaign.Result, goal capacitygoal.Goal) (Result, error) {
	return evaluateWithQuality(evidence, goal, report.AssessQuality)
}

// ValidateSavedCampaign validates the structure and native phase evidence of a
// saved campaign without applying a business-demand goal.
func ValidateSavedCampaign(evidence campaign.Result) error {
	return validateEvidence(evidence)
}

type qualityAssessor func(report.Result) *report.Quality

func evaluateWithQuality(evidence campaign.Result, goal capacitygoal.Goal, assess qualityAssessor) (Result, error) {
	target, err := capacitygoal.EvaluatedUsers(goal)
	if err != nil {
		return Result{}, err
	}
	result := Result{State: "INSUFFICIENT_EVIDENCE", TargetUsers: target, TestedUsers: evidence.BaseUsers, Findings: []string{
		"The target is a business demand calculation, not a prediction of server capacity.",
		"A result applies only to the recorded workload, environment, SLOs, observation windows, and generator evidence.",
	}}
	if err := validateEvidence(evidence); err != nil {
		result.Findings = append(result.Findings, "Campaign evidence is incomplete or malformed: "+err.Error())
		return result, nil
	}
	if evidence.Outcome != "PASS" {
		result.Findings = append(result.Findings, fmt.Sprintf("Campaign outcome is %s, so it cannot support a sustained-demand conclusion.", evidence.Outcome))
		return result, nil
	}
	if evidence.BaseUsers != target {
		result.Findings = append(result.Findings, fmt.Sprintf("The passing base level (%d VUs) differs from the required target (%d VUs). Test the exact target before deciding on capacity.", evidence.BaseUsers, target))
		return result, nil
	}
	if blocker := qualityBlocker(evidence, assess); blocker != "" {
		result.Findings = append(result.Findings, blocker)
		return result, nil
	}
	if blocker := statisticalBlocker(evidence); blocker != "" {
		result.Findings = append(result.Findings, blocker)
		return result, nil
	}
	if !validObservationVariability(evidence.ObservationVariability, evidence.Repeats) {
		result.Findings = append(result.Findings, "Observation variability is not recorded for every independent trial; demand remains INSUFFICIENT_EVIDENCE.")
		return result, nil
	}
	if evidence.SchemaVersion >= 3 && !validSuccessfulJourneyVariability(evidence.ObservationVariability) {
		result.Findings = append(result.Findings, fmt.Sprintf("Successful journey throughput spread is missing or malformed in this schema-v%d campaign; demand remains INSUFFICIENT_EVIDENCE.", evidence.SchemaVersion))
		return result, nil
	}
	if evidence.SchemaVersion < 2 {
		result.Findings = append(result.Findings, "Legacy campaign reports lack verifiable stage classification and observation drift evidence; demand remains INSUFFICIENT_EVIDENCE.")
		return result, nil
	}
	result.State = "GOAL_MET_UNDER_TESTED_CONDITIONS"
	result.Findings = append(result.Findings, fmt.Sprintf("The repeated campaign passed at %d base VUs, meeting the calculated target of %d VUs under the recorded conditions.", evidence.BaseUsers, target))
	result.Findings = append(result.Findings, "Statistical bounds are conditional on independent, stationary requests and latencies; temporal dependence and stationarity are not established by this assessment.")
	if evidence.ObservationVariability != nil {
		result.Findings = append(result.Findings, fmt.Sprintf("Observed observation p95 ranged from %.0f to %.0f ms across %d trials; review the full report before changing infrastructure.", evidence.ObservationVariability.P95MinMS, evidence.ObservationVariability.P95MaxMS, evidence.ObservationVariability.Runs))
	}
	return result, nil
}

func validSuccessfulJourneyVariability(v *campaign.Variability) bool {
	return v != nil && !math.IsNaN(v.SuccessfulJourneysMinPerSec) && !math.IsInf(v.SuccessfulJourneysMinPerSec, 0) &&
		!math.IsNaN(v.SuccessfulJourneysMaxPerSec) && !math.IsInf(v.SuccessfulJourneysMaxPerSec, 0) &&
		v.SuccessfulJourneysMinPerSec >= 0 && v.SuccessfulJourneysMaxPerSec >= v.SuccessfulJourneysMinPerSec
}

func qualityBlocker(evidence campaign.Result, assess qualityAssessor) string {
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			if phase.Run.Quality == nil {
				return "A required campaign phase is missing explicit data-quality evidence; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
			if phase.Run.Quality.State != report.QualityValid && phase.Run.Quality.State != report.QualityInvalid && phase.Run.Quality.State != report.QualityLimited {
				return "A required campaign phase has unknown measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
			if assess == nil {
				return "A required campaign phase cannot have its data quality recomputed; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
			recomputed := assess(phase.Run)
			if recomputed == nil {
				return "A required campaign phase has no recomputable data-quality result; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
			if phase.Run.Quality.State != recomputed.State {
				return fmt.Sprintf("A required campaign phase claims %s measurement quality but its report fields recompute as %s; the tested demand remains INSUFFICIENT_EVIDENCE.", phase.Run.Quality.State, recomputed.State)
			}
			switch recomputed.State {
			case report.QualityValid:
				continue
			case report.QualityInvalid:
				return "A required campaign phase has INVALID measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			case report.QualityLimited:
				return "A required campaign phase has LIMITED measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			default:
				return "A required campaign phase has unknown measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
		}
	}
	return ""
}

func statisticalBlocker(evidence campaign.Result) string {
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			run := phase.Run
			if run.Config.P95LimitMS <= 0 || math.IsNaN(run.Config.P95LimitMS) || math.IsInf(run.Config.P95LimitMS, 0) ||
				run.Config.MaxErrorRate <= 0 || run.Config.MaxErrorRate >= 1 || math.IsNaN(run.Config.MaxErrorRate) || math.IsInf(run.Config.MaxErrorRate, 0) {
				return "A required phase is missing a statistically assessable latency or nonzero error-budget objective; demand remains INSUFFICIENT_EVIDENCE."
			}
			if run.ObservationSeconds <= 0 || math.IsNaN(run.ObservationSeconds) || math.IsInf(run.ObservationSeconds, 0) {
				return "A required phase has no valid observation duration; demand remains INSUFFICIENT_EVIDENCE."
			}
			windowDuration, err := time.ParseDuration(run.Config.WindowDuration)
			if err != nil || windowDuration <= 0 || len(run.Windows) == 0 {
				return "A required phase has no complete, configured SLO-window evidence; demand remains INSUFFICIENT_EVIDENCE."
			}
			if blocker := metricSetBlocker(run.Metrics, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
				return "A required phase aggregate " + blocker
			}
			if run.SchemaVersion < 22 {
				return "A required phase lacks schema-v22 completed-journey evidence; demand remains INSUFFICIENT_EVIDENCE."
			}
			if blocker := metricSetBlocker(run.Journeys, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
				return "A required phase completed journeys " + blocker
			}
			if len(run.Steps) == 0 {
				return "A required phase has no per-step sample evidence; demand remains INSUFFICIENT_EVIDENCE."
			}
			for _, step := range run.Steps {
				if blocker := metricSetBlocker(step.Metrics, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
					return "A required phase per-step " + blocker
				}
			}
			windowSeconds := windowDuration.Seconds()
			observationSeconds := run.ObservationSeconds
			expectedStart := 0.0
			for _, window := range run.Windows {
				expectedEnd := math.Min(expectedStart+windowSeconds, observationSeconds)
				if math.IsNaN(window.StartSeconds) || math.IsInf(window.StartSeconds, 0) ||
					math.IsNaN(window.EndSeconds) || math.IsInf(window.EndSeconds, 0) ||
					window.StartSeconds < 0 || window.EndSeconds <= window.StartSeconds ||
					math.Abs(window.StartSeconds-expectedStart) > 0.001 ||
					math.Abs(window.EndSeconds-expectedEnd) > 0.001 {
					return "A required phase has missing, overlapping, or out-of-range SLO windows; demand remains INSUFFICIENT_EVIDENCE."
				}
				if blocker := metricSetBlocker(window.Metrics, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
					return "A required phase window " + blocker
				}
				if blocker := metricSetBlocker(window.Journeys, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
					return "A required phase completed-journey window " + blocker
				}
				if run.SchemaVersion < 13 || len(window.Steps) != len(run.Steps) {
					return "A required phase lacks schema-v13 per-step SLO-window evidence; demand remains INSUFFICIENT_EVIDENCE."
				}
				expectedSteps := make(map[string]bool, len(run.Steps))
				for _, step := range run.Steps {
					expectedSteps[step.Name] = true
				}
				for _, windowStep := range window.Steps {
					if !expectedSteps[windowStep.Name] {
						return "A required phase has duplicate or unknown per-step SLO-window evidence; demand remains INSUFFICIENT_EVIDENCE."
					}
					delete(expectedSteps, windowStep.Name)
					if blocker := metricSetBlocker(windowStep.Metrics, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
						return "A required phase per-step window " + blocker
					}
				}
				if len(expectedSteps) > 0 {
					return "A required phase omits one or more named steps from an SLO window; demand remains INSUFFICIENT_EVIDENCE."
				}
				if len(window.Profiles) != len(run.Config.Profiles) {
					return "A required phase lacks schema-v13 per-profile journey-window evidence; demand remains INSUFFICIENT_EVIDENCE."
				}
				expectedProfiles := make(map[string]bool, len(run.Config.Profiles))
				for _, profile := range run.Config.Profiles {
					expectedProfiles[profile.Name] = true
				}
				for _, windowProfile := range window.Profiles {
					if !expectedProfiles[windowProfile.Name] {
						return "A required phase has duplicate or unknown per-profile journey-window evidence; demand remains INSUFFICIENT_EVIDENCE."
					}
					delete(expectedProfiles, windowProfile.Name)
					if blocker := metricSetBlocker(windowProfile.Metrics, run.Config.P95LimitMS, run.Config.MaxErrorRate); blocker != "" {
						return "A required phase per-profile journey window " + blocker
					}
				}
				if len(expectedProfiles) > 0 {
					return "A required phase omits one or more weighted profiles from a journey SLO window; demand remains INSUFFICIENT_EVIDENCE."
				}
				expectedStart = window.EndSeconds
			}
			if expectedStart+0.001 < observationSeconds || run.Windows[len(run.Windows)-1].StartSeconds >= observationSeconds {
				return "A required phase does not have continuous SLO-window coverage through its observation horizon; demand remains INSUFFICIENT_EVIDENCE."
			}
		}
	}
	return ""
}

func metricSetBlocker(metrics report.MetricSet, p95LimitMS, maxErrorRate float64) string {
	if metrics.Total <= 0 || metrics.Success < 0 || metrics.Success > metrics.Total ||
		metrics.Failures != metrics.Total-metrics.Success || metrics.Cancelled != 0 {
		return "has insufficient or invalid request counts; demand remains INSUFFICIENT_EVIDENCE."
	}
	upperErrorRate, err := statistics.UpperFailureRate95(metrics.Failures, metrics.Total)
	if err != nil || upperErrorRate > maxErrorRate {
		return "does not establish its configured error budget under the conditional binomial bound; demand remains INSUFFICIENT_EVIDENCE."
	}
	if _, _, err := statistics.PercentileRankInterval95(metrics.Total, 0.95); err != nil ||
		!(metrics.P95IID95Approximation || metrics.P95IID95Exact) || math.IsNaN(metrics.P95IID95LowerMS) || math.IsInf(metrics.P95IID95LowerMS, 0) ||
		math.IsNaN(metrics.P95IID95UpperMS) || math.IsInf(metrics.P95IID95UpperMS, 0) ||
		metrics.P95IID95LowerMS < 0 || metrics.P95IID95UpperMS < metrics.P95IID95LowerMS || metrics.P95IID95UpperMS > p95LimitMS {
		return "lacks an adequate p95 rank interval below its configured limit; demand remains INSUFFICIENT_EVIDENCE."
	}
	return ""
}

func validObservationVariability(v *campaign.Variability, repeats int) bool {
	if v == nil || v.Runs != repeats ||
		math.IsNaN(v.P95MinMS) || math.IsInf(v.P95MinMS, 0) ||
		math.IsNaN(v.P95MaxMS) || math.IsInf(v.P95MaxMS, 0) || v.P95MinMS < 0 || v.P95MaxMS < v.P95MinMS ||
		math.IsNaN(v.ErrorRateMin) || math.IsInf(v.ErrorRateMin, 0) ||
		math.IsNaN(v.ErrorRateMax) || math.IsInf(v.ErrorRateMax, 0) || v.ErrorRateMin < 0 || v.ErrorRateMax > 1 || v.ErrorRateMax < v.ErrorRateMin ||
		math.IsNaN(v.ThroughputMinRPS) || math.IsInf(v.ThroughputMinRPS, 0) ||
		math.IsNaN(v.ThroughputMaxRPS) || math.IsInf(v.ThroughputMaxRPS, 0) || v.ThroughputMinRPS < 0 || v.ThroughputMaxRPS < v.ThroughputMinRPS {
		return false
	}
	return true
}

// ReadCampaign reads a bounded saved campaign report. It never makes a network
// request and accepts only supported historical or current campaign schemas.
func ReadCampaign(path string) (campaign.Result, error) {
	result, _, err := ReadCampaignWithFileSHA256(path)
	return result, err
}

// ReadCampaignWithFileSHA256 reads and decodes one bounded campaign report and
// hashes the exact bytes that were decoded. The hash identifies source bytes;
// it does not authenticate their producer or origin.
func ReadCampaignWithFileSHA256(path string) (campaign.Result, string, error) {
	data, err := readCampaignBytes(path, maxCampaignReportBytes)
	if err != nil {
		return campaign.Result{}, "", err
	}
	if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
		return campaign.Result{}, "", fmt.Errorf("invalid campaign metric evidence: %w", err)
	}
	if err := validateCampaignArtifactPresence(data); err != nil {
		return campaign.Result{}, "", err
	}
	var result campaign.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return campaign.Result{}, "", err
	}
	digest := sha256.Sum256(data)
	return result, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ReadCampaignCollection reads a bounded list of local campaign reports.
func ReadCampaignCollection(paths []string) ([]campaign.Result, error) {
	results, _, err := ReadCampaignCollectionWithFileSHA256(paths)
	return results, err
}

// ReadCampaignCollectionWithFileSHA256 reads a bounded list of campaign
// reports and returns one exact-source digest per report, in input order.
func ReadCampaignCollectionWithFileSHA256(paths []string) ([]campaign.Result, []string, error) {
	if len(paths) < 2 || len(paths) > MaxCampaignCollectionSize {
		return nil, nil, fmt.Errorf("campaign collection must contain 2-%d reports", MaxCampaignCollectionSize)
	}
	results := make([]campaign.Result, 0, len(paths))
	digests := make([]string, 0, len(paths))
	totalBytes := int64(0)
	for _, path := range paths {
		remaining := maxCampaignCollectionBytes - totalBytes
		if remaining <= 0 {
			return nil, nil, errors.New("campaign collection exceeds the 64 MiB safety limit")
		}
		limit := int64(maxCampaignReportBytes)
		if remaining < limit {
			limit = remaining
		}
		data, err := readCampaignBytes(path, limit)
		if err != nil {
			return nil, nil, err
		}
		totalBytes += int64(len(data))
		if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
			return nil, nil, fmt.Errorf("campaign %q has invalid metric evidence: %w", path, err)
		}
		if err := validateCampaignArtifactPresence(data); err != nil {
			return nil, nil, fmt.Errorf("campaign %q: %w", path, err)
		}
		var result campaign.Result
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, fmt.Errorf("cannot decode campaign %q: %w", path, err)
		}
		digest := sha256.Sum256(data)
		digests = append(digests, "sha256:"+hex.EncodeToString(digest[:]))
		results = append(results, result)
	}
	return results, digests, nil
}

func validateCampaignArtifactPresence(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"schema_version", "scenario", "base_users", "target", "started_at", "warmup", "observation", "recovery", "repeats", "trials", "outcome"} {
		value, ok := fields[name]
		if !ok || string(value) == "null" {
			return fmt.Errorf("campaign is missing required field %q; absence cannot be interpreted as zero", name)
		}
	}
	var schemaVersion int
	if err := json.Unmarshal(fields["schema_version"], &schemaVersion); err != nil {
		return fmt.Errorf("campaign schema_version is invalid: %w", err)
	}
	if schemaVersion == campaign.CurrentSchemaVersion {
		var startedAt string
		if err := json.Unmarshal(fields["started_at"], &startedAt); err != nil || startedAt == "" {
			return errors.New("current campaign started_at must be a non-empty RFC3339 timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, startedAt); err != nil {
			return errors.New("current campaign started_at must be a non-empty RFC3339 timestamp")
		}
	}
	return nil
}

func readCampaignBytes(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		if maxBytes == maxCampaignReportBytes {
			return nil, errors.New("campaign report exceeds the 16 MiB safety limit")
		}
		return nil, errors.New("campaign collection exceeds the 64 MiB safety limit")
	}
	if err := jsonartifact.RejectDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("campaign report contains ambiguous JSON: %w", err)
	}
	return data, nil
}

func validateEvidence(evidence campaign.Result) error {
	if evidence.SchemaVersion >= 10 {
		switch evidence.ExecutionState {
		case "RUNNING":
			if evidence.Outcome != "INCONCLUSIVE" || evidence.PendingTrial == nil || len(evidence.Trials) >= evidence.Repeats {
				return errors.New("in-progress campaign checkpoint is missing a pending trial or has an invalid state")
			}
			pending := *evidence.PendingTrial
			if pending.Number != len(evidence.Trials)+1 || pending.Outcome != "IN_PROGRESS" || !validCampaignRunID(pending.RunID) || pending.AbortReason != "" {
				return errors.New("in-progress campaign checkpoint has an invalid pending trial")
			}
			normalized := checkpointAsAbortedEvidence(evidence)
			return validateEvidence(normalized)
		case "COMPLETED":
			if evidence.PendingTrial != nil {
				return errors.New("completed campaign cannot contain a pending trial")
			}
		default:
			return errors.New("campaign execution state is missing or unsupported")
		}
	} else if evidence.ExecutionState != "" || evidence.PendingTrial != nil {
		return errors.New("execution checkpoints are unsupported by this campaign schema")
	}
	if evidence.SchemaVersion < 1 || evidence.SchemaVersion > campaign.CurrentSchemaVersion || evidence.Scenario == "" || evidence.BaseUsers < 1 || evidence.Repeats < 2 || evidence.Repeats > 5 || len(evidence.Trials) == 0 || len(evidence.Trials) > evidence.Repeats {
		return errors.New("expected a complete supported repeated campaign")
	}
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			if phase.Run.SchemaVersion < 1 || phase.Run.SchemaVersion > report.CurrentSchemaVersion {
				return fmt.Errorf("trial %d %s phase has an unsupported run-report schema", trial.Number, phase.Name)
			}
		}
	}
	if evidence.PendingTrial != nil {
		for _, phase := range evidence.PendingTrial.Phases {
			if phase.Run.SchemaVersion < 1 || phase.Run.SchemaVersion > report.CurrentSchemaVersion {
				return fmt.Errorf("pending trial %d %s phase has an unsupported run-report schema", evidence.PendingTrial.Number, phase.Name)
			}
		}
	}
	if err := validateWorkloadSource(evidence); err != nil {
		return err
	}
	if err := validateApplicationMetrics(evidence); err != nil {
		return err
	}
	if evidence.SchemaVersion >= 5 {
		if err := campaign.ValidateStabilityPolicy(evidence.EffectiveStabilityPolicy()); err != nil {
			return fmt.Errorf("invalid persisted stability policy: %w", err)
		}
	}
	if evidence.SchemaVersion >= 13 {
		if evidence.Objectives == nil {
			return errors.New("predeclared campaign objective contract is missing")
		}
		if err := evidence.Objectives.Validate(); err != nil {
			return fmt.Errorf("invalid predeclared campaign objectives: %w", err)
		}
		policy := evidence.EffectiveStabilityPolicy()
		if evidence.Objectives.MinimumObservationWindows != policy.MinimumWindows || evidence.Objectives.MinimumSamplesPerWindow != policy.MinimumSamplesPerWindow {
			return errors.New("predeclared window objectives do not match the saved stability policy")
		}
	} else if evidence.Objectives != nil {
		return errors.New("predeclared campaign objectives are unsupported by this campaign schema")
	}
	if evidence.SchemaVersion >= 13 {
		for _, trial := range evidence.Trials {
			for _, phase := range trial.Phases {
				if phase.Name != "warmup" && !evidence.Objectives.MatchesRun(phase.Run) {
					return fmt.Errorf("trial %d %s phase controls do not match predeclared objectives", trial.Number, phase.Name)
				}
			}
		}
	}
	if evidence.SchemaVersion >= 12 {
		if (evidence.Calibration == nil) != (evidence.CalibrationSource == "") {
			return errors.New("closed-loop calibration metadata is incomplete")
		}
		if evidence.Calibration != nil {
			plannedUsers := []int{evidence.BaseUsers}
			if evidence.SpikeUsers > 0 {
				plannedUsers = append(plannedUsers, evidence.SpikeUsers)
			}
			if err := calibration.ValidateClosedLoopEnvelope(*evidence.Calibration, plannedUsers); err != nil {
				return fmt.Errorf("closed-loop calibration envelope is malformed: %w", err)
			}
			if evidence.CalibrationRequired && evidence.Calibration.Status != "READY" {
				return errors.New("required closed-loop calibration evidence is blocked")
			}
		} else if evidence.CalibrationRequired {
			return errors.New("required closed-loop calibration evidence is missing")
		}
	} else if evidence.CalibrationRequired || evidence.Calibration != nil || evidence.CalibrationSource != "" {
		return errors.New("closed-loop calibration metadata is unsupported by this campaign schema")
	}
	if evidence.SchemaVersion < 6 && evidence.LongRun {
		return errors.New("long-run declaration is unsupported by this campaign schema")
	}
	if evidence.SchemaVersion < 7 && evidence.GeneratorGuardrails != nil {
		return errors.New("generator resource guardrails are unsupported by this campaign schema")
	}
	if evidence.SchemaVersion < 9 && evidence.Cooldown != "" {
		return errors.New("cooldown is unsupported by this campaign schema")
	}
	if evidence.SchemaVersion < 9 {
		for _, trial := range evidence.Trials {
			if trial.RunID != "" {
				return errors.New("trial run IDs are unsupported by this campaign schema")
			}
		}
	}
	if evidence.SchemaVersion >= 9 {
		cooldown, err := time.ParseDuration(evidence.Cooldown)
		if err != nil || cooldown < 0 || cooldown > 5*time.Minute {
			return errors.New("invalid saved inter-trial cooldown")
		}
		seenRunIDs := make(map[string]bool, len(evidence.Trials))
		for _, trial := range evidence.Trials {
			if !validCampaignRunID(trial.RunID) || seenRunIDs[trial.RunID] {
				return errors.New("campaign trials must have distinct valid run IDs")
			}
			seenRunIDs[trial.RunID] = true
		}
	}
	if evidence.Outcome == "PASS" && len(evidence.Trials) != evidence.Repeats {
		return errors.New("a passing campaign must contain every configured trial")
	}
	if evidence.Outcome != "PASS" {
		if evidence.Outcome != "FAIL" && evidence.Outcome != "INCONCLUSIVE" && evidence.Outcome != "ABORTED" {
			return errors.New("campaign has an unsupported terminal outcome")
		}
		if evidence.SchemaVersion >= 2 {
			return validatePartialStageEvidence(evidence)
		}
		return validatePartialLegacyTrials(evidence)
	}
	if evidence.SchemaVersion >= 2 {
		return validateStageEvidence(evidence)
	}
	for _, trial := range evidence.Trials {
		if trial.Outcome != "PASS" {
			return errors.New("a passing campaign contains a non-passing trial")
		}
		foundObservation, foundRecovery := false, false
		for _, phase := range trial.Phases {
			switch phase.Name {
			case "observation":
				foundObservation = true
			case "recovery":
				foundRecovery = true
			}
			if phase.Name != "warmup" && phase.Run.Outcome != "PASS" {
				return errors.New("a required campaign phase did not pass")
			}
		}
		if !foundObservation || !foundRecovery {
			return errors.New("a trial is missing observation or recovery evidence")
		}
	}
	return nil
}

func validateWorkloadSource(evidence campaign.Result) error {
	source := evidence.WorkloadSource
	if source == nil {
		return nil
	}
	if evidence.SchemaVersion < 15 {
		return errors.New("workload source metadata is unsupported by this campaign schema")
	}
	if !validSHA256Digest(source.FileSHA256) || !validSHA256Digest(source.SemanticFingerprint) {
		return errors.New("workload source digests must be full SHA-256 values")
	}
	switch source.Type {
	case "scenario_file":
		if source.Name != "" || source.SchemaVersion < 0 || source.SchemaVersion > config.CurrentScenarioVersion {
			return errors.New("scenario-file source metadata is malformed")
		}
	case "campaign_profile":
		if !campaignprofile.ValidName(source.Name) || source.SchemaVersion != campaignprofile.CurrentSchemaVersion {
			return errors.New("campaign-profile source metadata is malformed")
		}
	default:
		return errors.New("workload source type is unsupported")
	}
	foundFingerprint := false
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			fingerprint := phase.Run.Config.WorkloadFingerprint
			if fingerprint != source.SemanticFingerprint {
				return fmt.Errorf("workload source semantic fingerprint does not match trial %d %s phase", trial.Number, phase.Name)
			}
			foundFingerprint = true
		}
	}
	if evidence.PendingTrial != nil {
		for _, phase := range evidence.PendingTrial.Phases {
			if phase.Run.Config.WorkloadFingerprint != source.SemanticFingerprint {
				return fmt.Errorf("workload source semantic fingerprint does not match pending trial %d %s phase", evidence.PendingTrial.Number, phase.Name)
			}
			foundFingerprint = true
		}
	}
	if !foundFingerprint {
		return errors.New("workload source metadata has no phase fingerprint to verify")
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32
}

func validateApplicationMetrics(evidence campaign.Result) error {
	totalBytes := 0
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			run := phase.Run
			if run.ApplicationMetrics == nil {
				continue
			}
			if run.SchemaVersion < 25 {
				return errors.New("application-metrics timeline is not supported by its run-report schema")
			}
			if err := appmetrics.ValidateTimeline(*run.ApplicationMetrics); err != nil {
				return fmt.Errorf("invalid application-metrics timeline in %s phase: %w", phase.Name, err)
			}
			sampleBytes, err := appmetrics.SerializedSamplesBytes(*run.ApplicationMetrics)
			if err != nil {
				return fmt.Errorf("cannot measure application-metrics timeline in %s phase: %w", phase.Name, err)
			}
			totalBytes += sampleBytes
			if totalBytes > appmetrics.MaxTimelineBytes {
				return fmt.Errorf("campaign application-metrics timelines exceed %d bytes", appmetrics.MaxTimelineBytes)
			}
		}
	}
	return nil
}

func checkpointAsAbortedEvidence(evidence campaign.Result) campaign.Result {
	pending := *evidence.PendingTrial
	pending.Outcome = "ABORTED"
	pending.AbortReason = "The saved checkpoint has no terminal trial result; campaign completion is unknown."
	evidence.Trials = append(append([]campaign.Trial(nil), evidence.Trials...), pending)
	evidence.PendingTrial = nil
	evidence.ExecutionState = "COMPLETED"
	evidence.Outcome = "ABORTED"
	return evidence
}

func validatePartialStageEvidence(evidence campaign.Result) error {
	phaseCount := 0
	for _, trial := range evidence.Trials {
		phaseCount += len(trial.Phases)
	}
	if phaseCount == 0 {
		if evidence.Outcome != "INCONCLUSIVE" && evidence.Outcome != "ABORTED" {
			return errors.New("campaign has no phase evidence for its terminal outcome")
		}
		for trialIndex, trial := range evidence.Trials {
			if trial.Number != trialIndex+1 || trial.Outcome != evidence.Outcome {
				return errors.New("empty campaign trials must match the terminal outcome")
			}
			if trial.Outcome == "ABORTED" && strings.TrimSpace(trial.AbortReason) == "" {
				return errors.New("aborted trial has no persisted abort reason")
			}
		}
		return nil
	}
	options, _, err := campaignLevelSchedule(evidence)
	if err != nil {
		return err
	}
	for trialIndex, trial := range evidence.Trials {
		expected := campaign.PlannedPhases(options, evidence.BaseUsers)
		if evidence.SchemaVersion >= 4 {
			expected = campaign.PlannedTrialPhases(options, evidence.BaseUsers, trialIndex+1)
		}
		if trial.Number != trialIndex+1 {
			return errors.New("partial campaign trials are not in declared order")
		}
		last := trialIndex == len(evidence.Trials)-1
		if !last && (trial.Outcome != "PASS" || len(trial.Phases) != len(expected)) {
			return errors.New("a partial campaign has a non-passing trial before its terminal trial")
		}
		if last && trial.Outcome != evidence.Outcome {
			return errors.New("campaign and terminal trial outcomes differ")
		}
		if len(trial.Phases) > len(expected) || trial.Outcome == "PASS" && len(trial.Phases) != len(expected) {
			return errors.New("partial campaign trial phases do not match the declared schedule")
		}
		terminalCause := false
		for phaseIndex, phase := range trial.Phases {
			want := expected[phaseIndex]
			if phase.Name != want.Name || phase.Run.Config.Users != want.Users || phase.Run.Config.Duration != want.Duration.String() || phase.Run.Target != evidence.Target {
				return fmt.Errorf("trial %d has mismatched %s phase evidence", trial.Number, want.Name)
			}
			state, _ := campaign.ClassifyPhase(evidence, phase.Run, phase.Name)
			stability := ""
			legacySchema := evidence.SchemaVersion == 2 || evidence.SchemaVersion == 4
			if legacySchema {
				state = historicalCampaignPhaseState(phase.Run)
				stability = phase.Stability
				if (phase.Name == "observation" || phase.Name == "soak") && !validHistoricalStability(stability) {
					return fmt.Errorf("trial %d has unsupported historical %s stability classification", trial.Number, phase.Name)
				}
			} else if phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak" {
				stability = campaign.AssessStabilityWithPolicy(phase.Run, evidence.EffectiveStabilityPolicy())
			}
			if phase.EvidenceState != state || (phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak") && phase.Stability != stability {
				return fmt.Errorf("trial %d has stale or altered %s phase evidence", trial.Number, phase.Name)
			}
			observedFailure := want.Required && (phase.Run.Outcome == "FAIL" || state == "FAIL_OBSERVED")
			if evidence.SchemaVersion >= 8 && phase.Name == "spike" && phase.Run.Outcome == "FAIL" && state == "FAIL_OBSERVED" {
				if phaseIndex+1 >= len(trial.Phases) || trial.Phases[phaseIndex+1].Name != "recovery" {
					return errors.New("a failed spike phase is missing its recovery observation")
				}
				observedFailure = false
			}
			if observedFailure || phase.Run.Outcome == "ABORTED" || phase.Run.Outcome == "INCONCLUSIVE" || state == "INVALID_DELIVERY" || state == "INSUFFICIENT_EVIDENCE" || (phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak") && stability != "NO_LARGE_DRIFT_DETECTED" {
				if phaseIndex != len(trial.Phases)-1 {
					return errors.New("terminal phase evidence is followed by additional phases")
				}
				terminalCause = true
			}
		}
		if !last {
			continue
		}
		switch trial.Outcome {
		case "PASS":
			if terminalCause {
				return errors.New("passing trial contains terminal phase evidence")
			}
		case "FAIL":
			lastPhaseFailed := len(trial.Phases) > 0 && expected[len(trial.Phases)-1].Required && trial.Phases[len(trial.Phases)-1].Run.Outcome == "FAIL" && trial.Phases[len(trial.Phases)-1].EvidenceState == "FAIL_OBSERVED"
			if !terminalCause && !lastPhaseFailed && !(evidence.SchemaVersion >= 8 && recoveredSpikeFailure(trial)) {
				return errors.New("failed trial has no terminal observed objective breach")
			}
		case "INCONCLUSIVE":
			if !terminalCause {
				return errors.New("inconclusive trial has no terminal evidence limitation")
			}
		case "ABORTED":
			if strings.TrimSpace(trial.AbortReason) == "" {
				return errors.New("aborted trial has no persisted abort reason")
			}
		}
	}
	return nil
}

func historicalCampaignPhaseState(run report.Result) string {
	if run.Quality != nil && run.Quality.State == report.QualityInvalid || run.ArrivalsDropped > 0 ||
		run.ArrivalsCancelledBeforeAdmission != nil && *run.ArrivalsCancelledBeforeAdmission > 0 {
		return "INVALID_DELIVERY"
	}
	switch run.Outcome {
	case "PASS":
		return "PASS_OBSERVED"
	case "FAIL":
		return "FAIL_OBSERVED"
	default:
		return "INSUFFICIENT_EVIDENCE"
	}
}

func validHistoricalStability(value string) bool {
	switch value {
	case "NO_LARGE_DRIFT_DETECTED", "NON_STATIONARY", "INSUFFICIENT_WINDOWS":
		return true
	default:
		return false
	}
}

func validatePartialLegacyTrials(evidence campaign.Result) error {
	for trialIndex, trial := range evidence.Trials {
		if trial.Number != trialIndex+1 {
			return errors.New("partial campaign trials are not in declared order")
		}
		if trialIndex < len(evidence.Trials)-1 {
			if trial.Outcome != "PASS" {
				return errors.New("a partial campaign has a non-passing trial before its terminal trial")
			}
		} else if trial.Outcome != evidence.Outcome {
			return errors.New("campaign and terminal trial outcomes differ")
		}
		if trial.Outcome == "PASS" {
			foundObservation, foundRecovery := false, false
			for _, phase := range trial.Phases {
				switch phase.Name {
				case "observation":
					foundObservation = true
				case "recovery":
					foundRecovery = true
				}
				if phase.Name != "warmup" && phase.Run.Outcome != "PASS" {
					return errors.New("a passing legacy trial contains a non-passing required phase")
				}
			}
			if !foundObservation || !foundRecovery {
				return errors.New("a passing legacy trial is missing observation or recovery evidence")
			}
		}
		if trialIndex == len(evidence.Trials)-1 && trial.Outcome == "FAIL" {
			observedFailure := false
			for _, phase := range trial.Phases {
				observedFailure = observedFailure || phase.Name != "warmup" && phase.Run.Outcome == "FAIL"
			}
			if !observedFailure {
				return errors.New("failed legacy trial has no observed required-phase failure")
			}
		}
	}
	return nil
}

func validateStageEvidence(evidence campaign.Result) error {
	parsePhaseDuration := func(name, value string) (time.Duration, error) {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < 0 {
			return 0, fmt.Errorf("invalid %s duration", name)
		}
		return duration, nil
	}
	warmup, err := parsePhaseDuration("warmup", evidence.Warmup)
	if err != nil || warmup > 3*time.Minute {
		return errors.New("invalid warmup duration")
	}
	observation, err := parsePhaseDuration("observation", evidence.Observation)
	longRun := evidence.SchemaVersion >= 6 && evidence.LongRun
	observationLimit, soakLimit, stabilizationLimit, wallTimeLimit := 10*time.Minute, 3*time.Minute, 3*time.Minute, 15*time.Minute
	if longRun {
		observationLimit, soakLimit, stabilizationLimit, wallTimeLimit = campaign.LongRunPhaseLimit, campaign.LongRunPhaseLimit, campaign.LongRunPhaseLimit, campaign.LongRunWallTimeLimit
		if evidence.DeclaredEnvironment != "staging" {
			return errors.New("long-run campaign is missing its declared staging environment")
		}
	}
	if err != nil || observation < 10*time.Second || observation > observationLimit {
		return errors.New("invalid observation duration")
	}
	stabilization := time.Duration(0)
	if evidence.SchemaVersion >= 11 {
		stabilization, err = parsePhaseDuration("stabilization", evidence.Stabilization)
	} else if evidence.Stabilization != "" {
		return errors.New("stabilization is unsupported by this campaign schema")
	}
	if err != nil || stabilization > stabilizationLimit || stabilization > 0 && stabilization < 10*time.Second {
		return errors.New("invalid stabilization duration")
	}
	spike, err := parsePhaseDuration("spike", evidence.SpikeDuration)
	if err != nil || spike > 3*time.Minute || (spike > 0 && spike < 10*time.Second) || (spike > 0) != (evidence.SpikeUsers > 0) || evidence.SpikeUsers > 50 {
		return errors.New("invalid spike stage")
	}
	recovery, err := parsePhaseDuration("recovery", evidence.Recovery)
	if err != nil || recovery == 0 || recovery > 3*time.Minute {
		return errors.New("a passing campaign requires recovery duration")
	}
	soak := time.Duration(0)
	if evidence.SchemaVersion >= 4 {
		if evidence.Soak != "" {
			soak, err = parsePhaseDuration("soak", evidence.Soak)
		}
		if err != nil || (soak > 0 && (soak < 10*time.Second || soak > soakLimit)) {
			return errors.New("invalid soak stage")
		}
	}
	cooldown := time.Duration(0)
	if evidence.SchemaVersion >= 9 {
		cooldown, err = time.ParseDuration(evidence.Cooldown)
		if err != nil || cooldown < 0 || cooldown > 5*time.Minute {
			return errors.New("invalid cooldown stage")
		}
	}
	budget := time.Duration(0)
	if evidence.Budget != "" {
		budget, err = parsePhaseDuration("budget", evidence.Budget)
	} else if evidence.SchemaVersion < 4 {
		budget = campaign.PlannedBudget(campaign.Options{Warmup: warmup, Observation: observation, SpikeUsers: evidence.SpikeUsers, SpikeDuration: spike, Recovery: recovery, Repeats: evidence.Repeats})
	}
	if err != nil || budget == 0 || budget > wallTimeLimit || budget < time.Duration(evidence.Repeats)*(warmup+stabilization+observation+spike+recovery)+soak || evidence.Target == "" || evidence.BaseUsers > 50 {
		return errors.New("invalid campaign wall-time budget")
	}
	if spike > 0 {
		if evidence.SpikeUsers <= evidence.BaseUsers {
			return errors.New("spike load does not exceed base load")
		}
	}
	options := campaign.Options{Warmup: warmup, Stabilization: stabilization, Observation: observation, SpikeUsers: evidence.SpikeUsers, SpikeDuration: spike, Recovery: recovery, Soak: soak, Cooldown: cooldown, Repeats: evidence.Repeats, LongRun: longRun, StagingConfirmed: longRun}
	if evidence.SchemaVersion >= 7 && evidence.GeneratorGuardrails != nil {
		options.Guardrails = *evidence.GeneratorGuardrails
	}
	if evidence.SchemaVersion >= 4 && budget < campaign.PlannedBudget(options) {
		return errors.New("campaign wall-time budget omits bounded phase drain allowance")
	}
	if evidence.SchemaVersion >= 4 {
		options.Budget = budget
		if err := campaign.Validate(options); err != nil {
			return fmt.Errorf("invalid saved campaign schedule: %w", err)
		}
	}
	for i, trial := range evidence.Trials {
		expected := campaign.PlannedTrialPhases(options, evidence.BaseUsers, i+1)
		if trial.Number != i+1 || trial.Outcome != "PASS" || len(trial.Phases) != len(expected) {
			return errors.New("campaign trial sequence is incomplete")
		}
		for j, phase := range trial.Phases {
			want := expected[j]
			if phase.Name != want.Name || phase.Run.Config.Users != want.Users || phase.Run.Config.Duration != want.Duration.String() || phase.Run.Target != evidence.Target || (phase.Run.Outcome != "PASS" && (phase.Name != "warmup" || phase.Run.Outcome != "FAIL")) {
				return fmt.Errorf("trial %d has mismatched %s phase evidence", i+1, want.Name)
			}
			if !campaignRunGuardrailsMatch(evidence, phase.Run) {
				return fmt.Errorf("trial %d has mismatched %s generator guardrails", i+1, want.Name)
			}
			if evidence.SchemaVersion == 2 || evidence.SchemaVersion == 4 {
				// Historical campaign schemas recorded phase classifications but
				// their embedded run reports predate several fields used by today's
				// classifier. Preserve readability without promoting those reports:
				// AuditCampaign independently recomputes current quality and stability.
				if phase.Name != "warmup" && phase.EvidenceState != "PASS_OBSERVED" {
					return fmt.Errorf("trial %d has inconsistent %s phase classification", i+1, want.Name)
				}
				if (phase.Name == "observation" || phase.Name == "soak") && phase.Stability != "NO_LARGE_DRIFT_DETECTED" {
					return fmt.Errorf("trial %d has inconsistent %s stability classification", i+1, want.Name)
				}
				continue
			}
			state, _ := campaign.ClassifyPhase(evidence, phase.Run, phase.Name)
			if phase.EvidenceState != state || phase.EvidenceState == "INVALID_DELIVERY" || (phase.Name != "warmup" && phase.EvidenceState != "PASS_OBSERVED") {
				return fmt.Errorf("trial %d has inconsistent %s phase classification", i+1, want.Name)
			}
			if (phase.Name == "stabilization" || phase.Name == "observation" || phase.Name == "soak") && (phase.Stability != "NO_LARGE_DRIFT_DETECTED" || phase.Stability != campaign.AssessStabilityWithPolicy(phase.Run, evidence.EffectiveStabilityPolicy())) {
				return fmt.Errorf("trial %d lacks reproducible stable-window evidence", i+1)
			}
		}
	}
	return nil
}

func validCampaignRunID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func campaignRunGuardrailsMatch(evidence campaign.Result, run report.Result) bool {
	want := generatorhealth.Guardrails{}
	if evidence.SchemaVersion >= 7 && evidence.GeneratorGuardrails != nil {
		want = *evidence.GeneratorGuardrails
	}
	got := generatorhealth.Guardrails{}
	if run.Generator != nil && run.Generator.Guardrails != nil {
		got = *run.Generator.Guardrails
	}
	return want.Equal(got)
}

func recoveredSpikeFailure(trial campaign.Trial) bool {
	for index, phase := range trial.Phases {
		if phase.Name == "spike" && phase.Run.Outcome == "FAIL" && phase.EvidenceState == "FAIL_OBSERVED" &&
			index+1 == len(trial.Phases)-1 && trial.Phases[index+1].Name == "recovery" &&
			trial.Phases[index+1].Run.Outcome == "PASS" && trial.Phases[index+1].EvidenceState == "PASS_OBSERVED" {
			return true
		}
	}
	return false
}
