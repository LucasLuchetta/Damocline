package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// ProfileMixSchemaVersion is the first run schema that records executed
// workflow-mix goodness-of-fit evidence, per-workflow write and
// data-variability declarations and optional user-provided reference counts
// (ADR 0171).
const ProfileMixSchemaVersion = 40

// Workflow-mix fit method and fixed thresholds. They are documented in
// docs/METRICS.md and are not configurable, so a saved report can be
// recomputed offline without trusting producer-supplied policy.
const (
	MixFitMethod = "pearson_chi_square_v1"
	// MixFitAlpha is the significance level below which a fit can deviate.
	MixFitAlpha = 0.01
	// MixFitMinExpected is the minimum expected count per workflow under the
	// declared weights before the chi-square approximation is used.
	MixFitMinExpected = 5.0
	// MixExecutedShareTolerance is the largest absolute share difference
	// (fraction of 1) tolerated between executed and declared mixes.
	MixExecutedShareTolerance = 0.02
	// MixReferenceShareTolerance is the tolerance between a user-provided
	// reference mix and the declared mix.
	MixReferenceShareTolerance = 0.05
)

// Mix fit states.
const (
	MixMatched             = "MIX_MATCHED"
	MixDeviated            = "MIX_DEVIATED"
	MixInsufficientSamples = "INSUFFICIENT_SAMPLES"
	ReferenceMatched       = "REFERENCE_MATCHED"
	ReferenceDeviated      = "REFERENCE_DEVIATED"
)

// Data-variability labels derived from the declared workflow steps.
const (
	DataVariabilityFixed               = "fixed"
	DataVariabilitySyntheticPerJourney = "synthetic_per_journey"
)

// ProfileMixCount is one user-provided count for a declared workflow, for
// example from a sanitized access-log summary. It is a reference assumption,
// never measured evidence about the target.
type ProfileMixCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// MixFit is one goodness-of-fit comparison of observed counts with the
// declared workflow weights.
type MixFit struct {
	Method            string  `json:"method"`
	Alpha             float64 `json:"alpha"`
	MinExpectedCount  float64 `json:"min_expected_count"`
	ShareTolerance    float64 `json:"share_tolerance"`
	Total             int64   `json:"total"`
	DegreesOfFreedom  int     `json:"degrees_of_freedom"`
	ChiSquare         float64 `json:"chi_square"`
	PValue            float64 `json:"p_value"`
	SmallestExpected  float64 `json:"smallest_expected_count"`
	MaxShareDeviation float64 `json:"max_share_deviation"`
	State             string  `json:"state"`
}

// ProfileMixWorkflow records the declared and executed share of one workflow.
type ProfileMixWorkflow struct {
	Name              string  `json:"name"`
	Weight            int     `json:"weight"`
	DeclaredShare     float64 `json:"declared_share"`
	SelectedStarts    int     `json:"selected_starts"`
	CompletedJourneys int     `json:"completed_journeys"`
	ReferenceCount    *int64  `json:"reference_count,omitempty"`
}

// ProfileMixReference records the optional user-provided reference mix.
// Provenance is the declared basis and label from the scenario; Damocline does
// not verify that the counts represent real demand.
type ProfileMixReference struct {
	Provenance string `json:"provenance"`
	Basis      string `json:"basis"`
	Label      string `json:"label"`
	Fit        MixFit `json:"declared_vs_reference"`
}

// ProfileMixEvidence is the executed-mix evidence for one run or phase.
type ProfileMixEvidence struct {
	State     string               `json:"state"`
	Workflows []ProfileMixWorkflow `json:"workflows"`
	Starts    MixFit               `json:"selected_starts_fit"`
	Completed MixFit               `json:"completed_journeys_fit"`
	Reference *ProfileMixReference `json:"reference,omitempty"`
}

// ReferenceProvenance marks reference counts as user-provided assumptions.
const ReferenceProvenance = "user_provided_assumption"

