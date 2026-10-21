package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

func renderCampaignCapacityReport(assessment capacityplan.Assessment, source, reportPath string, evidence campaign.Result) string {
	a := assessment.Audit
	var b strings.Builder
	fmt.Fprintln(&b, "# Capacity assessment")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Executive summary")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Assessment:** %s\n- **Observed campaign:** %s\n- **Measurement quality:** %s\n", assessment.State, a.State, a.MeasurementQuality)
	fmt.Fprintf(&b, "- **Workload:** %s at %d modeled active users\n", markdownText(a.Scenario), a.TestedBaseUsers)
	fmt.Fprintf(&b, "- **Observed passing levels:** %v; **failing levels:** %v\n", a.PassingLevels, a.FailingLevels)
	writeMarkdownDemand(&b, assessment.DemandInputs, assessment.Demand)
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "This assessment describes the saved evidence under its declared conditions. It does not certify universal or human-user capacity, establish dependence-aware confidence, or recommend infrastructure changes.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Technical evidence")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Campaign evidence ID:** %s\n- **Campaign schema:** %d\n- **Campaign execution state:** %s\n- **Scenario:** %s\n- **Declared environment:** %s\n- **Target origin:** %s\n- **Start time:** %s\n- **Host type:** %s\n- **Required phase duration reviewed:** %s\n- **Base active users:** %d\n- **Workload fingerprint:** %s\n", markdownCode(a.EvidenceID), a.SchemaVersion, markdownText(evidence.ExecutionState), markdownText(evidence.Scenario), markdownText(evidence.DeclaredEnvironment), markdownText(a.Target), markdownText(a.StartedAt), markdownText(a.HostType), markdownText(a.TestedDuration), evidence.BaseUsers, markdownCode(a.ScenarioFingerprint))
	fmt.Fprintln(&b, "- **Evidence ID scope:** SHA-256 of canonical decoded campaign fields; it is not a hash of original file bytes, a signature, or producer/host/target attestation. Whitespace-only changes and fields unknown to this reader are outside this ID.")
	fmt.Fprintf(&b, "- **Campaign schedule:** warm-up %s; stabilization %s; observation %s; spike %d users for %s; recovery %s; soak %s; cooldown %s; repeats %d; wall-time budget %s\n", markdownOptionalDuration(evidence.Warmup), markdownOptionalDuration(evidence.Stabilization), markdownOptionalDuration(evidence.Observation), evidence.SpikeUsers, markdownOptionalDuration(evidence.SpikeDuration), markdownOptionalDuration(evidence.Recovery), markdownOptionalDuration(evidence.Soak), markdownOptionalDuration(evidence.Cooldown), evidence.Repeats, markdownOptionalDuration(evidence.Budget))
	stability := evidence.EffectiveStabilityPolicy()
	policySource := "persisted"
	if evidence.SchemaVersion < 11 || evidence.StabilityPolicy == nil {
		policySource = "historical default; not persisted in this evidence"
	}
	fmt.Fprintf(&b, "- **Effective drift screen policy** (%s): minimum %d windows, %d samples/window, p95 increase %.3f%% and %.3f ms, throughput decrease %.3f%%\n", policySource, stability.MinimumWindows, stability.MinimumSamplesPerWindow, stability.MaxP95IncreasePercent, stability.MaxP95IncreaseMS, stability.MaxThroughputDropPercent)
	fmt.Fprintf(&b, "- **Generator guardrails:** %s\n- **Exact calibration required:** %s\n", markdownText(generatorGuardrailSummary(evidence.GeneratorGuardrails)), markdownText(calibrationSummary(evidence)))
	if a.Objectives != nil {
		writeMarkdownObjectives(&b, a.Objectives)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Recorded phase workload controls")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Trial | Phase | Active users | Duration | max_rps | p95 limit (ms) | max error rate | Request timeout | Connection policy | Workload fingerprint |")
	fmt.Fprintln(&b, "| ---: | --- | ---: | --- | ---: | ---: | ---: | --- | --- | --- |")
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			cfg := phase.Run.Config
			fmt.Fprintf(&b, "| %d | %s | %d | %s | %d | %.3f | %.5f | %s | %s | %s |\n", trial.Number, markdownText(phase.Name), cfg.Users, markdownText(cfg.Duration), cfg.MaxRPS, cfg.P95LimitMS, cfg.MaxErrorRate, markdownText(cfg.RequestTimeout), markdownText(cfg.ConnectionPolicy), markdownCode(cfg.WorkloadFingerprint))
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Generator environment by phase")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Runtime identity consistency: **%s**; effective CPU capacity consistency: **%s**. Physical host type: **%s**.\n\n", markdownText(a.GeneratorRuntimeStatus), markdownText(a.GeneratorCapacityStatus), markdownText(a.HostType))
	fmt.Fprintln(&b, "| Trial | Phase | OS / architecture | Go version | Runner identity | Logical CPUs | GOMAXPROCS | Effective CPU cores | CPU evidence source |")
	fmt.Fprintln(&b, "| ---: | --- | --- | --- | --- | ---: | ---: | ---: | --- |")
	for _, environment := range a.GeneratorEnvironments {
		identityMethod, identity := "not recorded", "not recorded"
		osArch, goVersion := "not recorded", "not recorded"
		if environment.Identity != nil {
			identityMethod, identity = environment.Identity.RunnerIdentityMethod, environment.Identity.RunnerIdentity
			osArch = environment.Identity.OS + "/" + environment.Identity.Architecture
			goVersion = environment.Identity.GoVersion
		}
		capacity := "not recorded"
		if environment.CPUCapacityKnown {
			capacity = fmt.Sprintf("%.3f", environment.CPUCapacityCores)
		}
		runner := "not recorded"
		if identity != "not recorded" {
			runner = identityMethod + ":" + identity
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %d | %d | %s | %s |\n", environment.Trial, markdownText(environment.Phase), markdownText(osArch), markdownText(goVersion), markdownCode(runner), environment.LogicalCPUs, environment.GOMAXPROCS, capacity, markdownText(environment.CPUCapacitySource))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The phase configuration and fingerprint do not contain a complete request definition. Preserve the private scenario/profile and inputs; verify a saved profile against this campaign with `damocline campaign profile verify --name NAME --campaign <campaign-file>`. Profile verification is offline and generates no traffic.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Phase replay")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Trial | Phase | Delivery verdict | Measurement quality | Stability | Observation seconds | Phase evidence ID |")
	fmt.Fprintln(&b, "| ---: | --- | --- | --- | --- | ---: | --- |")
	for _, phase := range a.Phases {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %.3f | `%s` |\n", phase.Trial, markdownText(phase.Name), markdownText(phase.EvidenceState), markdownText(phase.Measurement), markdownText(phase.Stability), phase.ObservationSecs, phase.EvidenceID)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Observation trials")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Trial | Requests | Request p95 (ms) | Request p99 (ms) | Request p99 interval | Request errors | Completed journeys | Journey p95 (ms) | Journey p99 (ms) | Journey p99 interval | Journey errors | Successful journeys/s |")
	fmt.Fprintln(&b, "| ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | --- | ---: | ---: |")
	for _, trial := range a.ObservationTrials {
		fmt.Fprintf(&b, "| %d | %d | %.3f | %.3f | %s | %s | %d | %.3f | %.3f | %s | %s | %.3f |\n", trial.Trial, trial.Requests, trial.RequestP95MS, trial.RequestP99MS, markdownCode(trial.RequestP99IntervalState), markdownOptionalPercent(trial.RequestErrorRate), trial.CompletedJourneys, trial.JourneyP95MS, trial.JourneyP99MS, markdownCode(trial.JourneyP99IntervalState), markdownOptionalPercent(trial.JourneyErrorRate), trial.SuccessfulJourneysPerSec)
	}
	fmt.Fprintln(&b)
	minimumP99Samples, _ := statistics.MinimumSamplesForPercentileRankInterval95(0.99)
	fmt.Fprintf(&b, "P99 values are nearest-rank descriptive estimates. Exact 95%% rank intervals require at least %d observations and assume IID stationary latency samples; these states do not establish independence, a p99 objective, or capacity confidence.\n", minimumP99Samples)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Repeat-trial pass fraction: **%d/%d** evaluable; excluded incomplete trials: %d; method: %s; interval assumption: %s. The interval is conditional and is not capacity confidence.\n", a.Repeatability.PassingTrials, a.Repeatability.EvaluatedTrials, a.Repeatability.ExcludedTrials, markdownCode(a.Repeatability.Method), markdownCode(a.Repeatability.Assumption))
	if a.ObservationSpread != nil && a.ObservationSpread.Runs == 1 {
		fmt.Fprintln(&b, "Only one completed observation phase is available; independence and repeat spread cannot be assessed.")
	} else if a.ObservationSpread != nil && a.ObservationSpread.Runs > 1 {
		fmt.Fprintf(&b, "Observed request-p95 spread across %d completed observation phases is descriptive; independence is not established.\n", a.ObservationSpread.Runs)
	}
	if a.ObservationSpread != nil && a.ObservationSpread.RequestP95RobustMethod != "" {
		fmt.Fprintf(&b, "Repeat-level request p95: median **%.2f ms**, MAD %.2f ms; descriptive median +/- 3 MAD band %.2f–%.2f ms (%s). This is not a confidence interval and does not account for sequential dependence or non-stationarity.\n",
			a.ObservationSpread.RequestP95MedianMS, a.ObservationSpread.RequestP95MADMS, a.ObservationSpread.RequestP95RobustLowerMS, a.ObservationSpread.RequestP95RobustUpperMS, markdownCode(a.ObservationSpread.RequestP95RobustMethod))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Confidence dimensions: measurement **%s**, workload representativeness **%s**, decision **%s**.\n", markdownText(assessment.Confidence.Measurement), markdownText(assessment.Confidence.WorkloadRepresentativeness), markdownText(assessment.Confidence.Decision))
	writeMarkdownList(&b, "Observed workflow breaches", breachDescriptions(a.WorkflowBreaches))
	writeMarkdownList(&b, "Audit findings", a.Findings)
	writeMarkdownList(&b, "Confidence limits", assessment.Confidence.Findings)
	writeMarkdownList(&b, "Known unknowns", a.Unknowns)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Offline replay")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Source campaign: %s (verify its content against campaign evidence ID %s).\n\n", markdownCode(source), markdownCode(a.EvidenceID))
	fmt.Fprintf(&b, "%s\n", markdownCodeFence("powershell", markdownReplayCommand([]string{source}, []string{a.EvidenceID}, assessment.DemandInputs)))
	fmt.Fprintf(&b, "\nReport generated locally at %s. The saved campaign is the source of truth; keep it with this assessment.\n", markdownCode(reportPath))
	return b.String()
}

