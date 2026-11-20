package capacityplan

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// blockWith records one blocker with an explicit category and remediation.
func (r *Result) blockWith(category, finding, action string) {
	for _, existing := range r.Findings {
		if existing == finding {
			return
		}
	}
	r.Blockers = append(r.Blockers, DecisionBlocker{Category: category, Finding: finding, Action: action})
	r.Findings = append(r.Findings, finding)
}

type calibrationGap struct {
	finding string
	action  string
	levels  []string
}

// collectCalibrationBlockers reports generator calibration and load-envelope
// states of the required (non-warmup) phases together, naming the affected
// load levels so one remediation covers all of them.
func collectCalibrationBlockers(evidence campaign.Result, assess qualityAssessor, result *Result) {
	order := []string{"missing", "malformed", "failed", "stale", "mismatched", "uncalibrated", "envelope_invalid", "generator_limited"}
	gaps := map[string]*calibrationGap{
		"missing":           {finding: "Required phases carry no generator calibration (neither a fixed reference preflight nor a workload self-calibration)", action: "Run `damocline calibrate` for these levels and repeat the campaign with `--calibration` and `--require-calibration`."},
		"malformed":         {finding: "Required phases carry malformed or schema-disallowed workload self-calibration evidence", action: "Run `damocline calibrate` for these levels with the current release and repeat the campaign."},
		"failed":            {finding: "Required phases carry a workload self-calibration that did not pass its generator delivery checks", action: "Run `damocline calibrate` for these levels on a less loaded or larger generator host; do not repeat the campaign until it passes."},
		"stale":             {finding: "Required phases carry a workload self-calibration that is future-dated or older than the 24-hour review threshold", action: "Run `damocline calibrate` for these levels again and repeat the campaign within 24 hours."},
		"mismatched":        {finding: "Required phases carry a workload self-calibration recorded for a different generator, CPU entitlement, runtime controls, or workload", action: "Run `damocline calibrate` for these levels on the same generator with the exact campaign scenario and controls."},
		"uncalibrated":      {finding: "Required phases run at load levels the attached workload self-calibration does not cover or did not pass", action: "Run `damocline calibrate` for these levels and repeat the campaign with the new artifact."},
		"envelope_invalid":  {finding: "Required phases carry malformed or inconsistent load-envelope evidence", action: "Repeat the campaign with a current release; use `--extended-envelope` only with its explicit authorization and READY calibration gate."},
		"generator_limited": {finding: "Required phases under the extended load envelope stopped as GENERATOR_LIMITED; they describe the load generator, not the target", action: "Reduce these levels or use a larger generator host, recalibrate with `damocline calibrate`, and repeat the campaign."},
	}
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			run := phase.Run
			// Phases whose recomputed quality is valid are not blocked by
			// their calibration state; the quality screen stays authoritative.
			if assess != nil {
				if q := assess(run); q != nil && q.State == report.QualityValid {
					continue
				}
			}
			level := phaseLevel(run)
			add := func(key string) {
				g := gaps[key]
				for _, existing := range g.levels {
					if existing == level {
						return
					}
				}
				g.levels = append(g.levels, level)
			}
			if !report.ValidEnvelope(run) {
				add("envelope_invalid")
			} else if run.Envelope != nil && run.Envelope.Mode == report.EnvelopeModeExtended && run.Envelope.StopReason == report.EnvelopeStopGeneratorLimited {
				add("generator_limited")
			}
			switch {
			case run.Calibration == nil && run.ScenarioCalibration == nil:
				add("missing")
			case run.Calibration == nil && run.ScenarioCalibration != nil:
				issue, invalid := report.ScenarioCalibrationIssue(run)
				switch {
				case invalid:
					add("malformed")
				case issue == "":
				case strings.Contains(issue, "did not pass its generator"):
					add("failed")
				case strings.Contains(issue, "24-hour"):
					add("stale")
				case strings.Contains(issue, "level"):
					add("uncalibrated")
				default:
					add("mismatched")
				}
			}
		}
	}
	for _, key := range order {
		g := gaps[key]
		if len(g.levels) == 0 {
			continue
		}
		levels := strings.Join(g.levels, ", ")
		category := BlockerEvidence
		if key == "generator_limited" || key == "envelope_invalid" {
			category = BlockerDamocline
		}
		result.blockWith(category, fmt.Sprintf("%s (levels: %s).", g.finding, levels), strings.Replace(g.action, "these levels", "these levels ("+levels+")", 1))
	}
}

func phaseLevel(run report.Result) string {
	if run.Config.Mode == "arrival" || run.Config.ArrivalRate > 0 {
		return strconv.Itoa(run.Config.ArrivalRate) + " arrivals/s"
	}
	return strconv.Itoa(run.Config.Users) + " VUs"
}
