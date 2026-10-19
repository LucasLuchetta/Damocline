package capacitygoal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// EvaluateJourneyCampaign checks a saved sustained open-arrival campaign
// offline. It only assesses an exact scheduled journey-start demand target.
func EvaluateJourneyCampaign(saved arrivalcampaign.Result, goal JourneyStartGoal) (JourneyStartAssessment, error) {
	target, err := goal.required()
	if err != nil {
		return JourneyStartAssessment{}, err
	}
	if saved.SchemaVersion != arrivalcampaign.CurrentSchemaVersion || saved.Scenario == "" || saved.Target == "" || saved.WorkloadFingerprint == "" ||
		saved.WorkerLimit < 1 || saved.WorkerLimit > 50 || saved.BaseStartsPerSecond < 1 || saved.BaseStartsPerSecond > 100 ||
		saved.RequestRateCeiling < 1 || saved.RequestTimeout == "" || saved.P95LimitMS <= 0 || saved.MaxErrorRate < 0 || saved.MaxErrorRate >= 1 ||
		saved.MinSamplesPerStep < 1 || saved.Repeats < 2 || saved.Repeats > 5 || len(saved.Trials) == 0 {
		return JourneyStartAssessment{}, errors.New("invalid or empty arrival campaign artifact")
	}
	if saved.PeakStartsPerSecond > 0 {
		expected, calcErr := EvaluatedJourneyStarts(JourneyStartGoal{PeakStartsPerSecond: saved.PeakStartsPerSecond, GrowthPercent: saved.GrowthPercent, ReservePercent: saved.ReservePercent})
		if calcErr != nil || expected != saved.BaseStartsPerSecond || goal.PeakStartsPerSecond != saved.PeakStartsPerSecond || goal.GrowthPercent != saved.GrowthPercent || goal.ReservePercent != saved.ReservePercent {
			return JourneyStartAssessment{}, errors.New("saved campaign demand inputs do not match the requested exact goal")
		}
	}
	options, err := savedOptions(saved)
	if err != nil {
		return JourneyStartAssessment{}, err
	}
	campaignStartedAt, err := time.Parse(time.RFC3339, saved.StartedAt)
	if err != nil {
		return JourneyStartAssessment{}, errors.New("campaign start timestamp is invalid")
	}
	assessment := JourneyStartAssessment{PeakStartsPerSecond: goal.PeakStartsPerSecond, TargetStartsPerSecond: target,
		GrowthPercent: goal.GrowthPercent, ReservePercent: goal.ReservePercent, State: "INSUFFICIENT_EVIDENCE",
		Findings: []string{"Demand and observations use scheduled open-loop journey starts/s; they are not VUs, admitted journeys, completed journeys, or HTTP requests/s.",
			"The campaign is repeated and sustained only for its recorded phases and duration; passing evidence does not establish universal or long-term production capacity.",
			"No infrastructure upgrade is justified by this HTTP evidence alone."},
		Next: []string{"Use representative, independently verified workflows and session identities at the exact demand target.", "Collect time-aligned application and dependency evidence before making a capacity or infrastructure decision."}}
	if err := validateCampaignWindowDuration(saved.WindowDuration, options); err != nil {
		return JourneyStartAssessment{}, err
	}
	if len(saved.Trials) > saved.Repeats {
		return JourneyStartAssessment{}, errors.New("campaign contains more trials than declared")
	}
	if saved.BaseStartsPerSecond != target {
		assessment.State = "TARGET_NOT_DIRECTLY_TESTED"
		assessment.Findings = append(assessment.Findings, fmt.Sprintf("Campaign tested %d scheduled starts/s; required demand is %d/s. This assessor does not interpolate between rates.", saved.BaseStartsPerSecond, target))
		return assessment, nil
	}
	allPass := len(saved.Trials) == saved.Repeats && saved.Outcome == "PASS"
	if saved.Outcome != "PASS" && saved.Outcome != "FAIL" && saved.Outcome != "INCONCLUSIVE" && saved.Outcome != "ABORTED" {
		return JourneyStartAssessment{}, errors.New("campaign outcome is unknown")
	}
	seenStarted := map[string]bool{}
	observedSLOFailure := false
	var baselineConfig []byte
	for index, trial := range saved.Trials {
		if trial.Number != index+1 || len(trial.Phases) > len(arrivalcampaign.PlannedPhases(options, saved.BaseStartsPerSecond, trial.Number)) || len(trial.Phases) == 0 {
			return JourneyStartAssessment{}, errors.New("campaign trial numbering or phase count is invalid")
		}
		planned := arrivalcampaign.PlannedPhases(options, saved.BaseStartsPerSecond, trial.Number)
		phasePass := len(trial.Phases) == len(planned)
		for phaseIndex, phase := range trial.Phases {
			expected := planned[phaseIndex]
			run := phase.Run
			if phase.Name != expected.Name || run.SchemaVersion != report.CurrentSchemaVersion || run.Name != saved.Scenario || run.Target != saved.Target || run.Config.Mode != "arrival" ||
				run.Config.ArrivalRate != expected.Rate || run.Config.Users != saved.WorkerLimit || run.Config.Duration != expected.Duration.String() ||
				run.Config.WorkloadFingerprint != saved.WorkloadFingerprint || run.Config.MaxRPS != saved.RequestRateCeiling ||
				run.Config.P95LimitMS != saved.P95LimitMS || run.Config.MaxErrorRate != saved.MaxErrorRate || run.Config.MinSamplesPerStep != saved.MinSamplesPerStep ||
				run.Config.RequestTimeout != saved.RequestTimeout || run.StartedAt == "" || seenStarted[run.StartedAt] {
				return JourneyStartAssessment{}, errors.New("native phase controls disagree with the campaign manifest or timestamps are duplicated")
			}
			seenStarted[run.StartedAt] = true
			runStartedAt, timeErr := time.Parse(time.RFC3339, run.StartedAt)
			if timeErr != nil || runStartedAt.Before(campaignStartedAt.Add(-time.Second)) {
				return JourneyStartAssessment{}, errors.New("native phase timestamp precedes the campaign or is invalid")
			}
			configBytes, marshalErr := comparableCampaignConfig(run.Config)
			if marshalErr != nil {
				return JourneyStartAssessment{}, errors.New("native phase controls cannot be compared")
			}
			if baselineConfig == nil {
				baselineConfig = configBytes
			} else if !bytes.Equal(baselineConfig, configBytes) {
				return JourneyStartAssessment{}, errors.New("campaign phases do not share the same workload and environment controls")
			}
			state, _ := arrivalcampaign.ClassifyEvidence(run)
			phaseWindow, windowErr := time.ParseDuration(saved.WindowDuration)
			if windowErr != nil {
				return JourneyStartAssessment{}, errors.New("campaign observation window is invalid")
			}
			if phaseWindow > expected.Duration {
				phaseWindow = expected.Duration
			}
			if run.Config.WindowDuration != phaseWindow.String() {
				return JourneyStartAssessment{}, errors.New("native phase window configuration disagrees with campaign manifest")
			}
			stability := ""
			if expected.Name == "observation" || expected.Name == "soak" {
				stability = campaign.AssessStability(run)
			}
			if phase.EvidenceState != state || phase.Stability != stability {
				return JourneyStartAssessment{}, errors.New("saved phase evidence labels disagree with native report data")
			}
			if expected.Required && campaignHasAdequateSLOFailure(run, state, expected.Name == "observation" || expected.Name == "soak") {
				observedSLOFailure = true
			}
			if state == "INSUFFICIENT_EVIDENCE" || state == "INVALID_DELIVERY" {
				if trial.Outcome != "INCONCLUSIVE" {
					return JourneyStartAssessment{}, errors.New("trial outcome hides incomplete or invalid phase evidence")
				}
			}
			if expected.Required && state == "FAIL_OBSERVED" && trial.Outcome != "FAIL" && trial.Outcome != "INCONCLUSIVE" {
				return JourneyStartAssessment{}, errors.New("trial outcome hides a required phase SLO failure")
			}
			if expected.Required && run.Outcome != "PASS" && state == "PASS_OBSERVED" {
				return JourneyStartAssessment{}, errors.New("required phase did not pass its native outcome")
			}
			if expected.Required && (state != "PASS_OBSERVED" || run.Outcome != "PASS" ||
				(expected.Name == "observation" || expected.Name == "soak") && stability != "NO_LARGE_DRIFT_DETECTED" ||
				report.AssessQuality(run).State != report.QualityValid || campaignSLOBreach(run) ||
				campaignCoverageIssue(run, expected.Name == "observation" || expected.Name == "soak") != "" || campaignGeneratorIssue(run) != "") {
				phasePass = false
			}
		}
		if trial.Outcome != "PASS" && trial.Outcome != "FAIL" && trial.Outcome != "INCONCLUSIVE" && trial.Outcome != "ABORTED" {
			return JourneyStartAssessment{}, errors.New("trial outcome is unknown")
		}
		if trial.Outcome == "ABORTED" && trial.AbortReason == "" {
			return JourneyStartAssessment{}, errors.New("aborted trial has no saved reason")
		}
		if trial.Outcome != "PASS" || !phasePass {
			allPass = false
		}
		if trial.Outcome == "PASS" && !phasePass {
			return JourneyStartAssessment{}, errors.New("trial PASS does not match its phase evidence")
		}
		if trial.Outcome == "FAIL" && phasePass {
			return JourneyStartAssessment{}, errors.New("trial FAIL does not match its phase evidence")
		}
		if trial.Outcome == "INCONCLUSIVE" && phasePass {
			return JourneyStartAssessment{}, errors.New("trial INCONCLUSIVE does not match its phase evidence")
		}
		if trial.Outcome != "PASS" && index != len(saved.Trials)-1 {
			return JourneyStartAssessment{}, errors.New("campaign continued after a non-passing or incomplete trial")
		}
	}
	if saved.Outcome == "PASS" && !allPass || saved.Outcome == "ABORTED" && saved.Trials[len(saved.Trials)-1].Outcome != "ABORTED" ||
		saved.Outcome == "FAIL" && saved.Trials[len(saved.Trials)-1].Outcome != "FAIL" || saved.Outcome == "INCONCLUSIVE" && saved.Trials[len(saved.Trials)-1].Outcome != "INCONCLUSIVE" {
		return JourneyStartAssessment{}, errors.New("campaign outcome disagrees with its terminal trial")
	}
	if allPass {
		assessment.State = "SUSTAINED_TARGET_PASSED_UNDER_TESTED_CONDITIONS"
		matched := target
		assessment.MatchedStartsPerSecond = &matched
		assessment.MatchedTrials = len(saved.Trials)
		assessment.Findings = append(assessment.Findings, fmt.Sprintf("All %d trials passed their required phases at exactly %d scheduled journey starts/s, under the saved workload, generator limits, SLOs, and observation schedule.", len(saved.Trials), target))
		return assessment, nil
	}
	if saved.Outcome == "FAIL" && observedSLOFailure {
		assessment.State = "SLO_MISSED_AT_TESTED_TARGET"
		assessment.Findings = append(assessment.Findings, "A required campaign phase failed at the exact target with saved native run evidence. This identifies an observed SLO miss, not its root cause.")
	} else if saved.Outcome == "FAIL" {
		assessment.Findings = append(assessment.Findings, "The campaign ended with a failing phase, but the saved evidence does not establish a valid target SLO miss with adequate delivery, measurement quality and generator headroom.")
	}
	return assessment, nil
}

