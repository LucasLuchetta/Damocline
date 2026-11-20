package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/selfcalibration"
)

// selfCalibrationRun is injectable so CLI tests stay fast and deterministic.
var selfCalibrationRun = selfcalibration.Run

// calibrateCmd runs workload self-calibration (ADR 0154). It lives in the
// main executable so the artifact binds to the same executable identity that
// later runs record. It never contacts the scenario target.
func calibrateCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("config", "", "scenario JSON whose workload shape is replayed against a loopback mirror")
	users := fs.String("users", "", "comma-separated closed-loop VU levels (default: the scenario users)")
	rates := fs.String("arrival-rates", "", "comma-separated open-arrival journey-start rates (default: the scenario arrival_rate)")
	levelDuration := fs.Duration("level-duration", selfcalibration.DefaultLevelDuration, "duration of each calibrated level (5s-2m)")
	responseBytes := fs.Int("response-bytes", 0, "declared mirror response body size for every step")
	serverDelay := fs.Duration("server-delay", 0, "declared mirror server delay for every step")
	fromReport := fs.String("from-report", "", "take per-step mirror sizes and median latency from a recorded run report of the real target")
	maxCPU := fs.Float64("max-generator-cpu", selfcalibration.DefaultGuardrails.MaxProcessCPUPercent, "generator process CPU guardrail (percent of detected capacity)")
	maxRSSMB := fs.Uint64("max-generator-rss-mb", selfcalibration.DefaultGuardrails.MaxProcessRSSBytes/(1024*1024), "generator process RSS guardrail (MiB)")
	extended := fs.Bool("extended-envelope", false, "calibrate levels above the standard ceilings (loopback mirror only; up to the extended hard maximum)")
	out := fs.String("out", "", "self-calibration artifact output path (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *filename == "" || *out == "" || len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "--config and --out are required and positional arguments are not accepted")
		return 2
	}
	scenario, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	arrival := scenario.Mode == "arrival"
	if (arrival && *users != "") || (!arrival && *rates != "") {
		fmt.Fprintln(stderr, "use --users for closed-loop scenarios and --arrival-rates for open-arrival scenarios")
		return 2
	}
	raw, fallback := *users, scenario.Users
	if arrival {
		raw, fallback = *rates, scenario.ArrivalRate
	}
	levels, err := parseLevels(raw, fallback)
	if err != nil {
		fmt.Fprintf(stderr, "invalid levels: %v\n", err)
		return 2
	}
	if *maxRSSMB > (1<<64-1)/(1024*1024) {
		fmt.Fprintln(stderr, "--max-generator-rss-mb is too large")
		return 2
	}
	options := selfcalibration.Options{Levels: levels, LevelDuration: *levelDuration, ResponseBytes: *responseBytes, ServerDelay: *serverDelay,
		Guardrails:       generatorhealth.Guardrails{MaxProcessCPUPercent: *maxCPU, MaxProcessRSSBytes: *maxRSSMB * 1024 * 1024, ConsecutiveSamples: 3},
		ExtendedEnvelope: *extended}
	if *fromReport != "" {
		recorded, readErr := insights.ReadRun(*fromReport)
		if readErr != nil {
			fmt.Fprintf(stderr, "recorded report error: %v\n", readErr)
			return 2
		}
		data, readErr := calibration.ReadCalibrationBytes(*fromReport)
		if readErr != nil {
			fmt.Fprintf(stderr, "recorded report error: %v\n", readErr)
			return 2
		}
		options.FromReport = &recorded
		options.FromReportSHA256 = selfcalibration.DigestBytes(data)
	}
	fmt.Fprintf(stdout, "Workload self-calibration: %s | levels %v | %s per level | loopback mirror only; the scenario target is never contacted.\n", scenario.Name, levels, levelDuration.String())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	record, err := selfCalibrationRun(ctx, scenario, options)
	if err != nil && len(record.Levels) == 0 {
		fmt.Fprintf(stderr, "self-calibration error: %v\n", err)
		return 2
	}
	for _, level := range record.Levels {
		label := fmt.Sprintf("%d VUs", level.Users)
		if level.ArrivalRate > 0 {
			label = fmt.Sprintf("%d starts/s", level.ArrivalRate)
		}
		fmt.Fprintf(stdout, "  %s: %s | requests client=%d mirror=%d | overhead p95 %.1fms | delivery %.2f | peak CPU %.1f%%\n",
			label, level.Status, level.ClientRequests, level.ServerReceived, level.OverheadP95MS, level.DeliveryRatio, level.PeakProcessCPUPercent)
		if len(level.FailedChecks) > 0 {
			fmt.Fprintf(stdout, "    failed checks: %s\n", strings.Join(level.FailedChecks, ", "))
		}
	}
	if writeErr := report.WriteAnyJSON(*out, record); writeErr != nil {
		fmt.Fprintf(stderr, "artifact error: %v\n", writeErr)
		return 2
	}
	fmt.Fprintf(stdout, "Self-calibration: %s. Artifact: %s\n", record.Status, *out)
	fmt.Fprintln(stdout, "Scope: generator-side evidence only. It does not identify the target, verify application behavior, or establish workload representativeness.")
	if err != nil || record.Status != calibration.ScenarioCalibrationPassed {
		return 1
	}
	return 0
}

