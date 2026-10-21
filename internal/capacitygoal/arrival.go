package capacitygoal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const generatorUtilizationGuard = 0.90

// JourneyStartGoal expresses demand in scheduled open-loop journey starts per
// second. It is not interchangeable with active VUs or HTTP requests/second.
type JourneyStartGoal struct {
	PeakStartsPerSecond int
	GrowthPercent       float64
	ReservePercent      float64
}

// JourneyStartAssessment reports only direct evidence at an exact scheduled
// journey-start rate. It does not estimate a continuous capacity curve.
type JourneyStartAssessment struct {
	SourceEvidenceID              string `json:"source_evidence_id,omitempty"`
	PeakStartsPerSecond           int
	TargetStartsPerSecond         int
	GrowthPercent                 float64
	ReservePercent                float64
	State                         string
	ObservedPassingStartsPerSec   *int
	FirstObservedFailingStartsSec *int
	MatchedStartsPerSecond        *int
	MatchedTrials                 int
	Findings                      []string
	Next                          []string
	Objectives                    *campaign.ObjectiveContract `json:"predeclared_objectives,omitempty"`
	StabilityPolicy               *campaign.StabilityPolicy   `json:"stability_policy,omitempty"`
}

func (g JourneyStartGoal) required() (int, error) {
	if g.PeakStartsPerSecond < 1 || g.PeakStartsPerSecond > 1000000 {
		return 0, errors.New("peak-journeys-per-second must be between 1 and 1000000")
	}
	for _, p := range []float64{g.GrowthPercent, g.ReservePercent} {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1000 {
			return 0, errors.New("growth-percent and reserve-percent must be finite values from 0 to 1000")
		}
	}
	value := math.Ceil(float64(g.PeakStartsPerSecond) * (1 + g.GrowthPercent/100) * (1 + g.ReservePercent/100))
	if math.IsInf(value, 0) || value > float64(int(^uint(0)>>1)) {
		return 0, errors.New("calculated journey-start target overflows integer bounds")
	}
	return int(value), nil
}

func EvaluatedJourneyStarts(g JourneyStartGoal) (int, error) { return g.required() }

