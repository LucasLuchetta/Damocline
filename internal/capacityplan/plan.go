// Package capacityplan compares a business demand target with campaign evidence.
package capacityplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

const maxCampaignReportBytes = 16 << 20

type Result struct {
	State       string   `json:"state"`
	TargetUsers int      `json:"target_users"`
	TestedUsers int      `json:"tested_users"`
	Findings    []string `json:"findings"`
}

func Evaluate(evidence campaign.Result, goal capacitygoal.Goal) (Result, error) {
	return evaluateWithQuality(evidence, goal, report.AssessQuality)
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
	if evidence.SchemaVersion < campaign.CurrentSchemaVersion {
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
		!metrics.P95IID95Approximation || math.IsNaN(metrics.P95IID95LowerMS) || math.IsInf(metrics.P95IID95LowerMS, 0) ||
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
// request and accepts only the report schema emitted by the local campaign command.
func ReadCampaign(path string) (campaign.Result, error) {
	file, err := os.Open(path)
	if err != nil {
		return campaign.Result{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCampaignReportBytes+1))
	if err != nil {
		return campaign.Result{}, err
	}
	if len(data) > maxCampaignReportBytes {
		return campaign.Result{}, errors.New("campaign report exceeds the 16 MiB safety limit")
	}
	var result campaign.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return campaign.Result{}, err
	}
	return result, nil
}

func validateEvidence(evidence campaign.Result) error {
	if (evidence.SchemaVersion != 1 && evidence.SchemaVersion != campaign.CurrentSchemaVersion) || evidence.Scenario == "" || evidence.BaseUsers < 1 || evidence.Repeats < 2 || evidence.Repeats > 5 || len(evidence.Trials) != evidence.Repeats {
		return errors.New("expected a complete supported repeated campaign")
	}
	if evidence.Outcome != "PASS" {
		return nil
	}
	if evidence.SchemaVersion == campaign.CurrentSchemaVersion {
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
	if err != nil || observation < 10*time.Second || observation > 10*time.Minute {
		return errors.New("invalid observation duration")
	}
	spike, err := parsePhaseDuration("spike", evidence.SpikeDuration)
	if err != nil || spike > 3*time.Minute || (spike > 0 && spike < 10*time.Second) || (spike > 0) != (evidence.SpikeUsers > 0) || evidence.SpikeUsers > 50 {
		return errors.New("invalid spike stage")
	}
	recovery, err := parsePhaseDuration("recovery", evidence.Recovery)
	if err != nil || recovery == 0 || recovery > 3*time.Minute {
		return errors.New("a passing campaign requires recovery duration")
	}
	budget, err := parsePhaseDuration("budget", evidence.Budget)
	if err != nil || budget == 0 || budget > 15*time.Minute || budget < time.Duration(evidence.Repeats)*(warmup+observation+spike+recovery) || evidence.Target == "" || evidence.BaseUsers > 50 {
		return errors.New("invalid campaign wall-time budget")
	}
	if spike > 0 {
		if evidence.SpikeUsers <= evidence.BaseUsers {
			return errors.New("spike load does not exceed base load")
		}
	}
	expected := campaign.PlannedPhases(campaign.Options{Warmup: warmup, Observation: observation, SpikeUsers: evidence.SpikeUsers, SpikeDuration: spike, Recovery: recovery}, evidence.BaseUsers)
	for i, trial := range evidence.Trials {
		if trial.Number != i+1 || trial.Outcome != "PASS" || len(trial.Phases) != len(expected) {
			return errors.New("campaign trial sequence is incomplete")
		}
		for j, phase := range trial.Phases {
			want := expected[j]
			if phase.Name != want.Name || phase.Run.Config.Users != want.Users || phase.Run.Config.Duration != want.Duration.String() || phase.Run.Target != evidence.Target || (phase.Run.Outcome != "PASS" && (phase.Name != "warmup" || phase.Run.Outcome != "FAIL")) {
				return fmt.Errorf("trial %d has mismatched %s phase evidence", i+1, want.Name)
			}
			state, _ := campaign.ClassifyEvidence(phase.Run)
			if phase.EvidenceState != state || phase.EvidenceState == "INVALID_DELIVERY" || (phase.Name != "warmup" && phase.EvidenceState != "PASS_OBSERVED") {
				return fmt.Errorf("trial %d has inconsistent %s phase classification", i+1, want.Name)
			}
			if phase.Name == "observation" && (phase.Stability != "NO_LARGE_DRIFT_DETECTED" || phase.Stability != campaign.AssessStability(phase.Run)) {
				return fmt.Errorf("trial %d lacks reproducible stable-window evidence", i+1)
			}
		}
	}
	return nil
}
