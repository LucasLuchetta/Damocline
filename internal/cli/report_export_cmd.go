package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/reportexport"
)

const maxExportInputBytes = 16 << 20

func reportExportCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("file", "", "run or campaign JSON artifact to export")
	output := fs.String("out", "", "destination for redacted evidence JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 || *input == "" || *output == "" {
		fmt.Fprintln(stderr, "usage: damocline report export --file artifact.json --out redacted.json")
		return 2
	}
	if samePath(*input, *output) {
		fmt.Fprintln(stderr, "input and output paths must be different")
		return 2
	}
	data, err := readExportInput(*input)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read source artifact: %v\n", err)
		return 2
	}
	if err := jsonartifact.RejectDuplicateKeys(data); err != nil {
		fmt.Fprintf(stderr, "source artifact contains ambiguous JSON: %v\n", err)
		return 2
	}
	if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
		fmt.Fprintf(stderr, "source artifact has incomplete metric evidence: %v\n", err)
		return 2
	}
	var shape struct {
		SchemaVersion      *int            `json:"schema_version"`
		ArrivalCampaign    *int            `json:"arrival_campaign_schema_version"`
		BaseUsers          *int            `json:"base_users"`
		JourneyWorkerLimit *int            `json:"journey_worker_limit"`
		Probes             json.RawMessage `json:"probes"`
		Metrics            json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		fmt.Fprintf(stderr, "invalid source artifact JSON: %v\n", err)
		return 2
	}
	var evidence reportexport.Evidence
	switch {
	case shape.ArrivalCampaign != nil:
		var saved arrivalcampaign.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			fmt.Fprintf(stderr, "invalid arrival campaign JSON: %v\n", err)
			return 2
		}
		peak, growth, reserve := saved.PeakStartsPerSecond, saved.GrowthPercent, saved.ReservePercent
		if peak == 0 {
			peak = saved.BaseStartsPerSecond
			growth, reserve = 0, 0
		}
		if _, err := capacitygoal.EvaluateJourneyCampaign(saved, capacitygoal.JourneyStartGoal{PeakStartsPerSecond: peak, GrowthPercent: growth, ReservePercent: reserve}); err != nil {
			fmt.Fprintf(stderr, "arrival campaign is invalid: %v\n", err)
			return 2
		}
		evidence = reportexport.FromArrivalCampaign(saved)
	case shape.BaseUsers != nil:
		if err := capacityplan.ValidateCampaignArtifact(data); err != nil {
			fmt.Fprintf(stderr, "closed-loop campaign has incomplete evidence: %v\n", err)
			return 2
		}
		var saved campaign.Result
		if json.Unmarshal(data, &saved) != nil || capacityplan.ValidateSavedCampaign(saved) != nil {
			fmt.Fprintln(stderr, "closed-loop campaign is invalid or unsupported")
			return 2
		}
		evidence = reportexport.FromClosedCampaign(saved)
	case shape.SchemaVersion != nil && len(shape.Probes) > 0 && string(shape.Probes) != "null":
		if shape.JourneyWorkerLimit != nil {
			if err := arrivalexplorer.ValidateArtifactJSON(data); err != nil {
				fmt.Fprintf(stderr, "arrival exploration has incomplete evidence: %v\n", err)
				return 2
			}
			var saved arrivalexplorer.Result
			if json.Unmarshal(data, &saved) != nil {
				fmt.Fprintln(stderr, "arrival exploration is invalid or unsupported")
				return 2
			}
			peak := saved.TargetJourneysPerSecond
			if peak == 0 {
				peak = saved.MinJourneysPerSecond
			}
			if _, err := capacitygoal.EvaluateJourneyStarts(saved, capacitygoal.JourneyStartGoal{PeakStartsPerSecond: peak}); err != nil {
				fmt.Fprintf(stderr, "arrival exploration is invalid: %v\n", err)
				return 2
			}
			evidence = reportexport.FromArrivalExplorer(saved)
		} else {
			if err := explorer.ValidateArtifactJSON(data); err != nil {
				fmt.Fprintf(stderr, "closed-loop exploration has incomplete evidence: %v\n", err)
				return 2
			}
			var saved explorer.Result
			if json.Unmarshal(data, &saved) != nil {
				fmt.Fprintln(stderr, "closed-loop exploration is invalid or unsupported")
				return 2
			}
			if err := validateClosedExplorer(saved); err != nil {
				fmt.Fprintf(stderr, "closed-loop exploration is invalid: %v\n", err)
				return 2
			}
			evidence = reportexport.FromClosedExplorer(saved)
		}
	case shape.SchemaVersion != nil && len(shape.Metrics) > 0 && string(shape.Metrics) != "null":
		var saved report.Result
		if err := json.Unmarshal(data, &saved); err != nil || insights.ValidateRunReport(saved) != nil {
			fmt.Fprintln(stderr, "run artifact is invalid or unsupported")
			return 2
		}
		evidence = reportexport.FromRun(saved)
	default:
		fmt.Fprintln(stderr, "source is not a supported run or campaign artifact")
		return 2
	}
	if err := report.WriteAnyJSON(*output, evidence); err != nil {
		fmt.Fprintf(stderr, "cannot write redacted export: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Wrote redacted %s evidence (%s). This descriptive export is not a native assessment artifact.\n", evidence.Kind, reportexport.Format)
	return 0
}