func renderArrivalCampaignCapacityReport(assessment capacitygoal.JourneyStartAssessment, source, reportPath string, evidence arrivalcampaign.Result, sourceDigest string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Open-arrival capacity assessment")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Executive summary")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Demand decision:** %s\n- **Observed campaign outcome:** %s\n- **Scenario:** %s\n- **Tested scheduled journey starts/s:** %d\n- **Requested demand target:** %d scheduled journey starts/s (peak %d, growth %.1f%%, reserve %.1f%%)\n", assessment.State, evidence.Outcome, markdownText(evidence.Scenario), evidence.BaseStartsPerSecond, assessment.TargetStartsPerSecond, assessment.PeakStartsPerSecond, assessment.GrowthPercent, assessment.ReservePercent)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The result is direct evidence for the recorded workload, environment declaration and bounded schedule. It does not establish universal or long-term production capacity, workload representativeness, or causal root cause.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Technical evidence")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Campaign schema:** %d\n- **Execution state:** %s\n- **Scenario:** %s\n- **Target origin:** %s\n- **Start time:** %s\n- **Declared workload fingerprint:** %s\n- **Scheduled starts/s:** %d; **worker limit:** %d; **request start ceiling:** %d/s\n", evidence.SchemaVersion, markdownText(evidence.ExecutionState), markdownText(evidence.Scenario), markdownText(evidence.Target), markdownText(evidence.StartedAt), markdownCode(evidence.WorkloadFingerprint), evidence.BaseStartsPerSecond, evidence.WorkerLimit, evidence.RequestRateCeiling)
	fmt.Fprintf(&b, "- **Schedule:** warm-up %s; stabilization %s; observation %s; spike %d starts/s for %s; recovery %s; soak %s; cooldown %s; repeats %d; budget %s\n", markdownText(evidence.Warmup), markdownText(evidence.Stabilization), markdownText(evidence.Observation), evidence.SpikeStartsPerSecond, markdownText(evidence.SpikeDuration), markdownText(evidence.Recovery), markdownText(evidence.Soak), markdownText(evidence.Cooldown), evidence.Repeats, markdownText(evidence.Budget))
	writeMarkdownArrivalObjectives(&b, assessment.Objectives, assessment.StabilityPolicy)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Replayed phase evidence")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Trial | Phase | Scheduled starts | Admitted | Dropped | Completed journeys | Request p95 ms | Request p99 ms | Request p99 interval | Journey p95 ms | Journey p99 ms | Journey p99 interval | Evidence | Stability |")
	fmt.Fprintln(&b, "| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- | --- | --- |")
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			run := phase.Run
			admitted := int64(0)
			if run.ArrivalsAdmitted != nil {
				admitted = *run.ArrivalsAdmitted
			}
			fmt.Fprintf(&b, "| %d | %s | %d | %d | %d | %d | %.3f | %.3f | %s | %.3f | %.3f | %s | %s | %s |\n", trial.Number, markdownText(phase.Name), run.ArrivalsScheduled, admitted, run.ArrivalsDropped, run.Journeys.Total, run.Metrics.P95MS, run.Metrics.P99MS, markdownCode(string(run.Metrics.P99RankIntervalState())), run.Journeys.P95MS, run.Journeys.P99MS, markdownCode(string(run.Journeys.P99RankIntervalState())), markdownText(phase.EvidenceState), markdownText(phase.Stability))
		}
	}
	fmt.Fprintln(&b)
	minimumP99Samples, _ := statistics.MinimumSamplesForPercentileRankInterval95(0.99)
	fmt.Fprintf(&b, "P99 values are nearest-rank descriptive estimates. Exact 95%% rank intervals require at least %d observations and assume IID stationary latency samples; these states do not establish independence, a p99 objective, or capacity confidence.\n", minimumP99Samples)
	fmt.Fprintln(&b)
	writeMarkdownList(&b, "Findings", assessment.Findings)
	writeMarkdownList(&b, "Next experiments", assessment.Next)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Offline replay")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Source campaign: %s; SHA-256 of the exact source bytes: %s. Verify this digest before replay so a renamed or copied artifact can still be identified. Replay the native campaign audit with the same exact demand inputs.\n\n", markdownCode(source), markdownCode(sourceDigest))
	fmt.Fprintf(&b, "%s\n", markdownCodeFence("powershell", fmt.Sprintf("damocline goal --file %s --expect-source-sha256 %s --peak-journeys-per-second %d --growth-percent %s --reserve-percent %s", powershellQuote(source), powershellQuote(sourceDigest), assessment.PeakStartsPerSecond, markdownFloat(assessment.GrowthPercent), markdownFloat(assessment.ReservePercent))))
	fmt.Fprintf(&b, "\nReport generated locally at %s. Keep the private source campaign and scenario together; the digest identifies the saved campaign bytes, not the scenario file or original target.\n", markdownCode(reportPath))
	return b.String()
}