func comparableCampaignConfig(config report.RunConfig) ([]byte, error) {
	config.Duration = ""
	config.ArrivalRate = 0
	config.WindowDuration = ""
	return json.Marshal(config)
}

func savedOptions(saved arrivalcampaign.Result) (arrivalcampaign.Options, error) {
	parse := func(value string, optional bool) (time.Duration, error) {
		if value == "" && optional {
			return 0, nil
		}
		return time.ParseDuration(value)
	}
	warmup, err := parse(saved.Warmup, false)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	observation, err := parse(saved.Observation, false)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	spikeDuration, err := parse(saved.SpikeDuration, true)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	recovery, err := parse(saved.Recovery, false)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	soak, err := parse(saved.Soak, true)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	budget, err := parse(saved.Budget, false)
	if err != nil {
		return arrivalcampaign.Options{}, err
	}
	options := arrivalcampaign.Options{Warmup: warmup, Observation: observation, SpikeRate: saved.SpikeStartsPerSecond, SpikeDuration: spikeDuration, Recovery: recovery, Soak: soak, Repeats: saved.Repeats, Budget: budget}
	if err := validateSavedOptions(saved, options); err != nil {
		return arrivalcampaign.Options{}, err
	}
	return options, nil
}

func validateSavedOptions(saved arrivalcampaign.Result, options arrivalcampaign.Options) error {
	if options.Warmup < 0 || options.Warmup > 3*time.Minute || options.Recovery < 10*time.Second || options.Recovery > 3*time.Minute ||
		options.Observation < 10*time.Second || options.Observation > 10*time.Minute || options.Repeats < 2 || options.Repeats > 5 ||
		options.SpikeRate < 0 || options.SpikeRate > 100 || (options.SpikeRate == 0) != (options.SpikeDuration == 0) ||
		options.SpikeRate > 0 && (options.SpikeRate <= saved.BaseStartsPerSecond || options.SpikeDuration < 10*time.Second || options.SpikeDuration > 3*time.Minute) ||
		options.Soak < 0 || options.Soak > 3*time.Minute || options.Soak > 0 && options.Soak < 10*time.Second {
		return errors.New("saved campaign phase schedule is invalid")
	}
	steps, err := time.ParseDuration(saved.RequestTimeout)
	if err != nil || steps <= 0 {
		return errors.New("saved request timeout is invalid")
	}
	if saved.BaseStartsPerSecond > saved.RequestRateCeiling || options.SpikeRate > saved.RequestRateCeiling {
		return errors.New("saved campaign rate exceeds its request ceiling")
	}
	phases := 2
	if options.Warmup > 0 {
		phases++
	}
	if options.SpikeDuration > 0 {
		phases++
	}
	total := time.Duration(options.Repeats)*(options.Warmup+options.Observation+options.SpikeDuration+options.Recovery) + options.Soak
	soakDrain := 0
	if options.Soak > 0 {
		soakDrain = 1
	}
	withDrain := total + time.Duration(options.Repeats*phases+soakDrain)*30*time.Second
	if total > 15*time.Minute || withDrain > 15*time.Minute || options.Budget < withDrain || options.Budget > 15*time.Minute {
		return errors.New("saved campaign exceeds its bounded wall-time envelope")
	}
	return nil
}