// EvaluateJourneyStarts checks a saved arrival exploration offline. It
// recomputes target-run evidence rather than trusting the saved probe label.
func EvaluateJourneyStarts(ex arrivalexplorer.Result, g JourneyStartGoal) (JourneyStartAssessment, error) {
	target, err := g.required()
	if err != nil {
		return JourneyStartAssessment{}, err
	}
	stage, err := time.ParseDuration(ex.StageDuration)
	if (ex.SchemaVersion != 1 && ex.SchemaVersion != 2) || ex.Scenario == "" || ex.WorkerLimit < 1 || ex.WorkerLimit > 50 ||
		ex.MinJourneysPerSecond < 1 || ex.MaxJourneysPerSecond < ex.MinJourneysPerSecond || ex.MaxJourneysPerSecond > 100 ||
		ex.Repeats < 2 || ex.Repeats > 4 || len(ex.Probes) == 0 || err != nil || stage < 10*time.Second || stage > 3*time.Minute {
		return JourneyStartAssessment{}, errors.New("invalid or empty arrival exploration: expected bounded repeated schema-v1 probes")
	}
	if ex.SchemaVersion == 2 {
		if ex.RequestedMinJourneysPerSecond < 1 || ex.RequestedMaxJourneysPerSecond < ex.RequestedMinJourneysPerSecond ||
			ex.RequestedMaxJourneysPerSecond > 100 || len(ex.CalibratedProbeRates) < 1 ||
			ex.MinJourneysPerSecond < ex.RequestedMinJourneysPerSecond || ex.MaxJourneysPerSecond > ex.RequestedMaxJourneysPerSecond ||
			ex.CalibratedProbeRates[0] != ex.MinJourneysPerSecond || ex.CalibratedProbeRates[len(ex.CalibratedProbeRates)-1] != ex.MaxJourneysPerSecond {
			return JourneyStartAssessment{}, errors.New("invalid calibrated arrival exploration rate envelope")
		}
		for i, rate := range ex.CalibratedProbeRates {
			if rate < ex.MinJourneysPerSecond || rate > ex.MaxJourneysPerSecond || i > 0 && rate <= ex.CalibratedProbeRates[i-1] {
				return JourneyStartAssessment{}, errors.New("calibrated arrival exploration rates are not unique and ascending")
			}
		}
		if ex.TargetJourneysPerSecond > 0 && ex.TargetJourneysPerSecond >= ex.RequestedMinJourneysPerSecond &&
			ex.TargetJourneysPerSecond <= ex.RequestedMaxJourneysPerSecond && !containsCalibratedRate(ex.CalibratedProbeRates, ex.TargetJourneysPerSecond) {
			return JourneyStartAssessment{}, errors.New("arrival report exact target has no calibrated probe profile")
		}
	}

	a := JourneyStartAssessment{
		PeakStartsPerSecond: g.PeakStartsPerSecond, TargetStartsPerSecond: target,
		GrowthPercent: g.GrowthPercent, ReservePercent: g.ReservePercent,
		State: "INSUFFICIENT_EVIDENCE", ObservedPassingStartsPerSec: ex.HighestPassingJourneysPerSecond,
		FirstObservedFailingStartsSec: ex.FirstFailingJourneysPerSecond,
		Findings: []string{
			"Demand and observations use scheduled open-loop journey starts per second. They are not active virtual users, concurrent sessions, HTTP requests per second, or completed useful journeys per second.",
			"Arrival exploration uses short repeated stages. A passing target does not establish sustained capacity, representative users, or long-term stability.",
			"No infrastructure upgrade is justified by this HTTP evidence alone.",
		},
		Next: []string{
			"Run the exact demand target with a representative, independently verified workload and session model.",
			"Use a repeated sustained campaign with application and generator evidence before making a capacity claim.",
		},
	}
	var matched *arrivalexplorer.Probe
	seenRates := make(map[int]bool, len(ex.Probes))
	exactConfiguredTargetSeen := false
	for i := range ex.Probes {
		p := &ex.Probes[i]
		if p.JourneysPerSecond < ex.MinJourneysPerSecond || p.JourneysPerSecond > ex.MaxJourneysPerSecond || seenRates[p.JourneysPerSecond] {
			return JourneyStartAssessment{}, errors.New("arrival report contains duplicate or out-of-bounds probe rates")
		}
		if ex.SchemaVersion == 2 && !containsCalibratedRate(ex.CalibratedProbeRates, p.JourneysPerSecond) {
			return JourneyStartAssessment{}, errors.New("arrival report contains a probe outside its calibrated rate envelope")
		}
		seenRates[p.JourneysPerSecond] = true
		if ex.TargetJourneysPerSecond > 0 && p.JourneysPerSecond == ex.TargetJourneysPerSecond {
			exactConfiguredTargetSeen = true
		}
		if p.JourneysPerSecond == target {
			if matched != nil {
				return JourneyStartAssessment{}, errors.New("duplicate exact target probe is ambiguous")
			}
			matched = p
		}
	}
	if ex.TargetTested != (ex.TargetJourneysPerSecond > 0 && exactConfiguredTargetSeen) {
		return JourneyStartAssessment{}, errors.New("arrival report exact-target marker disagrees with its recorded probes")
	}
	for i := range ex.Probes {
		if issue := validateProbeRecord(ex.Probes[i], ex, stage); issue != "" {
			return JourneyStartAssessment{}, errors.New(issue)
		}
	}
	if target > ex.MaxJourneysPerSecond {
		a.State = "OUTSIDE_TESTED_CEILING"
		a.Findings = append(a.Findings, fmt.Sprintf("Required demand of %d scheduled journey starts/s exceeds the configured exploration ceiling of %d/s. No conclusion about meeting that demand is possible.", target, ex.MaxJourneysPerSecond))
		return a, nil
	}
	if target < ex.MinJourneysPerSecond {
		a.State = "TARGET_NOT_DIRECTLY_TESTED"
		a.Findings = append(a.Findings, fmt.Sprintf("Required demand of %d scheduled journey starts/s is below the configured exploration floor of %d/s and was not tested exactly.", target, ex.MinJourneysPerSecond))
		return a, nil
	}
	if ex.TargetJourneysPerSecond > 0 && ex.TargetJourneysPerSecond != target {
		a.Findings = append(a.Findings, fmt.Sprintf("The saved explorer was configured for exact target %d/s, which differs from the current required demand of %d/s.", ex.TargetJourneysPerSecond, target))
		return a, nil
	}
	if matched == nil {
		a.State = "TARGET_NOT_DIRECTLY_TESTED"
		a.Findings = append(a.Findings, fmt.Sprintf("No recorded arrival probe exercised the exact required rate of %d scheduled journey starts/s. Nearby rates cannot establish the target.", target))
		return a, nil
	}
	matchedRate := matched.JourneysPerSecond
	a.MatchedStartsPerSecond = &matchedRate
	a.MatchedTrials = len(matched.Runs)

	derivedStatus, issue := assessSavedProbe(*matched, ex, stage)
	if issue != "" {
		a.Findings = append(a.Findings, issue)
		return a, nil
	}
	if derivedStatus == "FAIL_OBSERVED" {
		a.State = "SLO_MISSED_AT_TESTED_TARGET"
		a.Findings = append(a.Findings, fmt.Sprintf("At %d scheduled journey starts/s, a recorded run missed the configured objectives with adequate generator evidence. This does not identify a bottleneck or justify a hardware change.", target))
		a.Next = append([]string{"Inspect workflow outcomes and collect time-aligned application and dependency evidence before changing infrastructure."}, a.Next...)
		return a, nil
	}
	if derivedStatus != "PASS_OBSERVED" {
		a.Findings = append(a.Findings, "The exact target lacks repeated passing runs with reproducible workload, delivery, and generator evidence.")
		return a, nil
	}
	a.State = "SHORT_TRIAL_TARGET_PASSED"
	a.Findings = append(a.Findings, fmt.Sprintf("%d short trials at the exact %d scheduled journey starts/s passed their configured criteria. This is not sustained capacity or a conversion to users or HTTP requests/s.", ex.Repeats, target))
	return a, nil
}

