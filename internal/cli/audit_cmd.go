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
	out := fs.String("out", "", "optional combined assessment JSON output path")
	peak := fs.Int("peak-users", 0, "optional expected simultaneous active workload-equivalent users")
	growth := fs.Float64("growth-percent", 0, "optional estimated growth of active demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "optional additional safety reserve (percent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(paths) == 0 || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline audit --campaign campaign.json [--campaign higher-level.json] [--peak-users 20] [--growth-percent 25] [--reserve-percent 20] [--out assessment.json]")
		return 2
	}
	if len(paths) > capacityplan.MaxCampaignCollectionSize {
		fmt.Fprintf(stderr, "cannot audit campaigns: at most %d campaign files are allowed\n", capacityplan.MaxCampaignCollectionSize)
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
		return auditCampaignCollection(paths, goal, *out, stdout, stderr)
	}
	evidence, err := capacityplan.ReadCampaign(paths[0])
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign: %v\n", err)
		return 2
	}
	assessment, err := capacityplan.BuildAssessment(evidence, goal)
	if err != nil {
		fmt.Fprintf(stderr, "cannot calculate business demand: %v\n", err)
		return 2
	}
	audit := assessment.Audit
	fmt.Fprintln(stdout, "--- DAMOCLINE | CAMPAIGN EVIDENCE AUDIT (OFFLINE) ---")
	fmt.Fprintf(stdout, "Observed campaign outcome: %s\n", audit.ObservedOutcome)
	fmt.Fprintf(stdout, "Audited evidence verdict: %s\n", audit.State)
	fmt.Fprintf(stdout, "Recomputed measurement quality: %s\n", audit.MeasurementQuality)
	fmt.Fprintf(stdout, "Required phases reviewed: %d\n", audit.RequiredPhases)
	fmt.Fprintf(stdout, "Confidence dimensions: measurement=%s | workload representativeness=%s | decision=%s\n", assessment.Confidence.Measurement, assessment.Confidence.WorkloadRepresentativeness, assessment.Confidence.Decision)
	fmt.Fprintf(stdout, "Evidence ID: %s\n", audit.EvidenceID)
	fmt.Fprintf(stdout, "Scenario fingerprint: %s\n", audit.ScenarioFingerprint)
	fmt.Fprintf(stdout, "Host type: %s\n", audit.HostType)
	fmt.Fprintf(stdout, "Tested required duration: %s | decision confidence: %s\n", audit.TestedDuration, assessment.Confidence.Decision)
	fmt.Fprintf(stdout, "Observed passing levels: %v | failing levels: %v\n", audit.PassingLevels, audit.FailingLevels)
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
	fmt.Fprintln(stdout, "No traffic was generated; the artifact links this verdict to its saved campaign evidence.")
	return 0
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("campaign path cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func auditCampaignCollection(paths []string, goal *capacitygoal.Goal, out string, stdout, stderr io.Writer) int {
	evidence, err := capacityplan.ReadCampaignCollection(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign collection: %v\n", err)
		return 2
	}
	assessment, err := capacityplan.BuildCampaignCollectionAssessment(evidence, goal)
	if err != nil {
		fmt.Fprintf(stderr, "cannot assess campaign collection: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, "--- DAMOCLINE | CAMPAIGN LEVEL AUDIT (OFFLINE) ---")
	fmt.Fprintf(stdout, "Level collection verdict: %s\n", assessment.State)
	if assessment.State == "NOT_COMPARABLE" {
		fmt.Fprintln(stdout, "Scenario, target and workload match: NOT ESTABLISHED")
	} else {
		fmt.Fprintf(stdout, "Scenario: %s | target: %s\n", assessment.Scenario, assessment.Target)
		fmt.Fprintf(stdout, "Workload fingerprint: %s\n", assessment.WorkloadFingerprint)
	}
	fmt.Fprintf(stdout, "Observed passing levels: %v | failing levels: %v | inconclusive levels: %v\n", assessment.PassingLevels, assessment.FailingLevels, assessment.InconclusiveLevels)
	fmt.Fprintf(stdout, "Observed pattern: %s\n", assessment.ObservationPattern)
	for _, level := range assessment.Levels {
		audit := level.Assessment.Audit
		fmt.Fprintf(stdout, "  %d VUs: %s | audit %s | measurement %s | evidence %s\n", level.TestedUsers, level.Outcome, audit.State, audit.MeasurementQuality, audit.EvidenceID)
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
	fmt.Fprintln(stdout, "No traffic was generated; every level remains linked to its saved campaign evidence.")
	return 0
}

func writeWorkflowBreaches(w io.Writer, breaches []capacityplan.WorkflowBreach, indent string) {
	for _, breach := range breaches {
		fmt.Fprintf(w, "%sTrial %d %s at %d VUs, %s: %s; p95 %.2f/%.2f ms; errors %d/%d (%.2f%%/%.2f%%).\n",
			indent, breach.Trial, breach.Phase, breach.TestedUsers, breach.Workflow,
			strings.Join(breach.Reasons, ", "), breach.P95MS, breach.P95LimitMS,
			breach.Failures, breach.Total, breach.ErrorRate*100, breach.MaxErrorRate*100)
	}
}
