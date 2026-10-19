package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditCmd replays a saved campaign without sending requests or consulting a
// live target. Its observed stage verdict is separate from capacity approval.
func auditCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("campaign", "", "saved campaign JSON path")
	out := fs.String("out", "", "optional combined assessment JSON output path")
	peak := fs.Int("peak-users", 0, "optional expected simultaneous active workload-equivalent users")
	growth := fs.Float64("growth-percent", 0, "optional estimated growth of active demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "optional additional safety reserve (percent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline audit --campaign campaign.json [--peak-users 20] [--growth-percent 25] [--reserve-percent 20] [--out assessment.json]")
		return 2
	}
	demandRequested := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "peak-users" || f.Name == "growth-percent" || f.Name == "reserve-percent" {
			demandRequested = true
		}
	})
	evidence, err := capacityplan.ReadCampaign(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign: %v\n", err)
		return 2
	}
	var goal *capacitygoal.Goal
	if demandRequested {
		if *peak < 1 {
			fmt.Fprintln(stderr, "--peak-users must be positive when demand options are used")
			return 2
		}
		goal = &capacitygoal.Goal{PeakUsers: *peak, GrowthPercent: *growth, ReservePercent: *reserve}
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
