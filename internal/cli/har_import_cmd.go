package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"

	"github.com/LucasLuchetta/Damocline/internal/harimport"
)

// importHARCmd converts a local HAR capture into a scenario draft (ADR 0178).
// It sends no traffic and never writes credentials, cookies, or bodies.
func importHARCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import-har", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var file, baseURL string
	fs.StringVar(&file, "har", "", "local HAR file")
	fs.StringVar(&file, "file", "", "alias for --har")
	fs.StringVar(&baseURL, "base-url", "", "explicit application origin; other origins are dropped")
	fs.StringVar(&baseURL, "origin", "", "alias for --base-url")
	out := fs.String("out", "", "new scenario draft path (never overwritten)")
	name := fs.String("name", "", "scenario name written into the draft")
	keepStatic := fs.Bool("keep-static", false, "retain static assets such as scripts, styles, images, and fonts")
	includeWrites := fs.Bool("include-writes", false, "retain POST, PUT, PATCH, and DELETE requests; running them still requires --allow-writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || file == "" || *out == "" {
		fmt.Fprintln(stderr, "usage: damocline import-har --har capture.har --base-url https://app.example.com --out scenario.json [--include-writes] [--keep-static]")
		return 2
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.User != nil || base.Hostname() == "" || base.RawQuery != "" || base.Fragment != "" ||
		(base.Scheme != "https" && base.Scheme != "http") || (base.Path != "" && base.Path != "/") {
		fmt.Fprintln(stderr, "import-har requires an explicit HTTP(S) --base-url origin")
		return 2
	}
	data, err := readCapacityFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "cannot read bounded HAR input")
		return 2
	}
	result, err := harimport.Convert(data, base, harimport.Options{Name: *name, KeepStatic: *keepStatic, IncludeWrites: *includeWrites})
	if err != nil {
		writeHARDropped(stdout, result)
		fmt.Fprintln(stderr, err)
		return 2
	}
	encoded, err := json.MarshalIndent(result.Scenario, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "cannot encode scenario draft")
		return 2
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(stderr, "cannot create a new scenario draft; the output path must not exist")
		return 2
	}
	_, err = f.Write(append(encoded, '\n'))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(*out)
		fmt.Fprintln(stderr, "cannot write scenario draft")
		return 2
	}
	steps := len(result.Scenario.Workflows[0].Steps)
	fmt.Fprintf(stdout, "Wrote a draft with %d step(s) to %s. No traffic was sent.\n", steps, *out)
	writeHARDropped(stdout, result)
	if len(result.RemovedQuery) > 0 {
		fmt.Fprintf(stdout, "Removed sensitive query parameters: %v\n", result.RemovedQuery)
	}
	if len(result.Placeholders) > 0 {
		fmt.Fprintln(stdout, "Set these environment variables locally before running; values were not copied from the HAR:")
		for _, name := range result.Placeholders {
			fmt.Fprintf(stdout, "  %s\n", name)
		}
	}
	if len(result.WriteSteps) > 0 {
		fmt.Fprintf(stdout, "%d write step(s) were kept by --include-writes: %v. Running them requires --allow-writes; use disposable data only.\n", len(result.WriteSteps), result.WriteSteps)
	}
	fmt.Fprintln(stdout, "Review paths, expected statuses, users, duration, and pass criteria before running the draft.")
	return 0
}

func writeHARDropped(w io.Writer, result harimport.Result) {
	if len(result.Dropped) == 0 {
		return
	}
	counts := map[string]int{}
	for _, d := range result.Dropped {
		counts[d.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	fmt.Fprintf(w, "Dropped %d request(s):\n", len(result.Dropped))
	for _, reason := range reasons {
		fmt.Fprintf(w, "  %s: %d\n", reason, counts[reason])
	}
	for _, d := range result.Dropped {
		fmt.Fprintf(w, "  - %s %s (%s)\n", d.Method, d.Target, d.Reason)
	}
}