func containsCalibratedRate(rates []int, target int) bool {
	index := sort.SearchInts(rates, target)
	return index < len(rates) && rates[index] == target
}

func validateProbeRecord(p arrivalexplorer.Probe, ex arrivalexplorer.Result, stage time.Duration) string {
	if len(p.Runs) > ex.Repeats {
		return "arrival report contains more runs than the declared repeat count"
	}
	var firstConfig []byte
	seenStartedAt := make(map[string]bool, len(p.Runs))
	for index, run := range p.Runs {
		if run.SchemaVersion != report.CurrentSchemaVersion {
			continue // Older, non-target probes remain descriptive legacy evidence.
		}
		if run.Config.Mode != "arrival" || run.Config.ArrivalRate != p.JourneysPerSecond || run.Config.Users != ex.WorkerLimit || run.Config.Duration != stage.String() {
			return "arrival report native run controls disagree with their declared probe rate or stage"
		}
		if run.Config.WorkloadFingerprint == "" || run.StartedAt == "" || seenStartedAt[run.StartedAt] {
			return "arrival report native run lacks a workload fingerprint or unique trial timestamp"
		}
		seenStartedAt[run.StartedAt] = true
		configBytes, err := json.Marshal(run.Config)
		if err != nil {
			return "arrival report native run configuration cannot be compared"
		}
		if index == 0 {
			firstConfig = configBytes
		} else if !bytes.Equal(configBytes, firstConfig) {
			return "repeated arrival trials use different saved workload or SLO controls"
		}
	}
	return ""
}

