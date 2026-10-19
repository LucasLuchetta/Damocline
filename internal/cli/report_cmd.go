package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// reportCmd explains saved measurements locally, with no network requests.
func reportCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "history", "save", "delete", "prune":
			return reportHistoryCmd(args[0], args[1:], stdout, stderr)
		case "export":
			return reportExportCmd(args[1:], stdout, stderr)
		}
	}
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "Damocline HTTP run report JSON")
	compare := fs.String("compare", "", "optional previous HTTP run for comparison")
	hostPath := fs.String("host", "", "optional Linux host monitor report JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline report --file run.json [--compare previous.json] [--host host.json]")
		return 2
	}
	current, err := insights.ReadRun(*path)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read run: %v\n", err)
		return 2
	}
	var previous *report.Result
	if *compare != "" {
		v, e := insights.ReadRun(*compare)
		if e != nil {
			fmt.Fprintf(stderr, "cannot read previous run: %v\n", e)
			return 2
		}
		previous = &v
	}
	var host *hostmon.Result
	if *hostPath != "" {
		v, e := insights.ReadHost(*hostPath)
		if e != nil {
			fmt.Fprintf(stderr, "cannot read host metrics: %v\n", e)
			return 2
		}
		host = &v
	}
	fmt.Fprintf(stdout, "Damocline report: %s | %d configured VU(s) | %.0f seconds\n", current.Name, current.Config.Users, current.ElapsedSeconds)
	insights.Write(stdout, current, host, previous)
	return 0
}
