package cli

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/selfcalibration"
)

// capacityMirror describes how the self-calibration mirror is sized: from a
// recorded run of the real target, or from declared values.
type capacityMirror struct {
	FromReport    string
	ResponseBytes int
	ServerDelay   time.Duration
	LevelDuration time.Duration
	Extended      bool
	TargetClass   string
}

// capacityCalibrationLevels reserves the ladder before splitting the widest
// remaining intervals. Only these predeclared levels can be probed.
func capacityCalibrationLevels(opt explorer.Options, duration time.Duration) []int {
	levels := explorer.PlannedLevels(opt)
	for len(levels) < calibration.ScenarioCalibrationMaxLevels {
		if _, err := capacityCalibrationWall(len(levels)+1, duration); err != nil {
			break
		}
		widest := 0
		midpoint := 0
		for i := 1; i < len(levels); i++ {
			if gap := levels[i] - levels[i-1]; gap > widest {
				widest = gap
				midpoint = levels[i-1] + gap/2
			}
		}
		if widest < 2 {
			break
		}
		levels = append(levels, midpoint)
		slices.Sort(levels)
	}
	return levels
}

func (m capacityMirror) declared() bool {
	return m.FromReport != "" || m.ResponseBytes != 0 || m.ServerDelay != 0
}

func (m capacityMirror) args(file, out string, levels []int) []string {
	users := make([]string, len(levels))
	for i, level := range levels {
		users[i] = strconv.Itoa(level)
	}
	args := []string{"--config", file, "--users", strings.Join(users, ","), "--level-duration", m.LevelDuration.String(), "--out", out}
	if m.FromReport != "" {
		args = append(args, "--from-report", m.FromReport)
	} else {
		args = append(args, "--response-bytes", strconv.Itoa(m.ResponseBytes), "--server-delay", m.ServerDelay.String())
	}
	if m.Extended {
		args = append(args, "--extended-envelope")
	}
	return args
}

// capacityCalibrationWall returns the conservative wall time for
// self-calibrating the levels, or an error with a proposed adjustment when it
// does not fit the self-calibration ceilings.
func capacityCalibrationWall(levels int, duration time.Duration) (time.Duration, error) {
	if levels > calibration.ScenarioCalibrationMaxLevels {
		return 0, fmt.Errorf("%d planned levels exceed the self-calibration maximum of %d; lower --max-users", levels, calibration.ScenarioCalibrationMaxLevels)
	}
	wall := selfcalibration.PlannedWallTime(levels, duration)
	if wall > calibration.ScenarioCalibrationMaxWallTime {
		return 0, fmt.Errorf("self-calibration of %d levels needs %s, above the %s ceiling; shorten --calibration-level-duration or lower --max-users", levels, wall, calibration.ScenarioCalibrationMaxWallTime)
	}
	return wall, nil
}

// capacityRateWarning warns before traffic when the scenario max_rps would
// cap throughput below the delivery the requested level implies, based on
// the per-VU request rate of a recorded run of the same target.
func capacityRateWarning(w io.Writer, referencePath string, users, maxRPS int) error {
	if referencePath == "" || maxRPS <= 0 {
		return nil
	}
	recorded, err := insights.ReadRun(referencePath)
	if err != nil {
		return fmt.Errorf("reference report: %w", err)
	}
	if recorded.ElapsedSeconds <= 0 || recorded.Config.Users <= 0 || recorded.Metrics.Total <= 0 {
		return errors.New("reference report has no usable request rate")
	}
	observed := float64(recorded.Metrics.Total) / recorded.ElapsedSeconds
	if recorded.Config.MaxRPS > 0 && observed >= 0.9*float64(recorded.Config.MaxRPS) {
		fmt.Fprintf(w, "WARNING: the reference delivered %.1f requests/s against its own max_rps %d. Its rate may have been capped and can underestimate delivery at the planned max_rps %d. Use a representative reference with observed rate headroom to forecast delivery.\n", observed, recorded.Config.MaxRPS, maxRPS)
		return nil
	}
	perVU := observed / float64(recorded.Config.Users)
	expected := perVU * float64(users)
	fmt.Fprintf(w, "Reference delivery: %.3f requests/s per VU; %d VUs imply about %.1f requests/s against max_rps %d.\n", perVU, users, expected, maxRPS)
	if expected > float64(maxRPS) {
		fmt.Fprintf(w, "WARNING: max_rps %d would cap throughput below the %d VU level (about %.0f requests/s); the measured capacity would reflect the rate cap, not the target. Raise max_rps within its ceiling or lower --max-users to about %d.\n",
			maxRPS, users, expected, max(1, int(float64(maxRPS)/perVU)))
	} else if expected >= 0.9*float64(maxRPS) {
		fmt.Fprintf(w, "WARNING: reference delivery for %d VUs is at least 90%% of max_rps %d; the exploration may be inconclusive at this ceiling.\n", users, maxRPS)
	}
	return nil
}

// capacityObservedRateWarning warns when the explored campaign level already
// ran at or near the max_rps cap.
func capacityObservedRateWarning(w io.Writer, result explorer.Result, users, maxRPS int) {
	if maxRPS <= 0 {
		return
	}
	for _, probe := range result.Probes {
		if probe.Users != users {
			continue
		}
		for _, run := range probe.Runs {
			if run.ElapsedSeconds > 0 && float64(run.Metrics.Total)/run.ElapsedSeconds >= 0.9*float64(maxRPS) {
				fmt.Fprintf(w, "WARNING: the %d VU level delivered at least 90%% of max_rps %d; throughput is likely capped by the scenario rate limit, not the target.\n", users, maxRPS)
				return
			}
		}
	}
}