func assessSavedProbe(p arrivalexplorer.Probe, ex arrivalexplorer.Result, stage time.Duration) (string, string) {
	if len(p.Runs) == 0 {
		if p.Status == "ABORTED" && p.Reason != "" {
			return "ABORTED", "The exact target probe was aborted before a run completed."
		}
		return "INSUFFICIENT_EVIDENCE", "The exact target probe contains no native run evidence."
	}
	if len(p.Runs) > ex.Repeats {
		return "", "The exact target probe contains more runs than its declared repeat count."
	}
	if p.Status == "ABORTED" && p.Reason != "" {
		for _, run := range p.Runs {
			if run.Outcome == "ABORTED" {
				return "ABORTED", "The probe deadline or caller interrupted a target trial."
			}
		}
		return "ABORTED", "The exact target probe was aborted before all declared repeated trials completed."
	}
	allPass := true
	for index, run := range p.Runs {
		if run.SchemaVersion != report.CurrentSchemaVersion {
			return "INSUFFICIENT_EVIDENCE", "The exact target contains a native report schema that this assessor cannot fully verify."
		}
		state, reason := classifySavedRun(run)
		if state == "PASS_OBSERVED" {
			continue
		}
		allPass = false
		if state != "PASS_OBSERVED" {
			if index != len(p.Runs)-1 {
				return "", "The exact target contains runs after its first non-passing or incomplete result."
			}
			if state != p.Status {
				return "", "The saved exact-target probe label disagrees with its native run evidence."
			}
			if state == "FAIL_OBSERVED" {
				return state, ""
			}
			return state, reason
		}
	}
	if allPass && len(p.Runs) == ex.Repeats && p.Status == "PASS_OBSERVED" {
		return "PASS_OBSERVED", ""
	}
	if allPass && p.Status == "ABORTED" && p.Reason != "" {
		return "ABORTED", "The probe stopped before completing all required repeated passing runs."
	}
	return "", "The exact-target probe label disagrees with the count or outcome of its recorded runs."
}

func classifySavedRun(run report.Result) (string, string) {
	if run.Outcome == "ABORTED" {
		return "ABORTED", "The exact target was interrupted."
	}
	if run.SchemaVersion != report.CurrentSchemaVersion {
		return "INSUFFICIENT_EVIDENCE", "The exact target contains a native report schema that this assessor cannot fully verify."
	}
	quality := report.AssessQuality(run)
	if quality == nil || quality.State == report.QualityInvalid {
		return "INVALID_DELIVERY", "The exact target has invalid report or delivery accounting."
	}
	if quality.State != report.QualityValid {
		return "INSUFFICIENT_EVIDENCE", "The exact target report quality is LIMITED, so it cannot support a positive demand finding."
	}
	if issue := savedRunCoverageIssue(run); issue != "" {
		return "INSUFFICIENT_EVIDENCE", issue
	}
	if issue := savedRunGeneratorIssue(run); issue != "" {
		return "INVALID_DELIVERY", issue
	}
	if run.Config.P95LimitMS <= 0 || run.Config.MaxErrorRate < 0 || run.Config.MaxErrorRate >= 1 {
		return "INSUFFICIENT_EVIDENCE", "The exact target has no usable latency or error objective."
	}
	breached := savedRunSLOBreach(run)
	if run.Outcome == "PASS" && breached {
		return "INVALID_DELIVERY", "The native run says PASS although its saved metrics breach a configured objective."
	}
	if run.Outcome == "FAIL" && !breached {
		return "INSUFFICIENT_EVIDENCE", "The native run says FAIL but the saved metrics do not reproduce an objective breach."
	}
	state, reason := campaign.ClassifyEvidence(run)
	return state, reason
}

