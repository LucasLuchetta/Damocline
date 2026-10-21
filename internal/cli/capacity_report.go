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

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
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
	fmt.Fprintf(&b, "- **Campaign schedule:** warm-up %s; stabilization %s; observation %s; spike %d users for %s; recovery %s; soak %s; cooldown %s; repeats %d; wall-time budget %s\n", markdownText(evidence.Warmup), markdownText(evidence.Stabilization), markdownText(evidence.Observation), evidence.SpikeUsers, markdownText(evidence.SpikeDuration), markdownText(evidence.Recovery), markdownText(evidence.Soak), markdownText(evidence.Cooldown), evidence.Repeats, markdownText(evidence.Budget))
	stability := evidence.EffectiveStabilityPolicy()
	fmt.Fprintf(&b, "- **Effective drift screen policy** (historical default when not persisted): minimum %d windows, %d samples/window, p95 increase %.3f%% and %.3f ms, throughput decrease %.3f%%\n", stability.MinimumWindows, stability.MinimumSamplesPerWindow, stability.MaxP95IncreasePercent, stability.MaxP95IncreaseMS, stability.MaxThroughputDropPercent)
	fmt.Fprintf(&b, "- **Generator guardrails:** %s\n- **Exact calibration required:** %s\n", markdownText(generatorGuardrailSummary(evidence.GeneratorGuardrails)), markdownText(calibrationSummary(evidence)))
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
	fmt.Fprintln(&b, "### Independent observation trials")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Trial | Requests | Request p95 (ms) | Request errors | Completed journeys | Journey p95 (ms) | Journey errors | Successful journeys/s |")
	fmt.Fprintln(&b, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, trial := range a.ObservationTrials {
		fmt.Fprintf(&b, "| %d | %d | %.3f | %s | %d | %.3f | %s | %.3f |\n", trial.Trial, trial.Requests, trial.RequestP95MS, markdownOptionalPercent(trial.RequestErrorRate), trial.CompletedJourneys, trial.JourneyP95MS, markdownOptionalPercent(trial.JourneyErrorRate), trial.SuccessfulJourneysPerSec)
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Repeat-trial pass fraction: **%d/%d** evaluable; excluded incomplete trials: %d; method: %s; interval assumption: %s. The interval is conditional and is not capacity confidence.\n", a.Repeatability.PassingTrials, a.Repeatability.EvaluatedTrials, a.Repeatability.ExcludedTrials, markdownCode(a.Repeatability.Method), markdownCode(a.Repeatability.Assumption))
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

func renderCampaignCollectionCapacityReport(assessment capacityplan.CampaignCollectionAssessment, sources []string, reportPath string, evidence []campaign.Result) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Capacity level assessment")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Executive summary")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Collection verdict:** %s\n- **Scenario:** %s\n- **Observed passing levels:** %v; **failing levels:** %v; **inconclusive levels:** %v\n- **Observed pattern:** %s\n", assessment.State, markdownText(assessment.Scenario), assessment.PassingLevels, assessment.FailingLevels, assessment.InconclusiveLevels, markdownText(assessment.ObservationPattern))
	writeMarkdownDemand(&b, assessment.DemandInputs, assessment.Demand)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Tested levels are observations only. The collection does not interpolate untested workloads or certify human-user capacity.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Technical evidence")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Scenario:** %s\n- **Target origin:** %s\n- **Declared workload fingerprint:** `%s`\n- **Collection assessment type:** `%s`\n", markdownText(assessment.Scenario), markdownText(assessment.Target), markdownCode(assessment.WorkloadFingerprint), markdownCode(assessment.AssessmentType))
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