func validateCampaignWindowDuration(declared string, options arrivalcampaign.Options) error {
	if declared == "" {
		return nil
	}
	duration, err := time.ParseDuration(declared)
	if err != nil || duration < time.Second || duration > time.Minute {
		return errors.New("campaign window duration is invalid")
	}
	for _, phase := range []time.Duration{options.Observation, options.Soak} {
		if phase > 0 && phase/duration < 3 {
			return errors.New("campaign observation or soak does not contain at least three configured windows")
		}
	}
	return nil
}

func campaignCoverageIssue(run report.Result, requireStableWindows bool) string {
	minimum := run.Config.MinSamplesPerStep
	if minimum < 1 || run.Metrics.Total < minimum || run.Metrics.Success == 0 || run.Journeys.Total == 0 || len(run.Steps) == 0 {
		return "aggregate request, journey, or named-step sample coverage is incomplete"
	}
	for _, step := range run.Steps {
		if step.Metrics.Total < minimum || step.Metrics.Success == 0 {
			return "a named step lacks configured sample coverage"
		}
	}
	for _, profile := range run.Profiles {
		if profile.Journeys.Total < minimum || profile.Journeys.Success == 0 {
			return "a workflow profile lacks completed journey sample coverage"
		}
	}
	if len(run.Config.Profiles) > 0 && len(run.Profiles) != len(run.Config.Profiles) {
		return "configured workflow profile evidence is incomplete"
	}
	if run.Config.WindowDuration != "" {
		if requireStableWindows && len(run.Windows) < 3 {
			return "configured observation windows are incomplete"
		}
		for _, window := range run.Windows {
			if window.Metrics.Total < minimum || window.Journeys.Total < minimum || window.Journeys.Success == 0 {
				return "an observation window lacks request or completed-journey sample coverage"
			}
			if len(window.Steps) != len(run.Steps) || len(window.Profiles) != len(run.Profiles) {
				return "an observation window omits configured step or profile evidence"
			}
			for _, item := range window.Steps {
				if item.Metrics.Total < minimum || item.Metrics.Success == 0 {
					return "an observation-window step lacks sample coverage"
				}
			}
			for _, item := range window.Profiles {
				if item.Metrics.Total < minimum || item.Metrics.Success == 0 {
					return "an observation-window profile lacks sample coverage"
				}
			}
		}
	}
	return ""
}