func writeArrivalCampaignAssessmentJSON(path string, assessment capacitygoal.JourneyStartAssessment) error {
	return report.WriteAnyJSON(path, assessment)
}

func renderCampaignCollectionCapacityReport(assessment capacityplan.CampaignCollectionAssessment, sources []string, reportPath string, evidence []campaign.Result) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Capacity level assessment")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Executive summary")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Collection verdict:** %s\n- **Scenario:** %s\n- **Highest passing tested level:** %s\n- **First failing tested level:** %s\n- **Upper bound:** %s\n- **Observed passing levels:** %v; **failing levels:** %v; **inconclusive levels:** %v\n- **Observed pattern:** %s\n", assessment.State, markdownText(assessment.Scenario), optionalLevel(assessment.HighestPassingLevel), optionalLevel(assessment.FirstFailingLevel), markdownText(assessment.UpperBound), assessment.PassingLevels, assessment.FailingLevels, assessment.InconclusiveLevels, markdownText(assessment.ObservationPattern))
	writeMarkdownDemand(&b, assessment.DemandInputs, assessment.Demand)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Tested levels are observations only. The collection does not interpolate untested workloads or certify human-user capacity.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Technical evidence")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Scenario:** %s\n- **Target origin:** %s\n- **Declared workload fingerprint:** `%s`\n- **Collection assessment type:** `%s`\n", markdownText(assessment.Scenario), markdownText(assessment.Target), markdownCode(assessment.WorkloadFingerprint), markdownCode(assessment.AssessmentType))
	if len(assessment.Levels) > 0 && assessment.Levels[0].Assessment.Audit.Objectives != nil {
		writeMarkdownObjectives(&b, assessment.Levels[0].Assessment.Audit.Objectives)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Campaigns by tested level")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Tested active users | Observed outcome | Audit verdict | Measurement quality | Generator identity | CPU capacity | Campaign evidence ID |")
	fmt.Fprintln(&b, "| ---: | --- | --- | --- | --- | --- | --- |")
	for _, level := range assessment.Levels {
		a := level.Assessment.Audit
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | `%s` |\n", level.TestedUsers, markdownText(level.Outcome), markdownText(a.State), markdownText(a.MeasurementQuality), markdownText(a.GeneratorRuntimeStatus), markdownText(a.GeneratorCapacityStatus), a.EvidenceID)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Replayed phase evidence")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Tested users | Trial | Phase | Delivery verdict | Measurement quality | Stability | Duration (s) | Phase evidence ID |")
	fmt.Fprintln(&b, "| ---: | ---: | --- | --- | --- | --- | ---: | --- |")
	for _, level := range assessment.Levels {
		for _, phase := range level.Assessment.Audit.Phases {
			fmt.Fprintf(&b, "| %d | %d | %s | %s | %s | %s | %.3f | %s |\n", level.TestedUsers, phase.Trial, markdownText(phase.Name), markdownText(phase.EvidenceState), markdownText(phase.Measurement), markdownText(phase.Stability), phase.ObservationSecs, markdownCode(phase.EvidenceID))
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Source files")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Tested active users | Campaign source | Campaign evidence ID | Workload fingerprint | Repeats | Observation | Recovery | Budget |")
	fmt.Fprintln(&b, "| ---: | --- | --- | --- | ---: | --- | --- | --- |")
	for i, source := range sources {
		if i >= len(evidence) {
			break
		}
		item := evidence[i]
		audit := capacityplan.AuditCampaign(item)
		workloadFingerprint := audit.ScenarioFingerprint
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %d | %s | %s | %s |\n", item.BaseUsers, markdownCode(source), markdownCode(audit.EvidenceID), markdownCode(workloadFingerprint), item.Repeats, markdownText(item.Observation), markdownText(item.Recovery), markdownText(item.Budget))
	}
	writeMarkdownList(&b, "Collection findings", assessment.Findings)
	writeMarkdownList(&b, "Demand findings", demandFindings(assessment.Demand))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Offline replay")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Source campaigns:")
	for _, source := range sources {
		fmt.Fprintf(&b, "- %s\n", markdownCode(source))
	}
	fmt.Fprintln(&b)
	expectedEvidenceIDs := make([]string, len(evidence))
	for i, item := range evidence {
		expectedEvidenceIDs[i] = capacityplan.AuditCampaign(item).EvidenceID
	}
	fmt.Fprintf(&b, "%s\n", markdownCodeFence("powershell", markdownReplayCommand(sources, expectedEvidenceIDs, assessment.DemandInputs)))
	fmt.Fprintf(&b, "\nReport generated locally at %s. Keep all source campaign files with this assessment.\n", markdownCode(reportPath))
	return b.String()
}

