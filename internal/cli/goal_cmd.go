package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
)

const maxGoalReportBytes = 16 << 20

// goalCmd compares the owner's planned demand with a saved, already-authorized
// exploration. It is entirely offline and never initiates HTTP traffic.
func goalCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "path to an existing Damocline explore JSON report")
	peak := fs.Int("peak-users", 0, "expected simultaneous active workload-equivalent users")
	growth := fs.Float64("growth-percent", 0, "estimated growth of active demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "additional safety reserve on projected demand (percent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *peak < 1 || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline goal --file capacity.json --peak-users 20 [--growth-percent 25] [--reserve-percent 20]")
		return 2
	}
	file, err := os.Open(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot open capacity report: %v\n", err)
		return 2
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGoalReportBytes+1))
	if err != nil {
		fmt.Fprintf(stderr, "cannot read capacity report: %v\n", err)
		return 2
	}
	if len(data)>maxGoalReportBytes {
		fmt.Fprintln(stderr, "capacity report exceeds the 16 MiB safety limit")
		return 2
	}
	var historical explorer.Result
	if err := json.Unmarshal(data, &historical); err != nil {
		fmt.Fprintf(stderr, "invalid capacity report JSON: %v\n", err)
		return 2
	}
	a, err := capacitygoal.Evaluate(historical, capacitygoal.Goal{
		PeakUsers: *peak, GrowthPercent: *growth, ReservePercent: *reserve,
	})
	if err != nil {
		fmt.Fprintf(stderr, "cannot assess capacity goal: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, "\n--- DAMOCLINE | CAPACITY GOAL (OFFLINE) ---")
	fmt.Fprintf(stdout, "Expected peak: %d synthetic workload-equivalent active actors\n", a.PeakUsers)
	fmt.Fprintf(stdout, "Planned growth: %.1f%% | Reserve: %.1f%%\n", a.GrowthPercent, a.ReservePercent)
	fmt.Fprintf(stdout, "DEMAND TO VALIDATE: %d concurrently active workload-equivalent actors\n", a.TargetUsers)
	fmt.Fprintf(stdout, "EVIDENCE STATUS: %s\n", a.State)
	if a.ObservedPassingUsers != nil {
		fmt.Fprintf(stdout, "Highest passing level REPORTED by explorer: %d VUs (not a certified server limit)\n", *a.ObservedPassingUsers)
	} else {
		fmt.Fprintln(stdout, "Highest passing level: NOT ESTABLISHED")
	}
	if a.FirstObservedFailingUsers != nil {
		fmt.Fprintf(stdout, "First failing level REPORTED by explorer: %d VUs (cause not established)\n", *a.FirstObservedFailingUsers)
	}
	if a.MatchedProbeUsers != nil {
		fmt.Fprintf(stdout, "Exact goal load: %d VUs; %d recorded trial(s)\n", *a.MatchedProbeUsers, a.MatchedTrials)
	}
	fmt.Fprintln(stdout, "\nEvidence and limitations:")
	for _, finding := range a.Findings {
		fmt.Fprintf(stdout, "  - %s\n", finding)
	}
	fmt.Fprintln(stdout, "\nWhat to do next:")
	for i, next := range a.Next {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, next)
	}
	fmt.Fprintln(stdout, "Decision: an infrastructure upgrade is NOT justified by this report alone.")
	fmt.Fprintln(stdout, "No load was generated. This assessment uses only the saved local JSON.")
	return 0
}