func savedRunSLOBreach(run report.Result) bool {
	breaches := func(metrics report.MetricSet) bool {
		return metrics.P95MS > run.Config.P95LimitMS ||
			metrics.Total > 0 && float64(metrics.Failures)/float64(metrics.Total) > run.Config.MaxErrorRate
	}
	if breaches(run.Metrics) || breaches(run.Journeys) {
		return true
	}
	for _, step := range run.Steps {
		if breaches(step.Metrics) {
			return true
		}
	}
	for _, profile := range run.Profiles {
		if breaches(profile.Journeys) {
			return true
		}
	}
	for _, window := range run.Windows {
		if breaches(window.Metrics) || breaches(window.Journeys) {
			return true
		}
		for _, step := range window.Steps {
			if breaches(step.Metrics) {
				return true
			}
		}
		for _, profile := range window.Profiles {
			if breaches(profile.Metrics) {
				return true
			}
		}
	}
	return false
}

func savedRunCoverageIssue(run report.Result) string {
	minimum := run.Config.MinSamplesPerStep
	if minimum < 1 || run.Metrics.Total < minimum || run.Metrics.Success == 0 || run.Journeys.Total == 0 || len(run.Steps) == 0 {
		return "The exact target lacks minimum aggregate request, journey, or named-step samples."
	}
	for _, step := range run.Steps {
		if step.Metrics.Total < minimum || step.Metrics.Success == 0 {
			return "A named workflow step lacks the configured minimum successful samples."
		}
	}
	for _, profile := range run.Profiles {
		if profile.Journeys.Total < minimum || profile.Journeys.Success == 0 {
			return "A workflow profile lacks the configured minimum successful journeys."
		}
	}
	if len(run.Config.Profiles) > 0 && len(run.Profiles) != len(run.Config.Profiles) {
		return "The exact target is missing one or more configured workflow profiles."
	}
	if run.Config.WindowDuration != "" {
		if len(run.Windows) == 0 || run.Config.WindowDuration == "" {
			return "The exact target is missing configured observation-window evidence."
		}
		for _, window := range run.Windows {
			if window.Metrics.Total < minimum || window.Journeys.Total < minimum || window.Journeys.Success == 0 {
				return "An observation window lacks request or completed-journey samples."
			}
			for _, step := range window.Steps {
				if step.Metrics.Total < minimum || step.Metrics.Success == 0 {
					return "An observation-window step lacks the configured minimum successful samples."
				}
			}
			for _, profile := range window.Profiles {
				if profile.Metrics.Total < minimum || profile.Metrics.Success == 0 {
					return "An observation-window workflow profile lacks the configured minimum successful journeys."
				}
			}
		}
	}
	return ""
}

func savedRunGeneratorIssue(run report.Result) string {
	if run.Metrics.Total <= 0 || run.Generator == nil || run.Generator.Samples < 2 || !run.Generator.GoCPUValid ||
		run.Generator.ProcessSamples < 2 || !run.Generator.ProcessCPUValid || run.Generator.PeakProcessRSSBytes == 0 ||
		run.ArrivalsDropped > 0 || run.ArrivalsDroppedLate > 0 || run.ArrivalsDroppedBusy > 0 ||
		run.ArrivalsCancelledBeforeAdmission == nil || *run.ArrivalsCancelledBeforeAdmission > 0 ||
		run.Metrics.Cancelled > 0 || run.Journeys.Cancelled > 0 || run.Config.MaxRPS <= 0 || run.WindowSeconds() <= 0 {
		return "Generator health or complete workload delivery is not established for the exact target."
	}
	if run.Generator.GoCPUUtilizationRatio >= generatorUtilizationGuard ||
		run.Generator.MeanProcessCPUUtilizationRatio >= generatorUtilizationGuard ||
		float64(run.Metrics.Total)/run.WindowSeconds() >= generatorUtilizationGuard*float64(run.Config.MaxRPS) {
		return "Generator CPU or the configured client request-rate ceiling may be limiting the exact target."
	}
	return ""
}
