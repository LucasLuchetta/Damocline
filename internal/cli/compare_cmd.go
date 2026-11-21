package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/comparison"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const compareUsage = "usage: damocline compare --before DIR|campaign.json --after DIR|campaign.json [--out comparison.json] [--report-out comparison.md]"

// compareCmd compares two saved capacity campaigns offline.
func compareCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	before := fs.String("before", "", "capacity output directory or campaign JSON measured before the change")
	after := fs.String("after", "", "capacity output directory or campaign JSON measured after the change")
	out := fs.String("out", "", "optional comparison JSON output path (must not exist)")
	reportOut := fs.String("report-out", "", "optional comparison Markdown output path (must not exist)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *before == "" || *after == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, compareUsage)
		return 2
	}
	b, bLabel, err := readComparisonCampaign(*before)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read --before: %v\n", err)
		return 2
	}
	a, aLabel, err := readComparisonCampaign(*after)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read --after: %v\n", err)
		return 2
	}
	result := comparison.Compare(b, a, bLabel, aLabel)
	markdown := comparison.Markdown(result)
	if *out != "" {
		if _, err := os.Stat(*out); err == nil {
			fmt.Fprintf(stderr, "comparison JSON already exists: %s\n", *out)
			return 2
		}
		if err := report.WriteAnyJSON(*out, result); err != nil {
			fmt.Fprintf(stderr, "cannot write comparison JSON: %v\n", err)
			return 2
		}
	}
	if *reportOut != "" {
		if err := writeCapacityMarkdown(*reportOut, markdown); err != nil {
			fmt.Fprintf(stderr, "cannot write comparison report: %v\n", err)
			return 2
		}
	}
	fmt.Fprint(stdout, markdown)
	if result.Comparability != comparison.Comparable {
		fmt.Fprintln(stderr, "campaigns are not comparable: scenario, load, envelope or pass criteria differ")
		return 1
	}
	return 0
}

// readComparisonCampaign accepts a capacity output directory (reading its
// campaign.json) or a campaign JSON file. The label is the base name only, so
// reports do not embed local directory paths.
func readComparisonCampaign(path string) (campaign.Result, string, error) {
	label := filepath.Base(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "campaign.json")
		label += "/campaign.json"
	}
	result, err := capacityplan.ReadCampaign(path)
	return result, label, err
}
