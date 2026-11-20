package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

// assessExtendedCoverage is replaceable only by tests.
var assessExtendedCoverage = calibration.AssessExtendedEnvelope

type extendedEnvelopeFlags struct {
	enabled      *bool
	acknowledged *bool
	targetClass  *string
	maxCPU       *float64
	maxRSSMB     *uint64
	samples      *int
}

func registerExtendedEnvelopeFlags(fs *flag.FlagSet) extendedEnvelopeFlags {
	return extendedEnvelopeFlags{
		enabled:      fs.Bool("extended-envelope", false, "opt in to per-run ceilings above the standard envelope (hard maximum 500 VUs/workers, 1000 starts/s, 15m); requires --extended-load-authorized, --target-class, --calibration and --require-calibration"),
		acknowledged: fs.Bool("extended-load-authorized", false, "confirm the target owner authorized load above the standard ceilings"),
		targetClass:  fs.String("target-class", "", "target class for the extended envelope: lab or staging (production is refused)"),
		maxCPU:       fs.Float64("max-generator-cpu-percent", 0, "extended envelope: tighten the generator CPU guardrail (default 85, cannot be disabled)"),
		maxRSSMB:     fs.Uint64("max-generator-rss-mb", 0, "extended envelope: tighten the generator RSS guardrail in MiB (default 2048, cannot be disabled)"),
		samples:      fs.Int("guardrail-samples", 0, "extended envelope: consecutive samples before a guardrail stops the run (default 3, 2-20)"),
	}
}

func (f extendedEnvelopeFlags) request(requireCalibration bool, calibrationFile string) (*config.ExtendedEnvelopeRequest, error) {
	if !*f.enabled {
		if *f.acknowledged || *f.targetClass != "" || *f.maxCPU != 0 || *f.maxRSSMB != 0 || *f.samples != 0 {
			return nil, errors.New("extended-envelope options require --extended-envelope")
		}
		return nil, nil
	}
	if !*f.acknowledged {
		return nil, errors.New("--extended-envelope requires --extended-load-authorized")
	}
	if *f.targetClass == "" {
		return nil, errors.New("--extended-envelope requires --target-class lab or staging")
	}
	if !requireCalibration || calibrationFile == "" {
		return nil, errors.New("--extended-envelope requires --calibration and --require-calibration covering the exact planned level")
	}
	if *f.maxRSSMB > (1<<64-1)/(1024*1024) {
		return nil, errors.New("--max-generator-rss-mb is too large")
	}
	return &config.ExtendedEnvelopeRequest{Acknowledged: true, TargetClass: *f.targetClass,
		Guardrails: generatorhealth.Guardrails{MaxProcessCPUPercent: *f.maxCPU, MaxProcessRSSBytes: *f.maxRSSMB * 1024 * 1024, ConsecutiveSamples: *f.samples}}, nil
}

// gateExtendedEnvelope refuses before any traffic unless a fresh, matching
// calibration covers the exact planned level (ADR 0155).
func gateExtendedEnvelope(v *config.Validated, calibrationFile string, stdout, stderr io.Writer) int {
	loaded, err := loadAnyCalibration(calibrationFile)
	var record calibration.Record
	if err == nil && loaded.scenario != nil {
		record = calibration.Record{SelfCalibration: loaded.scenario, SelfCalibrationFingerprint: v.WorkloadFingerprint}
	} else if err == nil {
		record = *loaded.reference
	}
	if err != nil {
		fmt.Fprintf(stderr, "calibration error: %v\n", err)
		fmt.Fprintln(stderr, "EXTENDED RUN BLOCKED: no valid calibration artifact; no traffic was sent.")
		return 2
	}
	coverage := assessExtendedCoverage(v.Scenario, record, currentCalibrationRuntime(), time.Now().UTC())
	fmt.Fprintf(stdout, "Extended envelope (target class %s, hard maximum %d VUs/workers, %d starts/s, %s): calibration coverage %s — %s\n",
		v.Envelope.TargetClass, v.Envelope.Limits.MaxUsers, v.Envelope.Limits.MaxRPS, v.Envelope.Limits.MaxDuration, coverage.Status, coverage.Reason)
	if coverage.Status != "READY" {
		fmt.Fprintln(stderr, "EXTENDED RUN BLOCKED: calibration does not cover this exact workload level; no traffic was sent.")
		return 2
	}
	v.Envelope.CalibrationCoverage = coverage.Status
	g := v.GeneratorGuardrails
	fmt.Fprintf(stdout, "Generator guardrails enforced: CPU <= %.1f%%, RSS <= %d MiB, %d consecutive samples; exceeding either stops the run as GENERATOR_LIMITED.\n", g.MaxProcessCPUPercent, g.MaxProcessRSSBytes/(1024*1024), g.ConsecutiveSamples)
	return 0
}