// ComputeProfileMix derives executed-mix evidence from a run's declared
// profile configuration and observed per-profile counters. It returns nil when
// the run did not declare explicit weighted workflows.
func ComputeProfileMix(r Result) *ProfileMixEvidence {
	if len(r.Config.Profiles) == 0 {
		return nil
	}
	weights := make([]float64, len(r.Config.Profiles))
	starts := make([]int64, len(r.Config.Profiles))
	completed := make([]int64, len(r.Config.Profiles))
	observed := map[string]ProfileResult{}
	for _, profile := range r.Profiles {
		observed[profile.Name] = profile
	}
	reference := map[string]int64{}
	for _, count := range r.Config.ProfileMixReferenceCounts {
		reference[count.Name] = count.Count
	}
	evidence := &ProfileMixEvidence{Workflows: make([]ProfileMixWorkflow, 0, len(r.Config.Profiles))}
	total := 0
	for _, profile := range r.Config.Profiles {
		total += profile.Weight
	}
	refs := make([]int64, len(r.Config.Profiles))
	for i, profile := range r.Config.Profiles {
		got := observed[profile.Name]
		weights[i] = float64(profile.Weight)
		starts[i] = int64(got.SelectedStarts)
		completed[i] = int64(got.Journeys.Total)
		entry := ProfileMixWorkflow{Name: profile.Name, Weight: profile.Weight, SelectedStarts: got.SelectedStarts, CompletedJourneys: got.Journeys.Total}
		if total > 0 {
			entry.DeclaredShare = roundRatio(float64(profile.Weight) / float64(total))
		}
		if count, ok := reference[profile.Name]; ok && len(r.Config.ProfileMixReferenceCounts) > 0 {
			value := count
			entry.ReferenceCount = &value
			refs[i] = count
		}
		evidence.Workflows = append(evidence.Workflows, entry)
	}
	evidence.Starts = FitMix(starts, weights, MixExecutedShareTolerance)
	evidence.Completed = FitMix(completed, weights, MixExecutedShareTolerance)
	evidence.State = worstMixState(evidence.Starts.State, evidence.Completed.State)
	if len(r.Config.ProfileMixReferenceCounts) > 0 {
		fit := FitMix(refs, weights, MixReferenceShareTolerance)
		switch fit.State {
		case MixMatched:
			fit.State = ReferenceMatched
		case MixDeviated:
			fit.State = ReferenceDeviated
		}
		evidence.Reference = &ProfileMixReference{Provenance: ReferenceProvenance, Basis: r.Config.ProfileMixBasis, Label: r.Config.ProfileMixReference, Fit: fit}
	}
	return evidence
}

func worstMixState(states ...string) string {
	state := MixMatched
	for _, current := range states {
		if current == MixDeviated {
			return MixDeviated
		}
		if current == MixInsufficientSamples {
			state = MixInsufficientSamples
		}
	}
	return state
}

func roundRatio(value float64) float64 { return math.Round(value*1e6) / 1e6 }

// FitMix applies Pearson's chi-square goodness-of-fit test of observed counts
// against shares proportional to weights. The fit is INSUFFICIENT_SAMPLES when
// any expected count is below MixFitMinExpected. It is MIX_DEVIATED only when
// the p-value is below MixFitAlpha and the largest absolute share difference
// exceeds tolerance; very large samples therefore do not flag negligible
// differences, and small samples cannot flag large ones by share alone.
func FitMix(counts []int64, weights []float64, tolerance float64) MixFit {
	fit := MixFit{Method: MixFitMethod, Alpha: MixFitAlpha, MinExpectedCount: MixFitMinExpected, ShareTolerance: tolerance, State: MixInsufficientSamples}
	weightTotal := 0.0
	for i, count := range counts {
		fit.Total += count
		weightTotal += weights[i]
	}
	if len(counts) == 0 || weightTotal <= 0 {
		return fit
	}
	fit.DegreesOfFreedom = len(counts) - 1
	smallest := math.Inf(1)
	chi := 0.0
	maxDeviation := 0.0
	for i, count := range counts {
		share := weights[i] / weightTotal
		expected := float64(fit.Total) * share
		smallest = math.Min(smallest, expected)
		if expected > 0 {
			diff := float64(count) - expected
			chi += diff * diff / expected
		}
		if fit.Total > 0 {
			maxDeviation = math.Max(maxDeviation, math.Abs(float64(count)/float64(fit.Total)-share))
		}
	}
	fit.SmallestExpected = roundRatio(smallest)
	fit.ChiSquare = roundRatio(chi)
	fit.MaxShareDeviation = roundRatio(maxDeviation)
	fit.PValue = 1
	if fit.DegreesOfFreedom > 0 {
		fit.PValue = roundRatio(chiSquareSurvival(chi, fit.DegreesOfFreedom))
	}
	if smallest < MixFitMinExpected {
		return fit
	}
	if fit.PValue < MixFitAlpha && fit.MaxShareDeviation > tolerance {
		fit.State = MixDeviated
	} else {
		fit.State = MixMatched
	}
	return fit
}