func parseLevels(raw string, fallback int) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		if fallback < 1 {
			return nil, errors.New("no level was given and the scenario does not declare one")
		}
		return []int{fallback}, nil
	}
	parts := strings.Split(raw, ",")
	levels := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", part)
		}
		levels = append(levels, value)
	}
	return levels, nil
}

// loadedCalibration holds exactly one of the two calibration artifact kinds.
type loadedCalibration struct {
	reference *calibration.Record
	scenario  *calibration.ScenarioRecord
}

func loadAnyCalibration(path string) (loadedCalibration, error) {
	data, err := calibration.ReadCalibrationBytes(path)
	if err != nil {
		return loadedCalibration{}, err
	}
	if calibration.IsScenarioCalibrationArtifact(data) {
		record, err := calibration.ParseScenarioRecord(data, true)
		if err != nil {
			return loadedCalibration{}, err
		}
		return loadedCalibration{scenario: &record}, nil
	}
	record, err := calibration.ValidateArtifactBytes(data, true)
	if err != nil {
		return loadedCalibration{}, err
	}
	return loadedCalibration{reference: &record}, nil
}

// scenarioCalibrationPreflight checks, before any traffic, that a
// self-calibration covers every planned level of this workload on this
// generator. The same conditions are recomputed per run by AssessQuality.
func scenarioCalibrationPreflight(record calibration.ScenarioRecord, v config.Validated, levels []int, now time.Time) error {
	if record.Status != calibration.ScenarioCalibrationPassed {
		return errors.New("self-calibration did not pass")
	}
	finished, err := time.Parse(time.RFC3339Nano, record.FinishedAt)
	if err != nil || now.Before(finished.Add(-5*time.Minute)) || now.Sub(finished) > calibration.ScenarioCalibrationMaxAge {
		return errors.New("self-calibration is future-dated or older than the 24-hour review threshold")
	}
	local := currentCalibrationRuntime()
	g := record.Generator
	if g.OS != local.OS || g.Architecture != local.Arch || g.GoVersion != local.GoVersion || g.GOMAXPROCS != local.GOMAXPROCS ||
		g.LogicalCPUs != local.LogicalCPUs || g.CPUCapacityCores != local.CPUCapacityCores || g.CPUCapacitySource != local.CPUCapacitySource || !local.CPUCapacityKnown {
		return errors.New("self-calibration was produced under a different generator runtime or CPU entitlement")
	}
	known := false
	for _, level := range record.Levels {
		if level.WorkloadFingerprint == v.WorkloadFingerprint {
			known = true
		}
	}
	if !known {
		return errors.New("self-calibration workload fingerprint differs from this scenario")
	}
	for _, want := range levels {
		found := false
		for _, level := range record.Levels {
			got := level.Users
			if v.Scenario.Mode == "arrival" {
				got = level.ArrivalRate
			}
			if got == want && level.Status == calibration.ScenarioCalibrationPassed {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("level %d was not self-calibrated", want)
		}
	}
	return nil
}

// withScenarioCalibration embeds the self-calibration in every run and
// recomputes measurement quality from the embedded evidence.
func withScenarioCalibration(record calibration.ScenarioRecord, runner phaseRunner) phaseRunner {
	return func(ctx context.Context, v config.Validated) report.Result {
		result := runner(ctx, v)
		attachScenarioCalibration(&result, record)
		return result
	}
}

func attachScenarioCalibration(result *report.Result, record calibration.ScenarioRecord) {
	embedded := record
	embedded.Levels = append([]calibration.ScenarioLevel(nil), record.Levels...)
	result.ScenarioCalibration = &embedded
	result.Quality = report.AssessQuality(*result)
}
