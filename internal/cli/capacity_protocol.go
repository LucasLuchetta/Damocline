package cli

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
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
	perVU := float64(recorded.Metrics.Total) / recorded.ElapsedSeconds / float64(recorded.Config.Users)
	expected := perVU * float64(users)
	fmt.Fprintf(w, "Reference delivery: %.3f requests/s per VU; %d VUs imply about %.1f requests/s against max_rps %d.\n", perVU, users, expected, maxRPS)
	if expected > float64(maxRPS) {
		fmt.Fprintf(w, "WARNING: max_rps %d would cap throughput below the %d VU level (about %.0f requests/s); the measured capacity would reflect the rate cap, not the target. Raise max_rps within its ceiling or lower --max-users to about %d.\n",
			maxRPS, users, expected, max(1, int(float64(maxRPS)/perVU)))
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

// capacityTiming holds the protocol's fixed stage floors. Tests may shorten
// them within the explore and campaign validators; production never does.
var capacityTiming = struct {
	ExploreStage, Warmup, Recovery time.Duration
}{ExploreStage: 20 * time.Second, Warmup: 10 * time.Second, Recovery: 15 * time.Second}

// capacityEnvelopeArgs forwards the extended-envelope opt-in to explore and
// campaign so traffic stages run under the same envelope as calibration.
func capacityEnvelopeArgs(extended bool, targetClass string) []string {
	if !extended {
		return nil
	}
	return []string{"--extended-envelope", "--extended-load-authorized", "--target-class", targetClass}
}

// capacityExtendedCalibrationLevels adds bisection fill-in levels to the
// explore ladder so extended bracket refinement, which probes only calibrated
// levels (ADR 0157), can narrow a failing bracket. Fill-ins are added breadth
// first, top bracket first, while the self-calibration level and wall
// ceilings allow. It also returns the worst-case number of refinement probes
// the calibrated set permits, to reserve explore time for them.
func capacityExtendedCalibrationLevels(ladder []int, duration time.Duration) ([]int, int) {
	limit := calibration.ScenarioCalibrationMaxLevels
	for limit > len(ladder) && selfcalibration.PlannedWallTime(limit, duration) > calibration.ScenarioCalibrationMaxWallTime {
		limit--
	}
	type bracket struct{ low, high int }
	queue := []bracket{}
	for i := len(ladder) - 1; i > 0; i-- {
		queue = append(queue, bracket{ladder[i-1], ladder[i]})
	}
	set := map[int]bool{}
	for _, level := range ladder {
		set[level] = true
	}
	for len(queue) > 0 && len(set) < limit {
		b := queue[0]
		queue = queue[1:]
		if b.high-b.low <= 1 {
			continue
		}
		mid := b.low + (b.high-b.low)/2
		set[mid] = true
		queue = append(queue, bracket{mid, b.high}, bracket{b.low, mid})
	}
	levels := make([]int, 0, len(set))
	for level := range set {
		levels = append(levels, level)
	}
	sort.Ints(levels)
	probes := 0
	for i := 1; i < len(ladder); i++ {
		inside := 0
		for _, level := range levels {
			if level > ladder[i-1] && level < ladder[i] {
				inside++
			}
		}
		probes = max(probes, bits.Len(uint(inside)))
	}
	return levels, probes
}

// capacityCoarseBracket returns the highest repeatedly passing level of an
// extended exploration that stopped because no calibrated level remained
// inside the failing bracket (ADR 0157). The campaign then confirms that
// conservative level; the bracket is never reported as refined.
func capacityCoarseBracket(result explorer.Result) (int, bool) {
	if result.Outcome != "INCONCLUSIVE" || !strings.HasPrefix(result.Reason, explorer.UncalibratedRefinementReason) {
		return 0, false
	}
	best := 0
	for _, probe := range result.Probes {
		if probe.Status == "PASS" && probe.Users > best {
			best = probe.Users
		}
	}
	return best, best > 0
}

// capacityGuardrails mirrors the guardrails campaign enforces under the
// extended envelope so the protocol validates the same schedule up front.
func capacityGuardrails(extended bool) generatorhealth.Guardrails {
	if !extended {
		return generatorhealth.Guardrails{}
	}
	resolved, err := config.ExtendedGuardrails(generatorhealth.Guardrails{})
	if err != nil {
		return generatorhealth.Guardrails{}
	}
	return resolved
}

// capacityStageSetupAllowance covers per-stage process, report and
// connection setup outside the engine's own bounded phases.
const capacityStageSetupAllowance = 2 * time.Second

// capacityStageOverhead is the worst-case wall time one explore stage may
// use beyond its stage duration, derived from the same bounds the engine
// enforces: the network baseline wall cap (when the baseline runs), the
// bounded post-schedule drain and a setup allowance. Ramp-up is inside the
// stage duration.
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