// ensureCapacityCalibration reuses a matching passing self-calibration or
// runs `damocline calibrate` for the levels. It returns calibrate's exit code.
func ensureCapacityCalibration(path, file string, v config.Validated, levels []int, mirror capacityMirror, stdout, stderr io.Writer) int {
	if _, err := os.Stat(path); err == nil {
		if loaded, err := loadAnyCalibration(path); err == nil && loaded.scenario != nil &&
			scenarioCalibrationPreflight(*loaded.scenario, v, levels, time.Now().UTC()) == nil {
			fmt.Fprintf(stdout, "Reused passing workload self-calibration for levels %v.\n", levels)
			return 0
		}
		if err := os.Rename(path, path+"."+strconv.FormatInt(time.Now().UnixNano(), 10)+".stale"); err != nil {
			fmt.Fprintf(stderr, "capacity: %v\n", err)
			return 2
		}
	}
	code := calibrateCmd(mirror.args(file, path, levels), stdout, stderr)
	switch code {
	case 0:
	case 1:
		fmt.Fprintf(stderr, "capacity: self-calibration did not pass for levels %v; no target traffic was sent for these levels. Use a less loaded or larger generator host, or lower --max-users.\n", levels)
		return 2
	default:
		fmt.Fprintf(stderr, "capacity: self-calibration could not start for levels %v (see the error above); no target traffic was sent. Fix the scenario or calibration options and rerun.\n", levels)
		return 2
	}
	return 0
}

// capacityExploreStage sizes the explore stage so the lowest level can reach
// min_samples_per_step for every step, using the per-VU request rate of a
// recorded run of the same target. Without a usable reference it reports
// false and the caller keeps its default. The result stays within the
// explorer's 10s-3m stage bounds and never goes below 20s.
func capacityExploreStage(s config.Scenario, minUsers int, referencePath string) (time.Duration, bool) {
	if referencePath == "" || minUsers <= 0 || len(s.Steps) == 0 {
		return 0, false
	}
	recorded, err := insights.ReadRun(referencePath)
	if err != nil || recorded.ElapsedSeconds <= 0 || recorded.Config.Users <= 0 || recorded.Metrics.Total <= 0 {
		return 0, false
	}
	need := s.MinSamplesPerStep
	if need <= 0 {
		need = 1
	}
	perStep := float64(recorded.Metrics.Total) / recorded.ElapsedSeconds / float64(recorded.Config.Users) * float64(minUsers) / float64(len(s.Steps))
	if s.MaxRPS > 0 {
		perStep = min(perStep, float64(s.MaxRPS)/float64(len(s.Steps)))
	}
	if perStep <= 0 {
		return 0, false
	}
	ramp, _ := time.ParseDuration(s.RampUp)
	// 25% headroom over the expected collection time, plus the ramp.
	stage := time.Duration(float64(need)/perStep*1.25*float64(time.Second)) + ramp
	stage = stage.Round(time.Second)
	if stage < 20*time.Second {
		stage = 20 * time.Second
	}
	if stage > 3*time.Minute {
		stage = 3 * time.Minute
	}
	return stage, true
}

// capacityTiming holds the protocol's fixed stage floors. Only tests shorten
// them, and always within the explore and campaign validators.
var capacityTiming = struct {
	ExploreStage, Warmup, Recovery time.Duration
}{ExploreStage: 20 * time.Second, Warmup: 10 * time.Second, Recovery: 15 * time.Second}

// capacityStageSetupAllowance covers per-stage process, report and
// connection setup outside the engine's own bounded phases.
const capacityStageSetupAllowance = 2 * time.Second

// capacityStageOverhead is the worst-case wall time one explore stage may
// use beyond its stage duration, derived from the bounds the engine
// enforces: the network baseline wall cap (unless the scenario skips the
// baseline), the bounded post-schedule drain and a setup allowance. Ramp-up
// runs inside the stage duration (ADR 0170).
func capacityStageOverhead(s config.Scenario, users int, extended *config.ExtendedEnvelopeRequest, insecureHTTP bool) (time.Duration, error) {
	s.Users = users
	s.WindowDuration = ""
	s.PerVUCookieEnvPrefix = ""
	v, err := config.Parse(s, config.Options{Authorized: true, NetworkBaseline: true, AllowInsecureHTTP: insecureHTTP, ExtendedEnvelope: extended})
	if err != nil {
		return 0, err
	}
	overhead := engine.DrainAllowance(v) + capacityStageSetupAllowance
	if s.NetworkBaselineSkipReason == "" {
		overhead += config.NetworkBaselineWallBudget
	}
	return overhead, nil
}

// capacityWorstCaseProbes is the worst-case number of explore probes: the
// ladder up to a failing level plus the refinement probes of that bracket,
// maximised over every possible failing level (or the full ladder when all
// pass). Standard refinement bisects any level inside a bracket; extended
// refinement bisects only calibrated levels (ADR 0157).
func capacityWorstCaseProbes(ladder, calibrated []int, extended bool) int {
	worst := len(ladder)
	for i := 1; i < len(ladder); i++ {
		candidates := ladder[i] - ladder[i-1] - 1
		if extended {
			candidates = 0
			for _, level := range calibrated {
				if level > ladder[i-1] && level < ladder[i] {
					candidates++
				}
			}
		}
		worst = max(worst, i+1+bits.Len(uint(max(0, candidates))))
	}
	return worst
}

// capacityExpectedOverhead is the per-stage setup, network preflight and drain
// time the capacity planner expects in practice. The worst case (each bound
// at its cap) is still reported and enforced by the explorer budget itself.
const capacityExpectedOverhead = 10 * time.Second
