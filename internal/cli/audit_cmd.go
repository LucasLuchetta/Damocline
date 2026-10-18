package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditCmd replays a saved campaign without sending requests or consulting a
// live target. Its observed stage verdict is separate from capacity approval.
func auditCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("campaign", "", "saved campaign JSON path")
	out := fs.String("out", "", "optional audit-only JSON output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline audit --campaign campaign.json [--out audit.json]")
		return 2
	}
	evidence, err := capacityplan.ReadCampaign(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign: %v\n", err)
		return 2
	}
	audit := capacityplan.AuditCampaign(evidence)
	fmt.Fprintln(stdout, "--- DAMOCLINE | CAMPAIGN EVIDENCE AUDIT (OFFLINE) ---")
	fmt.Fprintf(stdout, "Observed campaign outcome: %s\n", audit.ObservedOutcome)
	fmt.Fprintf(stdout, "Audited evidence verdict: %s\n", audit.State)
	fmt.Fprintf(stdout, "Recomputed measurement quality: %s\n", audit.MeasurementQuality)
	fmt.Fprintf(stdout, "Required phases reviewed: %d\n", audit.RequiredPhases)
	for _, phase := range audit.Phases {
		fmt.Fprintf(stdout, "  Trial %d %s: %s | measurement %s", phase.Trial, phase.Name, phase.EvidenceState, phase.Measurement)
		if phase.Stability != "" {
			fmt.Fprintf(stdout, " | drift screen %s", phase.Stability)
		}
		fmt.Fprintln(stdout)
	}
	for _, finding := range audit.Findings {
		fmt.Fprintf(stdout, "  - %s\n", finding)
	}
	if *out != "" {
		if err := report.WriteAnyJSON(*out, audit); err != nil {
			fmt.Fprintf(stderr, "cannot write audit: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Audit JSON: %s\n", *out)
	}
	fmt.Fprintln(stdout, "No traffic was generated; run 'damocline plan' separately to assess a specific demand target.")
	return 0
}