func writeMarkdownArrivalObjectives(b *strings.Builder, objectives *campaign.ObjectiveContract, policy *campaign.StabilityPolicy) {
	fmt.Fprintln(b, "### Predeclared objectives")
	fmt.Fprintln(b)
	if objectives == nil || policy == nil {
		fmt.Fprintln(b, "Not recorded by this historical open-arrival campaign schema.")
		return
	}
	fmt.Fprintf(b, "- Journey scope: %s; p95 <= %.3f ms; maximum journey error rate %.5f%%.\n", markdownText(objectives.JourneyScope), objectives.JourneyP95LimitMS, objectives.MaxJourneyErrorRate*100)
	fmt.Fprintf(b, "- Minimum samples per step: %d; minimum observation windows: %d with at least %d completed journeys/window; window duration: %s.\n", objectives.MinSamplesPerStep, objectives.MinimumObservationWindows, objectives.MinimumSamplesPerWindow, markdownText(objectives.WindowDuration))
	fmt.Fprintf(b, "- Completed-work ratio: %.0f%%; maximum dropped arrivals: %d.\n", objectives.MinCompletedWorkRatio*100, objectives.MaxDroppedArrivals)
	fmt.Fprintf(b, "- Stability screen: minimum %d windows and %d samples/window; maximum p95 increase %.3f%% and %.3f ms; maximum throughput drop %.3f%%. This coarse screen does not establish stationarity.\n", policy.MinimumWindows, policy.MinimumSamplesPerWindow, policy.MaxP95IncreasePercent, policy.MaxP95IncreaseMS, policy.MaxThroughputDropPercent)
}