func campaignGeneratorIssue(run report.Result) string {
	if run.Generator == nil || run.Generator.Samples < 2 || !run.Generator.GoCPUValid || run.Generator.ProcessSamples < 2 || !run.Generator.ProcessCPUValid || run.Generator.PeakProcessRSSBytes == 0 || run.Metrics.Total <= 0 || run.WindowSeconds() <= 0 || run.Config.MaxRPS <= 0 ||
		run.ArrivalsDropped > 0 || run.ArrivalsDroppedLate > 0 || run.ArrivalsDroppedBusy > 0 || run.ArrivalsCancelledBeforeAdmission == nil || *run.ArrivalsCancelledBeforeAdmission > 0 || run.Metrics.Cancelled > 0 || run.Journeys.Cancelled > 0 {
		return "generator health or complete workload delivery is not established"
	}
	if run.Generator.GoCPUUtilizationRatio >= .90 || run.Generator.MeanProcessCPUUtilizationRatio >= .90 || float64(run.Metrics.Total)/run.WindowSeconds() >= .90*float64(run.Config.MaxRPS) {
		return "generator or client request-rate guardrail may limit offered load"
	}
	return ""
}

func campaignHasAdequateSLOFailure(run report.Result, state string, stableWindows bool) bool {
	return state == "FAIL_OBSERVED" && report.AssessQuality(run).State == report.QualityValid &&
		campaignSLOBreach(run) && campaignCoverageIssue(run, stableWindows) == "" && campaignGeneratorIssue(run) == ""
}

func campaignSLOBreach(run report.Result) bool {
	breaches := func(m report.MetricSet) bool {
		return m.P95MS > run.Config.P95LimitMS || m.Total > 0 && float64(m.Failures)/float64(m.Total) > run.Config.MaxErrorRate
	}
	if savedRunSLOBreach(run) {
		return true
	}
	if breaches(run.Journeys) {
		return true
	}
	for _, profile := range run.Profiles {
		if breaches(profile.Journeys) {
			return true
		}
	}
	for _, window := range run.Windows {
		if breaches(window.Journeys) {
			return true
		}
		for _, profile := range window.Profiles {
			if breaches(profile.Metrics) {
				return true
			}
		}
	}
	return false
}
