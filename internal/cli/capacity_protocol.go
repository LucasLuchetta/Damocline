package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
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
