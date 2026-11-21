package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/crosscheck"
	"github.com/LucasLuchetta/Damocline/internal/externalcompare"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const crosscheckUsage = "usage: damocline crosscheck --run report.json --access-log FILE --format nginx|combined|json [--path-map /log-prefix=/scenario-prefix]... [--clock-tolerance 2s] [--count-tolerance 0.01] [--count-abs-tolerance 2] [--out crosscheck.json] [--report-out crosscheck.md]"

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func readRunReport(path string) (report.Result, error) {
	var r report.Result
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("decode run report: %w", err)
	}
	if r.StartedAt == "" || r.SchemaVersion == 0 {
		return r, fmt.Errorf("not a Damocline run report (missing schema_version or started_at)")
	}
	return r, nil
}

func writeOutputs(stderr io.Writer, out, reportOut string, v any, markdown string) bool {
	if out != "" {
		if _, err := os.Stat(out); err == nil {
			fmt.Fprintf(stderr, "output JSON already exists: %s\n", out)
			return false
		}
		if err := report.WriteAnyJSON(out, v); err != nil {
			fmt.Fprintf(stderr, "cannot write JSON: %v\n", err)
			return false
		}
	}
	if reportOut != "" {
		if err := writeCapacityMarkdown(reportOut, markdown); err != nil {
			fmt.Fprintf(stderr, "cannot write Markdown: %v\n", err)
			return false
		}
	}
	return true
}

// crosscheckCmd compares a run report with a server access log offline.
func crosscheckCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crosscheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	def := crosscheck.DefaultOptions()
	run := fs.String("run", "", "Damocline run report JSON")
	logPath := fs.String("access-log", "", "server or reverse-proxy access log")
	format := fs.String("format", "", "access log format: nginx, combined or json")
	var maps multiFlag
	fs.Var(&maps, "path-map", "rewrite a log path prefix to a scenario prefix, /from=/to (repeatable)")
	fs.DurationVar(&def.ClockTolerance, "clock-tolerance", def.ClockTolerance, "widen the run window on both sides")
	fs.Float64Var(&def.CountRelTolerance, "count-tolerance", def.CountRelTolerance, "relative count tolerance")
	fs.IntVar(&def.CountAbsTolerance, "count-abs-tolerance", def.CountAbsTolerance, "absolute count tolerance")
	out := fs.String("out", "", "optional JSON output path (must not exist)")
	reportOut := fs.String("report-out", "", "optional Markdown output path (must not exist)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *run == "" || *logPath == "" || *format == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, crosscheckUsage)
		return 2
	}
	for _, m := range maps {
		pm, err := crosscheck.ParsePathMap(m)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		def.PathMaps = append(def.PathMaps, pm)
	}
	r, err := readRunReport(*run)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read --run: %v\n", err)
		return 2
	}
	f, err := os.Open(*logPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot open --access-log: %v\n", err)
		return 2
	}
	parsed, err := crosscheck.ParseLog(f, *format)
	f.Close()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	res, err := crosscheck.Compare(r, parsed, *format, def)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	md := crosscheck.Markdown(res)
	if !writeOutputs(stderr, *out, *reportOut, res, md) {
		return 2
	}
	fmt.Fprint(stdout, md)
	// Only independently corroborated MATCH results may exit successfully.
	// Treat missing/ambiguous evidence as non-approval, like a mismatch.
	if res.Overall != crosscheck.Match {
		return 1
	}
	return 0
}

const compareExternalUsage = "usage: damocline compare-external --damocline report.json --k6-summary k6.json [--out comparison.json] [--report-out comparison.md]"

// compareExternalCmd compares a run report with a k6 summary offline.
func compareExternalCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare-external", flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts := externalcompare.DefaultOptions()
	dam := fs.String("damocline", "", "Damocline run report JSON")
	k6 := fs.String("k6-summary", "", "k6 --summary-export or handleSummary JSON")
	fs.Float64Var(&opts.ThroughputRelTolerance, "throughput-tolerance", opts.ThroughputRelTolerance, "relative throughput tolerance")
	fs.Float64Var(&opts.ErrorRateAbsTolerance, "error-rate-tolerance", opts.ErrorRateAbsTolerance, "absolute error-rate tolerance")
	fs.Float64Var(&opts.P95RelTolerance, "p95-tolerance", opts.P95RelTolerance, "relative p95 tolerance")
	out := fs.String("out", "", "optional JSON output path (must not exist)")
	reportOut := fs.String("report-out", "", "optional Markdown output path (must not exist)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dam == "" || *k6 == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, compareExternalUsage)
		return 2
	}
	r, err := readRunReport(*dam)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read --damocline: %v\n", err)
		return 2
	}
	data, err := os.ReadFile(*k6)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read --k6-summary: %v\n", err)
		return 2
	}
	summary, err := externalcompare.ParseK6Summary(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	res := externalcompare.Compare(r, summary, opts)
	md := externalcompare.Markdown(res)
	if !writeOutputs(stderr, *out, *reportOut, res, md) {
		return 2
	}
	fmt.Fprint(stdout, md)
	if res.Overall == externalcompare.RedFlag {
		return 1
	}
	return 0
}