func optionalLevel(level *int) string {
	if level == nil {
		return "UNKNOWN"
	}
	return strconv.Itoa(*level) + " active users (tested)"
}

func writeMarkdownObjectives(b *strings.Builder, objectives *campaign.ObjectiveContract) {
	fmt.Fprint(b, "\n### Predeclared objectives\n\n")
	if objectives == nil {
		fmt.Fprintln(b, "Not recorded by this historical campaign schema.")
		return
	}
	fmt.Fprintf(b, "- Journey scope: %s; p95 ≤ %.3f ms; maximum journey error rate %.5f%%.\n", markdownText(objectives.JourneyScope), objectives.JourneyP95LimitMS, objectives.MaxJourneyErrorRate*100)
	fmt.Fprintf(b, "- Minimum samples per step: %d; minimum observation windows: %d with at least %d completed journeys/window.\n", objectives.MinSamplesPerStep, objectives.MinimumObservationWindows, objectives.MinimumSamplesPerWindow)
	fmt.Fprintf(b, "- Completed-work ratio: %.0f%%; maximum dropped arrivals: %d; window duration: %s.\n", objectives.MinCompletedWorkRatio*100, objectives.MaxDroppedArrivals, markdownText(objectives.WindowDuration))
}

func writeMarkdownDemand(b *strings.Builder, inputs *capacitygoal.Goal, result *capacityplan.Result) {
	if inputs == nil || result == nil {
		fmt.Fprintln(b, "- **Demand decision:** no demand goal was supplied")
		return
	}
	fmt.Fprintf(b, "- **Demand inputs:** peak %d active users, growth %s%%, reserve %s%%\n", inputs.PeakUsers, markdownFloat(inputs.GrowthPercent), markdownFloat(inputs.ReservePercent))
	fmt.Fprintf(b, "- **Calculated target:** %d workload-equivalent active actors; **plan state:** %s\n", result.TargetUsers, markdownText(result.State))
}