// chiSquareSurvival returns P(X >= x) for a chi-square variable with k
// degrees of freedom, using the regularized upper incomplete gamma function.
func chiSquareSurvival(x float64, k int) float64 {
	if x <= 0 {
		return 1
	}
	return upperRegularizedGamma(float64(k)/2, x/2)
}

func upperRegularizedGamma(a, x float64) float64 {
	lg, _ := math.Lgamma(a)
	if x < a+1 {
		// Series for the lower function.
		sum, term := 1/a, 1/a
		for n := 1; n < 10000; n++ {
			term *= x / (a + float64(n))
			sum += term
			if math.Abs(term) < math.Abs(sum)*1e-15 {
				break
			}
		}
		return math.Max(0, 1-sum*math.Exp(-x+a*math.Log(x)-lg))
	}
	// Lentz continued fraction for the upper function.
	tiny := 1e-300
	b := x + 1 - a
	c := 1 / tiny
	d := 1 / b
	h := d
	for i := 1; i < 10000; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		d = an*d + b
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = b + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		delta := d * c
		h *= delta
		if math.Abs(delta-1) < 1e-15 {
			break
		}
	}
	return math.Min(1, math.Exp(-x+a*math.Log(x)-lg)*h)
}

// ValidProfileMix checks schema gating and recomputes executed-mix evidence
// from the saved report. Offline recomputation must match the persisted
// evidence exactly.
func ValidProfileMix(r Result) error {
	if r.SchemaVersion < ProfileMixSchemaVersion {
		if r.ProfileMix != nil || len(r.Config.ProfileMixReferenceCounts) > 0 {
			return errors.New("profile_mix and profile_mix_reference_counts are not allowed before schema v40")
		}
		for _, profile := range r.Config.Profiles {
			if profile.Writes || profile.DataVariability != "" {
				return errors.New("profile writes and data_variability are not allowed before schema v40")
			}
		}
		return nil
	}
	for _, profile := range r.Config.Profiles {
		if profile.DataVariability != DataVariabilityFixed && profile.DataVariability != DataVariabilitySyntheticPerJourney {
			return fmt.Errorf("profile %q data_variability must be %s or %s", profile.Name, DataVariabilityFixed, DataVariabilitySyntheticPerJourney)
		}
	}
	if len(r.Config.ProfileMixReferenceCounts) > 0 {
		if len(r.Config.ProfileMixReferenceCounts) != len(r.Config.Profiles) || r.Config.ProfileMixBasis == "" || r.Config.ProfileMixBasis == "assumption" || r.Config.ProfileMixReference == "" {
			return errors.New("profile_mix_reference_counts require a non-assumption basis, a label and one count per declared workflow")
		}
		names := map[string]bool{}
		for _, profile := range r.Config.Profiles {
			names[profile.Name] = true
		}
		for _, count := range r.Config.ProfileMixReferenceCounts {
			if !names[count.Name] || count.Count < 0 {
				return errors.New("profile_mix_reference_counts must name each declared workflow once with a non-negative count")
			}
			delete(names, count.Name)
		}
	}
	want := ComputeProfileMix(r)
	if want == nil {
		if r.ProfileMix != nil {
			return errors.New("profile_mix requires declared weighted workflows")
		}
		return nil
	}
	if r.ProfileMix == nil {
		return errors.New("schema v40 runs with weighted workflows must record profile_mix")
	}
	gotJSON, err1 := json.Marshal(r.ProfileMix)
	wantJSON, err2 := json.Marshal(want)
	if err1 != nil || err2 != nil || string(gotJSON) != string(wantJSON) {
		return errors.New("profile_mix does not match offline recomputation from the saved profile counters")
	}
	return nil
}

// WithoutProfileMixDeclarations removes schema-v40 workflow-mix declarations
// so a current configuration projection can be compared with an older report.
func WithoutProfileMixDeclarations(c RunConfig) RunConfig {
	c.ProfileMixReferenceCounts = nil
	if c.Profiles != nil {
		profiles := make([]ProfileConfig, len(c.Profiles))
		copy(profiles, c.Profiles)
		for i := range profiles {
			profiles[i].Writes, profiles[i].DataVariability = false, ""
		}
		c.Profiles = profiles
	}
	return c
}
