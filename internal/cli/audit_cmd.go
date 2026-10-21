package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditCmd replays a saved campaign without sending requests or consulting a
// live target. Its observed stage verdict is separate from capacity approval.
func auditCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var paths stringList
	fs.Var(&paths, "campaign", "saved campaign JSON path; repeat for a multi-level offline assessment")
	var expectedEvidenceIDs stringList
	fs.Var(&expectedEvidenceIDs, "expect-evidence-id", "require the campaign SHA-256 evidence ID; repeat in --campaign order")
	var expectedCampaignFileSHA256s stringList
	fs.Var(&expectedCampaignFileSHA256s, "expect-campaign-file-sha256", "require the exact campaign file SHA-256; repeat in --campaign order")
	expectedAuditorSHA256 := fs.String("expect-auditor-sha256", "", "require this exact auditor executable SHA-256 before replay")
	out := fs.String("out", "", "optional combined assessment JSON output path")
	reportOut := fs.String("report-out", "", "optional executive and technical Markdown report path")
	peak := fs.Int("peak-users", 0, "optional expected simultaneous active workload-equivalent users")
	growth := fs.Float64("growth-percent", 0, "optional estimated growth of active demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "optional additional safety reserve (percent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(paths) == 0 || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline audit --campaign campaign.json [--campaign higher-level.json] [--expect-evidence-id sha256:<64-hex>]... [--expect-campaign-file-sha256 sha256:<64-hex>]... [--expect-auditor-sha256 sha256:<64-hex>] [--peak-users 20] [--growth-percent 25] [--reserve-percent 20] [--out assessment.json] [--report-out capacity-report.md]")
		return 2
	}
	if len(paths) > capacityplan.MaxCampaignCollectionSize {
		fmt.Fprintf(stderr, "cannot audit campaigns: at most %d campaign files are allowed\n", capacityplan.MaxCampaignCollectionSize)
		return 2
	}
	if err := validateExpectedEvidenceIDCount(expectedEvidenceIDs, len(paths)); err != nil {
		fmt.Fprintf(stderr, "invalid expected campaign evidence IDs: %v\n", err)
		return 2
	}
	if err := validateExpectedCampaignFileSHA256Count(expectedCampaignFileSHA256s, len(paths)); err != nil {
		fmt.Fprintf(stderr, "invalid expected campaign file SHA-256 values: %v\n", err)
		return 2
	}
	if *expectedAuditorSHA256 != "" && !validAuditorSHA256(*expectedAuditorSHA256) {
		fmt.Fprintln(stderr, "invalid expected auditor SHA-256: use sha256:<64 lowercase hexadecimal characters>")
		return 2
	}
	auditor, err := currentAuditorIdentity()
	if err != nil {
		fmt.Fprintf(stderr, "cannot identify audit executable: %v\n", err)
		return 2
	}
	if *expectedAuditorSHA256 != "" && auditor.ExecutableSHA256 != *expectedAuditorSHA256 {
		fmt.Fprintf(stderr, "auditor executable SHA-256 is %s; expected %s\n", auditor.ExecutableSHA256, *expectedAuditorSHA256)
		return 2
	}
	demandRequested := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "peak-users" || f.Name == "growth-percent" || f.Name == "reserve-percent" {
			demandRequested = true
		}
	})
	var goal *capacitygoal.Goal
	if demandRequested {
		if *peak < 1 {
			fmt.Fprintln(stderr, "--peak-users must be positive when demand options are used")
			return 2
		}
		goal = &capacitygoal.Goal{PeakUsers: *peak, GrowthPercent: *growth, ReservePercent: *reserve}
	}
	if len(paths) > 1 {
		return auditCampaignCollection(paths, expectedEvidenceIDs, expectedCampaignFileSHA256s, auditor, goal, *out, *reportOut, stdout, stderr)
	}
	evidence, sourceFileSHA256, err := capacityplan.ReadCampaignWithFileSHA256(paths[0])
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign: %v\n", err)
		return 2
	}
	if err := verifyExpectedEvidenceIDs(expectedEvidenceIDs, []string{capacityplan.AuditCampaign(evidence).EvidenceID}); err != nil {
		fmt.Fprintf(stderr, "campaign evidence ID check failed: %v\n", err)
		return 2
	}
	if err := verifyExpectedCampaignFileSHA256s(expectedCampaignFileSHA256s, []string{sourceFileSHA256}); err != nil {
		fmt.Fprintf(stderr, "campaign file SHA-256 check failed: %v\n", err)
		return 2
	}
	assessment, err := capacityplan.BuildAssessment(evidence, goal)
	if err != nil {
		fmt.Fprintf(stderr, "cannot calculate business demand: %v\n", err)
		return 2
	}
	assessment.Audit.SourceFileSHA256 = sourceFileSHA256
	assessment.Auditor = auditor
	audit := assessment.Audit
	fmt.Fprintln(stdout, "--- DAMOCLINE | CAMPAIGN EVIDENCE AUDIT (OFFLINE) ---")
	fmt.Fprintf(stdout, "Observed campaign outcome: %s\n", audit.ObservedOutcome)
	if audit.ExecutionState != "" {
		fmt.Fprintf(stdout, "Campaign execution state: %s\n", audit.ExecutionState)
	}
	fmt.Fprintf(stdout, "Audited evidence verdict: %s\n", audit.State)
	fmt.Fprintf(stdout, "Recomputed measurement quality: %s\n", audit.MeasurementQuality)
	fmt.Fprintf(stdout, "Required phases reviewed: %d\n", audit.RequiredPhases)
	fmt.Fprintf(stdout, "Confidence dimensions: measurement=%s | workload representativeness=%s | decision=%s\n", assessment.Confidence.Measurement, assessment.Confidence.WorkloadRepresentativeness, assessment.Confidence.Decision)
	fmt.Fprintf(stdout, "Evidence ID: %s\n", audit.EvidenceID)
	fmt.Fprintln(stdout, "Evidence ID scope: SHA-256 of canonical decoded campaign fields; it is not a hash of original file bytes, signature, or producer/host/target attestation.")
	fmt.Fprintf(stdout, "Source file SHA-256: %s\n", audit.SourceFileSHA256)
	fmt.Fprintf(stdout, "Scenario fingerprint: %s\n", audit.ScenarioFingerprint)
	fmt.Fprintf(stdout, "Host type: %s\n", audit.HostType)
	fmt.Fprintf(stdout, "Tested required duration: %s | decision confidence: %s\n", audit.TestedDuration, assessment.Confidence.Decision)
	fmt.Fprintf(stdout, "Observed passing levels: %v | failing levels: %v\n", audit.PassingLevels, audit.FailingLevels)
	writeObservationEvidence(stdout, audit, "  ")
	if len(audit.LimitingWorkflows) > 0 {
		fmt.Fprintf(stdout, "Workflows with observed SLO breaches: %v\n", audit.LimitingWorkflows)
	}
	writeWorkflowBreaches(stdout, audit.WorkflowBreaches, "  ")
	for _, phase := range audit.Phases {
		fmt.Fprintf(stdout, "  Trial %d %s: %s | measurement %s", phase.Trial, phase.Name, phase.EvidenceState, phase.Measurement)
		if phase.Stability != "" {
			fmt.Fprintf(stdout, " | drift screen %s", phase.Stability)
		}
		fmt.Fprintln(stdout)
	}
	if assessment.Demand != nil {
		fmt.Fprintf(stdout, "Demand target: %d workload-equivalent active actors | plan state: %s\n", assessment.Demand.TargetUsers, assessment.Demand.State)
		for _, finding := range assessment.Demand.Findings {
			fmt.Fprintf(stdout, "  - %s\n", finding)
		}
	}
	for _, finding := range audit.Findings {
		fmt.Fprintf(stdout, "  - %s\n", finding)
	}
	for _, finding := range assessment.Confidence.Findings {
		fmt.Fprintf(stdout, "  Confidence limit: %s\n", finding)
	}
	for _, unknown := range audit.Unknowns {
		fmt.Fprintf(stdout, "  Unknown: %s\n", unknown)
	}
	if *out != "" {
		if err := report.WriteAnyJSON(*out, assessment); err != nil {
			fmt.Fprintf(stderr, "cannot write audit: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Audit JSON: %s\n", *out)
	}
	if *reportOut != "" {
		if err := writeCapacityMarkdown(*reportOut, renderCampaignCapacityReport(assessment, paths[0], evidence)); err != nil {
			fmt.Fprintf(stderr, "cannot write capacity report: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Executive and technical report: %s\n", *reportOut)
	}
	fmt.Fprintln(stdout, "No traffic was generated; the artifact links this verdict to its saved campaign evidence.")
	return 0
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("argument cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func auditCampaignCollection(paths, expectedEvidenceIDs, expectedCampaignFileSHA256s []string, auditor capacityplan.AuditorIdentity, goal *capacitygoal.Goal, out, reportOut string, stdout, stderr io.Writer) int {
	evidence, sourceFileSHA256s, err := capacityplan.ReadCampaignCollectionWithFileSHA256(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign collection: %v\n", err)
		return 2
	}
	actualEvidenceIDs := make([]string, len(evidence))
	for i, item := range evidence {
		actualEvidenceIDs[i] = capacityplan.AuditCampaign(item).EvidenceID
	}
	if err := verifyExpectedEvidenceIDs(expectedEvidenceIDs, actualEvidenceIDs); err != nil {
		fmt.Fprintf(stderr, "campaign evidence ID check failed: %v\n", err)
		return 2
	}
	if err := verifyExpectedCampaignFileSHA256s(expectedCampaignFileSHA256s, sourceFileSHA256s); err != nil {
		fmt.Fprintf(stderr, "campaign file SHA-256 check failed: %v\n", err)
		return 2
	}
	assessment, err := capacityplan.BuildCampaignCollectionAssessment(evidence, goal)
	if err != nil {
		fmt.Fprintf(stderr, "cannot assess campaign collection: %v\n", err)
		return 2
	}
	assessment.Auditor = auditor
	fileSHA256ByLevel := make(map[int]string, len(evidence))
	for index, item := range evidence {
		fileSHA256ByLevel[item.BaseUsers] = sourceFileSHA256s[index]
	}
	for index := range assessment.Levels {
		assessment.Levels[index].Assessment.Audit.SourceFileSHA256 = fileSHA256ByLevel[assessment.Levels[index].TestedUsers]
		assessment.Levels[index].Assessment.Auditor = auditor
	}
	fmt.Fprintln(stdout, "--- DAMOCLINE | CAMPAIGN LEVEL AUDIT (OFFLINE) ---")
	fmt.Fprintf(stdout, "Level collection verdict: %s\n", assessment.State)
	fmt.Fprintf(stdout, "Generator environment consistency: %s\n", assessment.GeneratorEnvironmentStatus)
	if assessment.State == "NOT_COMPARABLE" {
		fmt.Fprintln(stdout, "Scenario, target and workload match: NOT ESTABLISHED")
	} else {
		fmt.Fprintf(stdout, "Scenario: %s | target: %s\n", assessment.Scenario, assessment.Target)
		if assessment.WorkloadFingerprint != "" {
			fmt.Fprintf(stdout, "Workload fingerprint: %s\n", assessment.WorkloadFingerprint)
		}
	}
	fmt.Fprintf(stdout, "Observed passing levels: %v | failing levels: %v | inconclusive levels: %v\n", assessment.PassingLevels, assessment.FailingLevels, assessment.InconclusiveLevels)
	fmt.Fprintf(stdout, "Observed pattern: %s\n", assessment.ObservationPattern)
	for _, level := range assessment.Levels {
		audit := level.Assessment.Audit
		fmt.Fprintf(stdout, "  %d VUs: %s | audit %s | measurement %s | evidence %s\n", level.TestedUsers, level.Outcome, audit.State, audit.MeasurementQuality, audit.EvidenceID)
		writeObservationEvidence(stdout, audit, "    ")
		if len(audit.LimitingWorkflows) > 0 {
			fmt.Fprintf(stdout, "    Observed workflow breaches: %v\n", audit.LimitingWorkflows)
		}
		writeWorkflowBreaches(stdout, audit.WorkflowBreaches, "    ")
	}
	if assessment.Demand != nil {
		fmt.Fprintf(stdout, "Demand target: %d workload-equivalent active actors | plan state: %s\n", assessment.Demand.TargetUsers, assessment.Demand.State)
		if assessment.DemandEvidenceID != "" {
			fmt.Fprintf(stdout, "Demand evidence ID: %s\n", assessment.DemandEvidenceID)
		}
		for _, finding := range assessment.Demand.Findings {
			fmt.Fprintf(stdout, "  - %s\n", finding)
		}
	}
	for _, finding := range assessment.Findings {
		fmt.Fprintf(stdout, "  - %s\n", finding)
	}
	if out != "" {
		if err := report.WriteAnyJSON(out, assessment); err != nil {
			fmt.Fprintf(stderr, "cannot write audit: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Audit JSON: %s\n", out)
	}
	if reportOut != "" {
		if err := writeCapacityMarkdown(reportOut, renderCampaignCollectionCapacityReport(assessment, paths, evidence)); err != nil {
			fmt.Fprintf(stderr, "cannot write capacity report: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Executive and technical report: %s\n", reportOut)
	}
	fmt.Fprintln(stdout, "No traffic was generated; every level remains linked to its saved campaign evidence.")
	return 0
}

func validateExpectedEvidenceIDCount(expected []string, campaignCount int) error {
	if len(expected) == 0 {
		return nil
	}
	if len(expected) != campaignCount {
		return fmt.Errorf("got %d IDs for %d campaign files; provide one ID per --campaign in the same order", len(expected), campaignCount)
	}
	for i, id := range expected {
		if len(id) != len("sha256:")+64 || !strings.HasPrefix(id, "sha256:") {
			return fmt.Errorf("ID %d must use sha256:<64 lowercase hexadecimal characters>", i+1)
		}
		for _, char := range id[len("sha256:"):] {
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return fmt.Errorf("ID %d must use sha256:<64 lowercase hexadecimal characters>", i+1)
			}
		}
	}
	return nil
}

func validateExpectedCampaignFileSHA256Count(expected []string, campaignCount int) error {
	if len(expected) == 0 {
		return nil
	}
	if len(expected) != campaignCount {
		return fmt.Errorf("got %d hashes for %d campaign files; provide one hash per --campaign in the same order", len(expected), campaignCount)
	}
	for index, value := range expected {
		if !validSHA256EvidenceID(value) {
			return fmt.Errorf("hash %d must use sha256:<64 lowercase hexadecimal characters>", index+1)
		}
	}
	return nil
}

func verifyExpectedCampaignFileSHA256s(expected, actual []string) error {
	if err := validateExpectedCampaignFileSHA256Count(expected, len(actual)); err != nil {
		return err
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return fmt.Errorf("campaign %d has source file SHA-256 %s; expected %s", index+1, actual[index], expected[index])
		}
	}
	return nil
}

func validSHA256EvidenceID(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func verifyExpectedEvidenceIDs(expected, actual []string) error {
	if err := validateExpectedEvidenceIDCount(expected, len(actual)); err != nil {
		return err
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return fmt.Errorf("campaign %d has %s; expected %s", i+1, actual[i], expected[i])
		}
	}
	return nil
}

func writeObservationEvidence(w io.Writer, audit capacityplan.CampaignAudit, indent string) {
	repeatability := audit.Repeatability
	if repeatability.Status == "CONDITIONAL_INTERVAL" {
		fmt.Fprintf(w, "%sRepeat pass probability: %d/%d evaluable trials passed; exact 95%% interval %.1f%%–%.1f%%, conditional on independent stationary trials. This is not capacity confidence.\n",
			indent, repeatability.PassingTrials, repeatability.EvaluatedTrials, repeatability.Lower95*100, repeatability.Upper95*100)
	} else if repeatability.ExcludedTrials > 0 {
		fmt.Fprintf(w, "%sRepeat pass probability interval unavailable: %d trial(s) were not terminal PASS/FAIL outcomes.\n", indent, repeatability.ExcludedTrials)
	}
	if len(audit.ObservationTrials) == 0 {
		return
	}
	fmt.Fprintf(w, "%sObservation trial detail (%s):\n", indent, audit.SpreadConsistency)
	for _, observation := range audit.ObservationTrials {
		fmt.Fprintf(w, "%s  Trial %d: requests=%d request-p95=%.2f ms request-errors=%s; completed-journeys=%d journey-p95=%.2f ms journey-errors=%s successful-journeys=%.2f/s\n",
			indent, observation.Trial, observation.Requests, observation.RequestP95MS, formatObservedRate(observation.RequestErrorRate),
			observation.CompletedJourneys, observation.JourneyP95MS, formatObservedRate(observation.JourneyErrorRate),
			observation.SuccessfulJourneysPerSec)
	}
	if spread := audit.ObservationSpread; spread != nil {
		if spread.Runs == 1 {
			fmt.Fprintf(w, "%s  Single completed observation phase: request-p95 %.2f ms, request-errors %.2f%%, request-rate %.2f/s, successful-journeys %.2f/s.\n",
				indent, spread.P95MinMS, spread.ErrorRateMin*100, spread.ThroughputMinRPS, spread.SuccessfulJourneysMinPerSec)
		} else {
			fmt.Fprintf(w, "%s  Observed spread across %d completed observation phases: request-p95 %.2f–%.2f ms, request-errors %.2f–%.2f%%, request-rate %.2f–%.2f/s, successful-journeys %.2f–%.2f/s. Descriptive spread, not a confidence interval; independence is not established.\n",
				indent, spread.Runs, spread.P95MinMS, spread.P95MaxMS, spread.ErrorRateMin*100, spread.ErrorRateMax*100,
				spread.ThroughputMinRPS, spread.ThroughputMaxRPS, spread.SuccessfulJourneysMinPerSec, spread.SuccessfulJourneysMaxPerSec)
		}
		if spread.Runs < 2 || spread.RequestP95RobustMethod == "" {
			fmt.Fprintf(w, "%s  Robust request-p95 repeat summary unavailable: at least two completed observation phases are required.\n", indent)
		} else {
			fmt.Fprintf(w, "%s  Request p95 repeat spread: median %.2f ms, MAD %.2f ms, descriptive median +/- 3 MAD band %.2f–%.2f ms. This does not account for sequential dependence or non-stationarity and is not a confidence interval.\n",
				indent, spread.RequestP95MedianMS, spread.RequestP95MADMS, spread.RequestP95RobustLowerMS, spread.RequestP95RobustUpperMS)
		}
	}
}

func formatObservedRate(rate *float64) string {
	if rate == nil {
		return "not available"
	}
	return fmt.Sprintf("%.2f%%", *rate*100)
}

func writeWorkflowBreaches(w io.Writer, breaches []capacityplan.WorkflowBreach, indent string) {
	for _, breach := range breaches {
		fmt.Fprintf(w, "%sTrial %d %s at %d VUs, %s: %s; p95 %.2f/%.2f ms; errors %d/%d (%.2f%%/%.2f%%).\n",
			indent, breach.Trial, breach.Phase, breach.TestedUsers, breach.Workflow,
			strings.Join(breach.Reasons, ", "), breach.P95MS, breach.P95LimitMS,
			breach.Failures, breach.Total, breach.ErrorRate*100, breach.MaxErrorRate*100)
	}
}