func markdownReplayCommand(sources, evidenceIDs []string, goal *capacitygoal.Goal) string {
	var args []string
	args = append(args, "damocline", "audit")
	for i, source := range sources {
		args = append(args, "--campaign", powershellQuote(source))
		if i < len(evidenceIDs) {
			args = append(args, "--expect-evidence-id", powershellQuote(evidenceIDs[i]))
		}
	}
	if goal != nil {
		args = append(args, "--peak-users", strconv.Itoa(goal.PeakUsers), "--growth-percent", markdownFloat(goal.GrowthPercent), "--reserve-percent", markdownFloat(goal.ReservePercent))
	}
	args = append(args, "--out", "'replayed-assessment.json'", "--report-out", "'replayed-capacity-report.md'")
	return strings.Join(args, " ")
}

func demandFindings(result *capacityplan.Result) []string {
	if result == nil {
		return nil
	}
	return result.Findings
}

func generatorGuardrailSummary(policy *generatorhealth.Guardrails) string {
	if policy == nil || !policy.Enabled() {
		return "disabled"
	}
	return fmt.Sprintf("CPU %.3f%%, RSS %.3f MiB, %d consecutive samples", policy.MaxProcessCPUPercent, float64(policy.MaxProcessRSSBytes)/(1024*1024), policy.ConsecutiveSamples)
}