func validateClosedExplorer(saved explorer.Result) error {
	if _, err := capacitygoal.Evaluate(saved, capacitygoal.Goal{PeakUsers: saved.MinUsers}); err != nil {
		return err
	}
	if len(saved.Probes) > 50 {
		return fmt.Errorf("closed-loop exploration exceeds the 50-level bound")
	}
	seen := make(map[int]bool, len(saved.Probes))
	stage := safeParseDuration(saved.StageDuration)
	for _, probe := range saved.Probes {
		if probe.Users < saved.MinUsers || probe.Users > saved.MaxUsers || seen[probe.Users] ||
			len(probe.Runs) > saved.Repeats || !validProbeStatus(probe.Status) {
			return fmt.Errorf("closed-loop exploration contains an invalid probe")
		}
		seen[probe.Users] = true
		for _, run := range probe.Runs {
			if err := insights.ValidateRunReport(run); err != nil {
				return fmt.Errorf("closed-loop exploration contains an invalid run report")
			}
			if run.Config.Mode != "closed" || run.Config.Users != probe.Users || safeParseDuration(run.Config.Duration) != stage {
				return fmt.Errorf("closed-loop exploration run does not match its declared probe")
			}
		}
	}
	return nil
}

func safeParseDuration(value string) time.Duration {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

func validProbeStatus(value string) bool {
	switch value {
	case "PASS", "FAIL", "INCONCLUSIVE", "ABORTED":
		return true
	default:
		return false
	}
}

func readExportInput(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxExportInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxExportInputBytes {
		return nil, errors.New("artifact exceeds the 16 MiB safety limit")
	}
	return data, nil
}

func samePath(input, output string) bool {
	inPath, inErr := canonicalPath(input)
	outPath, outErr := canonicalPath(output)
	if inErr != nil || outErr != nil {
		return true
	}
	if samePathString(inPath, outPath) {
		return true
	}
	inInfo, inErr := os.Stat(inPath)
	outInfo, outErr := os.Stat(outPath)
	return inErr == nil && outErr == nil && os.SameFile(inInfo, outInfo)
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	parent, name := filepath.Dir(abs), filepath.Base(abs)
	var missingParents []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(parent)
		if resolveErr == nil {
			parts := append([]string{resolved}, missingParents...)
			parts = append(parts, name)
			return filepath.Join(parts...), nil
		}
		next := filepath.Dir(parent)
		if next == parent {
			return abs, nil
		}
		missingParents = append([]string{filepath.Base(parent)}, missingParents...)
		parent = next
	}
}

func samePathString(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
