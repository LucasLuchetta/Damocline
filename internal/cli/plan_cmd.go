package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
)

// planCmd assesses a saved campaign against an owner-supplied demand goal. It
// is offline and cannot initiate HTTP traffic or change infrastructure.
func planCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("campaign", "", "path to an existing Damocline campaign JSON report")
	peak := fs.Int("peak-users", 0, "expected simultaneous active workload-equivalent users")
	growth := fs.Float64("growth-percent", 0, "estimated growth of active demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "additional safety reserve on projected demand (percent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *peak < 1 || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline plan --campaign campaign.json --peak-users 20 [--growth-percent 25] [--reserve-percent 20]")
		return 2
	}
	evidence, err := capacityplan.ReadCampaign(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read campaign report: %v\n", err)
		return 2
	}
	result, err := capacityplan.Evaluate(evidence, capacitygoal.Goal{
		PeakUsers: *peak, GrowthPercent: *growth, ReservePercent: *reserve,
	})
	if err != nil {
		fmt.Fprintf(stderr, "cannot calculate business demand: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, "\n--- DAMOCLINE | CAPACITY PLAN (OFFLINE) ---")
	fmt.Fprintf(stdout, "Demand to validate: %d synthetic workload-equivalent active actors\n", result.TargetUsers)
	fmt.Fprintf(stdout, "Repeated campaign base level: %d VUs\n", result.TestedUsers)
	fmt.Fprintf(stdout, "EVIDENCE STATUS: %s\n", result.State)
	fmt.Fprintln(stdout, "\nEvidence and limitations:")
	for _, finding := range result.Findings {
		fmt.Fprintf(stdout, "  - %s\n", finding)
	}
	fmt.Fprintln(stdout, "No load was generated. This assessment does not diagnose a bottleneck or recommend an infrastructure purchase.")
	return 0
}