func calibrationSummary(evidence campaign.Result) string {
	if !evidence.CalibrationRequired {
		return "no exact coverage requirement"
	}
	if evidence.Calibration == nil {
		return "required but no calibration envelope recorded"
	}
	levels := make([]string, 0, len(evidence.Calibration.Levels))
	for _, level := range evidence.Calibration.Levels {
		levels = append(levels, strconv.Itoa(level.Users)+" VUs")
	}
	return fmt.Sprintf("required; envelope %s across %s", evidence.Calibration.Status, strings.Join(levels, ", "))
}

func breachDescriptions(breaches []capacityplan.WorkflowBreach) []string {
	items := make([]string, 0, len(breaches))
	for _, breach := range breaches {
		items = append(items, fmt.Sprintf("Trial %d %s at %d VUs, workflow %s: %s; p95 %.3f/%.3f ms; errors %d/%d (%.3f%%/%.3f%%).", breach.Trial, breach.Phase, breach.TestedUsers, breach.Workflow, strings.Join(breach.Reasons, ", "), breach.P95MS, breach.P95LimitMS, breach.Failures, breach.Total, breach.ErrorRate*100, breach.MaxErrorRate*100))
	}
	return items
}

func writeMarkdownList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", markdownText(item))
	}
}

func markdownOptionalPercent(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return "not recorded"
	}
	return fmt.Sprintf("%.3f%%", *value*100)
}

func markdownOptionalDuration(value string) string {
	if strings.TrimSpace(value) == "" {
		return "not configured"
	}
	return markdownText(value)
}

func markdownText(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "`", "\\`", "|", "\\|", "<", "&lt;", ">", "&gt;", "[", "\\[", "]", "\\]", "\r", " ", "\n", " ").Replace(value)
	return value
}

func markdownCode(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	delimiter := strings.Repeat("`", maxBacktickRun(value)+1)
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		value = " " + value + " "
	}
	return delimiter + value + delimiter
}

func markdownCodeFence(language, value string) string {
	fenceLength := maxBacktickRun(value) + 1
	if fenceLength < 3 {
		fenceLength = 3
	}
	fence := strings.Repeat("`", fenceLength)
	return fence + language + "\n" + value + "\n" + fence
}

func maxBacktickRun(value string) int {
	longest, current := 0, 0
	for _, char := range value {
		if char == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	return longest
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(filepath.Clean(value), "'", "''") + "'"
}

func markdownFloat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func writeCapacityMarkdown(path, content string) error {
	const maxCapacityReportBytes = 1 << 20
	if len(content) > maxCapacityReportBytes {
		return fmt.Errorf("capacity report exceeds the %d-byte output limit", maxCapacityReportBytes)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("capacity report already exists: %s", path)
		}
		return err
	}
	removeOnError := true
	defer func() {
		_ = file.Close()
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	n, err := file.WriteString(content)
	if err != nil {
		return err
	}
	if n != len(content) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	removeOnError = false
	return nil
}
