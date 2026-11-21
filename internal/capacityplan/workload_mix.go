package capacityplan

import (
	"fmt"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// AssessmentSchemaVersion is the persisted capacity assessment schema.
// Version 7 adds confidence_dimensions.workload_mix (ADR 0171).
const AssessmentSchemaVersion = 7

// Workload-mix reference states when no comparison can be made.
const (
	ReferenceNotProvided  = "NOT_PROVIDED"
	ReferenceInconsistent = "INCONSISTENT"
)

// PhaseWorkloadMix is the executed-mix state of one required phase.
type PhaseWorkloadMix struct {
	Trial          int    `json:"trial"`
	Phase          string `json:"phase"`
	State          string `json:"state"`
	StartsState    string `json:"selected_starts_state,omitempty"`
	CompletedState string `json:"completed_journeys_state,omitempty"`
	ReferenceState string `json:"reference_state,omitempty"`
}

// WorkloadMixAssessment states whether the executed workflow mix was assessed
// against the declared weights in every required phase and, separately,
// whether the declared weights agree with an optional user-provided reference.
type WorkloadMixAssessment struct {
	Assessed       bool               `json:"assessed"`
	State          string             `json:"state"`
	ReferenceState string             `json:"reference_state"`
	Phases         []PhaseWorkloadMix `json:"phases"`
	Blockers       []DecisionBlocker  `json:"blockers"`
}

func assessWorkloadMix(e campaign.Result) WorkloadMixAssessment {
	out := WorkloadMixAssessment{State: "NOT_ASSESSED", ReferenceState: ReferenceNotProvided, Phases: []PhaseWorkloadMix{}, Blockers: []DecisionBlocker{}}
	block := func(category, finding, action string) {
		for _, existing := range out.Blockers {
			if existing.Finding == finding {
				return
			}
		}
		out.Blockers = append(out.Blockers, DecisionBlocker{Category: category, Finding: finding, Action: action})
	}
	required, withEvidence := 0, 0
	worst := report.MixMatched
	referenceStates := map[string]int{}
	for _, trial := range e.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			required++
			entry := PhaseWorkloadMix{Trial: trial.Number, Phase: phase.Name, State: "NOT_ASSESSED"}
			mix := phase.Run.ProfileMix
			switch {
			case phase.Run.SchemaVersion < report.ProfileMixSchemaVersion:
				block(BlockerEvidence, fmt.Sprintf("Phase reports use run schema v%d, which predates executed workflow-mix evidence.", phase.Run.SchemaVersion), "Repeat the campaign with a current Damocline build so every phase records run schema v40 workflow-mix evidence.")
			case len(phase.Run.Config.Profiles) == 0 || mix == nil:
				block(BlockerConfiguration, "The scenario does not declare weighted workflows, so no executed mix can be compared.", "Declare the intended mix as weighted profiles (for example 60/25/15) and repeat the campaign.")
			default:
				withEvidence++
				entry.State, entry.StartsState, entry.CompletedState = mix.State, mix.Starts.State, mix.Completed.State
				if mix.Reference != nil {
					entry.ReferenceState = mix.Reference.Fit.State
					referenceStates[mix.Reference.Fit.State]++
				} else {
					referenceStates[ReferenceNotProvided]++
				}
				switch mix.State {
				case report.MixDeviated:
					worst = report.MixDeviated
					block(BlockerEvidence, "The executed workflow mix deviates from the declared weights in at least one required phase.", "Inspect per-workflow failures and slow journeys that change the completed mix, fix the workload or target, and repeat the campaign.")
				case report.MixInsufficientSamples:
					if worst != report.MixDeviated {
						worst = report.MixInsufficientSamples
					}
					block(BlockerEvidence, "At least one required phase has fewer than 5 expected journey starts or completions for a declared workflow.", "Lengthen observation phases or raise the tested demand so every workflow has enough journeys per phase.")
				}
			}
			out.Phases = append(out.Phases, entry)
		}
	}
	if required == 0 {
		block(BlockerEvidence, "The campaign has no required phases with workflow-mix evidence.", "Run a campaign with observation phases.")
		return out
	}
	if withEvidence != required {
		return out
	}
	out.Assessed, out.State = true, worst
	switch {
	case len(referenceStates) > 1:
		out.ReferenceState = ReferenceInconsistent
		block(BlockerConfiguration, "The reference mix is not recorded identically in every required phase.", "Use one scenario reference declaration for every phase.")
	case referenceStates[ReferenceNotProvided] > 0:
		block(BlockerConfiguration, "No reference mix from real usage is recorded; the declared weights remain an assumption.", "Add profile_mix_reference_counts from a sanitized usage summary with a non-assumption profile_mix_basis.")
	default:
		for state := range referenceStates {
			out.ReferenceState = state
		}
		switch out.ReferenceState {
		case report.ReferenceDeviated:
			block(BlockerConfiguration, "The declared weights differ from the user-provided reference counts.", "Revisit the declared weights or the reference summary before treating the mix as representative.")
		case report.MixInsufficientSamples:
			block(BlockerEvidence, "The user-provided reference counts are too small to compare with the declared weights.", "Provide a reference summary with at least 5 expected observations per workflow.")
		}
	}
	return out
}
