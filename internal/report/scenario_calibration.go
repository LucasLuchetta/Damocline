package report

import (
	"reflect"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
)

// ScenarioCalibrationIssue decides whether an embedded workload
// self-calibration (ADR 0154) applies to this run. It uses only the report
// and the embedded artifact, so offline audits recompute the same verdict.
// An empty issue means the generator-side calibration matches: workload
// fingerprint, calibrated level, run controls, executable identity, CPU
// entitlement and age. invalid marks evidence that must not be trusted at all.
func ScenarioCalibrationIssue(r Result) (issue string, invalid bool) {
	rec := r.ScenarioCalibration
	if rec == nil {
		return "No workload self-calibration is attached.", false
	}
	if r.SchemaVersion < ScenarioCalibrationSchemaVersion {
		return "Workload self-calibration evidence is not allowed by the report schema.", true
	}
	if err := calibration.ValidateScenarioRecord(*rec); err != nil {
		return "Attached workload self-calibration is malformed or inconsistent.", true
	}
	if rec.Status != calibration.ScenarioCalibrationPassed {
		return "Attached workload self-calibration did not pass its generator delivery checks.", false
	}
	finished, _ := time.Parse(time.RFC3339Nano, rec.FinishedAt)
	runStarted, err := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil || runStarted.Before(finished.Add(-5*time.Minute)) || runStarted.Sub(finished) > calibration.ScenarioCalibrationMaxAge {
		return "Workload self-calibration is future-dated or older than the 24-hour review threshold for this run.", false
	}
	g := rec.Generator
	id := r.GeneratorIdentity
	if id.RunnerIdentityMethod == "unavailable" || id.OS != g.OS || id.Architecture != g.Architecture || id.GoVersion != g.GoVersion ||
		id.RunnerIdentityMethod != g.RunnerIdentityMethod || id.RunnerIdentity != g.RunnerIdentity {
		return "Workload self-calibration was produced by a different generator executable or runtime.", false
	}
	if r.Generator == nil || !r.Generator.CPUCapacityKnown || !g.CPUCapacityKnown || r.Generator.CPUCapacityCores != g.CPUCapacityCores ||
		r.Generator.CPUCapacitySource != g.CPUCapacitySource || r.Generator.GOMAXPROCS != g.GOMAXPROCS || r.Generator.LogicalCPUs != g.LogicalCPUs {
		return "Workload self-calibration was produced under a different or unknown generator CPU entitlement.", false
	}
	if !scenarioControlsMatch(rec.Controls, r) {
		return "Run controls differ from the self-calibrated workload controls.", false
	}
	// Fingerprints of one workload differ across levels only by the per-VU
	// session count. Commands that slice sessions from a base scenario (for
	// example explore and campaign spikes) record the base-level fingerprint,
	// so any calibrated level's fingerprint identifies the same workload.
	known := false
	for _, level := range rec.Levels {
		if level.WorkloadFingerprint != "" && level.WorkloadFingerprint == r.Config.WorkloadFingerprint {
			known = true
		}
	}
	if !known {
		return "The run's workload fingerprint differs from the self-calibrated workload.", false
	}
	for _, level := range rec.Levels {
		if level.Users != r.Config.Users || level.ArrivalRate != r.Config.ArrivalRate {
			continue
		}
		if level.Status != calibration.ScenarioCalibrationPassed {
			return "The self-calibrated level for this run did not pass.", false
		}
		return "", false
	}
	return "The run's load level was not self-calibrated.", false
}

// ScenarioCalibrationMatches reports whether an embedded self-calibration
// applies to the run with no remaining generator-side issue.
func ScenarioCalibrationMatches(r Result) bool {
	issue, _ := ScenarioCalibrationIssue(r)
	return issue == ""
}

func scenarioControlsMatch(c calibration.ScenarioControls, r Result) bool {
	cfg := r.Config
	samples := cfg.ThinkTimeSamples
	if len(samples) == 0 {
		samples = nil
	}
	rangeMin, rangeMax := "", ""
	if cfg.ThinkTimeRange != nil {
		rangeMin, rangeMax = cfg.ThinkTimeRange.Min, cfg.ThinkTimeRange.Max
	}
	return c.Mode == cfg.Mode && c.Steps == len(r.Steps) && c.MaxRPS == cfg.MaxRPS && c.RequestTimeout == cfg.RequestTimeout &&
		c.ConnectionPolicy == cfg.ConnectionPolicy && c.MaxConnectionsPerHost == cfg.MaxConnectionsPerHost && c.ThinkTime == cfg.ThinkTime &&
		c.ThinkTimeRangeMin == rangeMin && c.ThinkTimeRangeMax == rangeMax && reflect.DeepEqual(c.ThinkTimeSamples, samples) &&
		c.WorkloadSeed == cfg.WorkloadSeed && c.PerVUCookieSessions == cfg.SeparateSessions
}
