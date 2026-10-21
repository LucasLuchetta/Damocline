package cli

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const maxGoalReportBytes = 16 << 20

// goalCmd compares the owner's planned demand with a saved, already-authorized
// exploration. It is entirely offline and never initiates HTTP traffic.
func goalCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "path to an existing explore or explore-arrival JSON report")
	peakUsers := fs.Int("peak-users", 0, "expected simultaneous active workload-equivalent VUs")
	peakStarts := fs.Int("peak-journeys-per-second", 0, "expected scheduled open-loop journey starts/s")
	growth := fs.Float64("growth-percent", 0, "estimated growth of demand (percent)")
	reserve := fs.Float64("reserve-percent", 0, "additional safety reserve on projected demand (percent)")
	out := fs.String("out", "", "optional offline assessment JSON output path")
	reportOut := fs.String("report-out", "", "optional executive and technical Markdown report path")
	expectedSourceSHA256 := fs.String("expect-source-sha256", "", "require the exact source file SHA-256 as sha256:<64 lowercase hexadecimal characters>")
	expectedAuditorSHA256 := fs.String("expect-auditor-sha256", "", "require the exact auditor executable SHA-256 as sha256:<64 lowercase hexadecimal characters>")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *peakUsers < 0 || *peakStarts < 0 || (*peakUsers == 0) == (*peakStarts == 0) || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: damocline goal --file capacity.json (--peak-users 20 | --peak-journeys-per-second 5) [--growth-percent 25] [--reserve-percent 20] [--expect-source-sha256 sha256:<64-hex>] [--expect-auditor-sha256 sha256:<64-hex>] [--out assessment.json] [--report-out capacity.md]")
		return 2
	}
	var pinnedAuditor capacityplan.AuditorIdentity
	if *expectedAuditorSHA256 != "" {
		if !validAuditorSHA256(*expectedAuditorSHA256) {
			fmt.Fprintln(stderr, "invalid expected auditor SHA-256: use sha256:<64 lowercase hexadecimal characters>")
			return 2
		}
		var err error
		pinnedAuditor, err = currentAuditorIdentity()
		if err != nil {
			fmt.Fprintf(stderr, "cannot identify audit executable: %v\n", err)
			return 2
		}
		if pinnedAuditor.ExecutableSHA256 != *expectedAuditorSHA256 {
			fmt.Fprintf(stderr, "auditor executable SHA-256 is %s; expected %s\n", pinnedAuditor.ExecutableSHA256, *expectedAuditorSHA256)
			return 2
		}
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
	if len(data) > maxGoalReportBytes {
		fmt.Fprintln(stderr, "capacity report exceeds the 16 MiB safety limit")
		return 2
	}
	if err := jsonartifact.RejectDuplicateKeys(data); err != nil {
		fmt.Fprintf(stderr, "capacity report contains ambiguous JSON: %v\n", err)
		return 2
	}
	if *expectedSourceSHA256 != "" {
		if err := validateExpectedEvidenceIDCount([]string{*expectedSourceSHA256}, 1); err != nil {
			fmt.Fprintf(stderr, "invalid expected source SHA-256: %v\n", err)
			return 2
		}
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		if digest != *expectedSourceSHA256 {
			fmt.Fprintf(stderr, "source SHA-256 check failed: got %s; expected %s\n", digest, *expectedSourceSHA256)
			return 2
		}
	}
	var shape struct {
		JourneyWorkerLimit     *int            `json:"journey_worker_limit"`
		ArrivalCampaignVersion *int            `json:"arrival_campaign_schema_version"`
		Probes                 json.RawMessage `json:"probes"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		fmt.Fprintf(stderr, "invalid capacity report JSON: %v\n", err)
		return 2
	}
	if *expectedAuditorSHA256 != "" && shape.ArrivalCampaignVersion == nil {
		fmt.Fprintln(stderr, "--expect-auditor-sha256 is supported only for open-arrival campaign assessments")
		return 2
	}

	fmt.Fprintln(stdout, "\n--- DAMOCLINE | CAPACITY GOAL (OFFLINE) ---")
	if shape.ArrivalCampaignVersion != nil {
		if *peakUsers != 0 {
			fmt.Fprintln(stderr, "arrival campaign reports require --peak-journeys-per-second; VUs cannot be substituted")
			return 2
		}
		if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
			fmt.Fprintf(stderr, "arrival campaign has incomplete metric evidence: %v\n", err)
			return 2
		}
		var saved arrivalcampaign.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			fmt.Fprintf(stderr, "invalid arrival campaign report JSON: %v\n", err)
			return 2
		}
		assessment, err := capacitygoal.EvaluateJourneyCampaign(saved, capacitygoal.JourneyStartGoal{PeakStartsPerSecond: *peakStarts, GrowthPercent: *growth, ReservePercent: *reserve})
		if err != nil {
			fmt.Fprintf(stderr, "cannot assess arrival campaign: %v\n", err)
			return 2
		}
		sourceDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		assessment.SourceEvidenceID = sourceDigest
		auditor := pinnedAuditor
		if auditor.ExecutableSHA256 == "" {
			auditor, err = currentAuditorIdentity()
			if err != nil {
				fmt.Fprintf(stderr, "cannot identify audit executable: %v\n", err)
				return 2
			}
		}
		assessment.AuditorVersion = auditor.Version
		assessment.AuditorExecutableSHA256 = auditor.ExecutableSHA256
		fmt.Fprintf(stdout, "Scenario identity consistency: %s\n", assessment.ScenarioIdentityStatus)
		fmt.Fprintf(stdout, "Expected peak: %d scheduled journey starts/s\n", assessment.PeakStartsPerSecond)
		fmt.Fprintf(stdout, "Planned growth: %.1f%% | Reserve: %.1f%%\n", assessment.GrowthPercent, assessment.ReservePercent)
		fmt.Fprintf(stdout, "DEMAND TO VALIDATE: %d scheduled journey starts/s\n", assessment.TargetStartsPerSecond)
		fmt.Fprintln(stdout, "Units: scheduled journey starts/s; VUs, admitted journeys, completed journeys, and HTTP requests/s are separate.")
		fmt.Fprintf(stdout, "EVIDENCE STATUS: %s\n", assessment.State)
		if *out != "" && saved.SchemaVersion >= 8 {
			if err := writeArrivalCampaignAssessmentJSON(*out, assessment); err != nil {
				fmt.Fprintf(stderr, "cannot write assessment: %v\n", err)
				return 2
			}
			fmt.Fprintf(stdout, "Assessment JSON: %s\n", *out)
		}
		if *reportOut != "" && saved.SchemaVersion >= 8 {
			if err := writeCapacityMarkdown(*reportOut, renderArrivalCampaignCapacityReport(assessment, *path, saved, sourceDigest)); err != nil {
				fmt.Fprintf(stderr, "cannot write capacity report: %v\n", err)
				return 2
			}
			fmt.Fprintf(stdout, "Executive and technical report: %s\n", *reportOut)
		}
		if saved.Objectives != nil {
			o := saved.Objectives
			fmt.Fprintf(stdout, "Predeclared objectives: %s scope; journey p95 <= %.3fms; error rate <= %.5f%%; samples/step >= %d; complete work %.0f%%; dropped arrivals <= %d; windows >= %d with >= %d journeys/window; window %s\n",
				o.JourneyScope, o.JourneyP95LimitMS, o.MaxJourneyErrorRate*100, o.MinSamplesPerStep, o.MinCompletedWorkRatio*100,
				o.MaxDroppedArrivals, o.MinimumObservationWindows, o.MinimumSamplesPerWindow, o.WindowDuration)
		}
		if saved.StabilityPolicy != nil {
			p := saved.StabilityPolicy
			fmt.Fprintf(stdout, "Stability screen: minimum %d windows, %d samples/window; max p95 rise %.3f%% and %.3fms; max throughput drop %.3f%% (coarse screen, not a stationarity test)\n",
				p.MinimumWindows, p.MinimumSamplesPerWindow, p.MaxP95IncreasePercent, p.MaxP95IncreaseMS, p.MaxThroughputDropPercent)
		}
		if assessment.MatchedStartsPerSecond != nil {
			fmt.Fprintf(stdout, "Exact campaign target: %d scheduled starts/s; %d passing trial(s)\n", *assessment.MatchedStartsPerSecond, assessment.MatchedTrials)
		}
		printGoalNextSteps(stdout, assessment.Findings, assessment.Next)
	} else if shape.JourneyWorkerLimit != nil {
		if *peakUsers != 0 {
			fmt.Fprintln(stderr, "arrival exploration reports require --peak-journeys-per-second; VUs cannot be substituted")
			return 2
		}
		if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
			fmt.Fprintf(stderr, "arrival exploration has incomplete metric evidence: %v\n", err)
			return 2
		}
		if err := arrivalexplorer.ValidateArtifactJSON(data); err != nil {
			fmt.Fprintf(stderr, "arrival exploration has incomplete evidence: %v\n", err)
			return 2
		}
		var saved arrivalexplorer.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			fmt.Fprintf(stderr, "invalid arrival capacity report JSON: %v\n", err)
			return 2
		}
		assessment, err := capacitygoal.EvaluateJourneyStarts(saved, capacitygoal.JourneyStartGoal{
			PeakStartsPerSecond: *peakStarts, GrowthPercent: *growth, ReservePercent: *reserve,
		})
		if err != nil {
			fmt.Fprintf(stderr, "cannot assess arrival capacity goal: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Expected peak: %d scheduled journey starts/s\n", assessment.PeakStartsPerSecond)
		fmt.Fprintf(stdout, "Planned growth: %.1f%% | Reserve: %.1f%%\n", assessment.GrowthPercent, assessment.ReservePercent)
		fmt.Fprintf(stdout, "DEMAND TO VALIDATE: %d scheduled journey starts/s\n", assessment.TargetStartsPerSecond)
		fmt.Fprintln(stdout, "Units: scheduled journey starts/s; VUs, admitted journeys, completed journeys, and HTTP requests/s are separate.")
		fmt.Fprintf(stdout, "EVIDENCE STATUS: %s\n", assessment.State)
		if assessment.ObservedPassingStartsPerSec != nil {
			fmt.Fprintf(stdout, "Highest passing rate REPORTED by explorer: %d scheduled starts/s (not a certified capacity limit)\n", *assessment.ObservedPassingStartsPerSec)
		}
		if assessment.FirstObservedFailingStartsSec != nil {
			fmt.Fprintf(stdout, "First failing rate REPORTED by explorer: %d scheduled starts/s (cause not established)\n", *assessment.FirstObservedFailingStartsSec)
		}
		if assessment.MatchedStartsPerSecond != nil {
			fmt.Fprintf(stdout, "Exact goal rate: %d scheduled starts/s; %d recorded trial(s)\n", *assessment.MatchedStartsPerSecond, assessment.MatchedTrials)
		}
		printGoalNextSteps(stdout, assessment.Findings, assessment.Next)
	} else {
		if *peakStarts != 0 {
			fmt.Fprintln(stderr, "closed-loop exploration reports require --peak-users; journey starts/s cannot be substituted")
			return 2
		}
		if len(shape.Probes) > 0 && string(shape.Probes) != "null" {
			if err := report.ValidateMetricCountersInArtifactJSON(data); err != nil {
				fmt.Fprintf(stderr, "closed-loop exploration has incomplete metric evidence: %v\n", err)
				return 2
			}
		}
		if err := explorer.ValidateArtifactJSON(data); err != nil {
			fmt.Fprintf(stderr, "closed-loop exploration has incomplete evidence: %v\n", err)
			return 2
		}
		var saved explorer.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			fmt.Fprintf(stderr, "invalid capacity report JSON: %v\n", err)
			return 2
		}
		assessment, err := capacitygoal.Evaluate(saved, capacitygoal.Goal{
			PeakUsers: *peakUsers, GrowthPercent: *growth, ReservePercent: *reserve,
		})
		if err != nil {
			fmt.Fprintf(stderr, "cannot assess capacity goal: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Expected peak: %d synthetic workload-equivalent active actors\n", assessment.PeakUsers)
		fmt.Fprintf(stdout, "Planned growth: %.1f%% | Reserve: %.1f%%\n", assessment.GrowthPercent, assessment.ReservePercent)
		fmt.Fprintf(stdout, "DEMAND TO VALIDATE: %d concurrently active workload-equivalent actors\n", assessment.TargetUsers)
		fmt.Fprintf(stdout, "EVIDENCE STATUS: %s\n", assessment.State)
		if assessment.ObservedPassingUsers != nil {
			fmt.Fprintf(stdout, "Highest passing level REPORTED by explorer: %d VUs (not a certified server limit)\n", *assessment.ObservedPassingUsers)
		} else {
			fmt.Fprintln(stdout, "Highest passing level: NOT ESTABLISHED")
		}
		if assessment.FirstObservedFailingUsers != nil {
			fmt.Fprintf(stdout, "First failing level REPORTED by explorer: %d VUs (cause not established)\n", *assessment.FirstObservedFailingUsers)
		}
		if assessment.MatchedProbeUsers != nil {
			fmt.Fprintf(stdout, "Exact goal load: %d VUs; %d recorded trial(s)\n", *assessment.MatchedProbeUsers, assessment.MatchedTrials)
		}
		printGoalNextSteps(stdout, assessment.Findings, assessment.Next)
	}
	fmt.Fprintln(stdout, "Decision: an infrastructure upgrade is NOT justified by this report alone.")
	fmt.Fprintln(stdout, "No load was generated. This assessment uses only the saved local JSON.")
	return 0
}

func printGoalNextSteps(w io.Writer, findings, next []string) {
	fmt.Fprintln(w, "\nEvidence and limitations:")
	for _, finding := range findings {
		fmt.Fprintf(w, "  - %s\n", finding)
	}
	fmt.Fprintln(w, "\nWhat to do next:")
	for i, item := range next {
		fmt.Fprintf(w, "  %d. %s\n", i+1, item)
	}
}
