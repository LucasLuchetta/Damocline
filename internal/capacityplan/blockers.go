package capacityplan

import (
	"slices"
	"strings"
)

type DecisionBlocker struct {
	Category string `json:"category"`
	Finding  string `json:"finding"`
	Action   string `json:"action"`
}

const (
	BlockerConfiguration = "configuration"
	BlockerEvidence      = "insufficient_evidence"
	BlockerDamocline     = "damocline_limitation"
)

func (r *Result) block(finding string) {
	if slices.Contains(r.Findings, finding) {
		return
	}
	category := BlockerEvidence
	action := "Repeat the campaign with the planned phases and preserve the complete report."
	switch {
	case strings.Contains(finding, "required target"), strings.Contains(finding, "exact calculated workload"):
		category = BlockerConfiguration
		action = "Run the campaign at the exact required VU level."
	case strings.Contains(finding, "statistically assessable"):
		category = BlockerConfiguration
		action = "Set valid request and journey latency objectives and a nonzero error budget."
	case strings.Contains(finding, "schema-v22"), strings.Contains(finding, "Legacy campaign"):
		category = BlockerDamocline
		action = "Record a new campaign with a current Damocline release."
	case strings.Contains(finding, "LIMITED measurement"):
		category = BlockerDamocline
		action = "Inspect the run quality findings and qualify the generator for the workload. Treat results beyond the supported reference envelope as descriptive."
	case strings.Contains(finding, "binomial"):
		action = "Collect more complete samples in every affected series and window, or reduce the observed failure rate."
	case strings.Contains(finding, "rank interval"):
		action = "Collect more samples and verify the upper p95 bound stays within the objective."
	case strings.Contains(finding, "Campaign outcome"):
		action = "Review the failed or incomplete phase and repeat the full schedule after resolving it."
	case strings.Contains(finding, "breached"):
		action = "Inspect the affected request and journey windows and rerun the same objectives after addressing the breach."
	case strings.Contains(finding, "drift"):
		action = "Inspect the window series and repeat once the workload and environment are steady."
	case strings.Contains(finding, "INVALID measurement"), strings.Contains(finding, "recompute"):
		action = "Inspect the phase quality findings and reconcile delivery and generator counters before repeating."
	case strings.Contains(finding, "SLO window"), strings.Contains(finding, "SLO-window"):
		action = "Configure complete SLO windows and rerun with per-step and per-workflow measurements."
	}
	r.Blockers = append(r.Blockers, DecisionBlocker{Category: category, Finding: finding, Action: action})
	r.Findings = append(r.Findings, finding)
}
