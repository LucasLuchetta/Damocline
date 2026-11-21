package capacityplan

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

var passAudit = CampaignAudit{State: "OBSERVED_PASS_ONLY", MeasurementQuality: report.QualityLimited}

func applyMix(e *campaign.Result, starts, completed [3]int, reference []report.ProfileMixCount) {
	for trialIndex := range e.Trials {
		for phaseIndex := range e.Trials[trialIndex].Phases {
			run := &e.Trials[trialIndex].Phases[phaseIndex].Run
			run.SchemaVersion = report.ProfileMixSchemaVersion
			if reference != nil {
				run.Config.ProfileMixBasis, run.Config.ProfileMixReference = "sanitized_analytics", "synthetic-summary"
			}
			run.Config.ProfileMixReferenceCounts = reference
			run.Config.Profiles = []report.ProfileConfig{{Name: "view", Weight: 60}, {Name: "search", Weight: 25}, {Name: "update", Weight: 15, Writes: true}}
			run.Profiles = nil
			for i, profile := range run.Config.Profiles {
				run.Profiles = append(run.Profiles, report.ProfileResult{Name: profile.Name, Weight: profile.Weight, SelectedStarts: starts[i], Journeys: report.MetricSet{Total: completed[i], Success: completed[i]}})
			}
			run.ProfileMix = report.ComputeProfileMix(*run)
		}
	}
}

func TestWorkloadMixAssessmentStates(t *testing.T) {
	audit := passAudit
	cases := []struct {
		name               string
		starts, completed  [3]int
		reference          []report.ProfileMixCount
		want, wantRef      string
		wantBlockerContain string
	}{
		{"matched without reference", [3]int{600, 250, 150}, [3]int{600, 250, 150}, nil, report.MixMatched, ReferenceNotProvided, "No reference mix"},
		{"matched with reference", [3]int{600, 250, 150}, [3]int{599, 250, 150}, []report.ProfileMixCount{{Name: "view", Count: 6000}, {Name: "search", Count: 2500}, {Name: "update", Count: 1500}}, report.MixMatched, report.ReferenceMatched, ""},
		{"reference deviates", [3]int{600, 250, 150}, [3]int{600, 250, 150}, []report.ProfileMixCount{{Name: "view", Count: 3000}, {Name: "search", Count: 3000}, {Name: "update", Count: 4000}}, report.MixMatched, report.ReferenceDeviated, "differ from the user-provided reference"},
		{"completed deviates", [3]int{600, 250, 150}, [3]int{600, 250, 20}, nil, report.MixDeviated, ReferenceNotProvided, "deviates from the declared weights"},
		{"insufficient", [3]int{6, 3, 1}, [3]int{6, 3, 1}, nil, report.MixInsufficientSamples, ReferenceNotProvided, "fewer than 5 expected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := auditFixture()
			applyMix(&e, tc.starts, tc.completed, tc.reference)
			dimensions := assessConfidence(e, audit)
			mix := dimensions.WorkloadMix
			if mix == nil || !mix.Assessed || mix.State != tc.want || mix.ReferenceState != tc.wantRef || dimensions.WorkloadRepresentativeness != tc.want {
				t.Fatalf("unexpected mix assessment: %+v representativeness=%s", mix, dimensions.WorkloadRepresentativeness)
			}
			found := tc.wantBlockerContain == ""
			for _, blocker := range mix.Blockers {
				if tc.wantBlockerContain != "" && strings.Contains(blocker.Finding, tc.wantBlockerContain) && blocker.Action != "" {
					found = true
				}
			}
			if !found || (tc.wantBlockerContain == "" && len(mix.Blockers) != 0) {
				t.Fatalf("blockers do not match: %+v", mix.Blockers)
			}
			if dimensions.Decision != "NOT_ESTABLISHED" {
				t.Fatalf("a matched mix upgraded decision confidence: %+v", dimensions)
			}
		})
	}
}

func TestWorkloadMixNotAssessedForLegacyOrUndeclaredPhases(t *testing.T) {
	audit := passAudit
	e := auditFixture()
	dimensions := assessConfidence(e, audit)
	if dimensions.WorkloadMix == nil || dimensions.WorkloadMix.Assessed || dimensions.WorkloadRepresentativeness != "NOT_ASSESSED" || len(dimensions.WorkloadMix.Blockers) == 0 {
		t.Fatalf("legacy phases were assessed: %+v", dimensions.WorkloadMix)
	}
	mixed := auditFixture()
	applyMix(&mixed, [3]int{600, 250, 150}, [3]int{600, 250, 150}, nil)
	mixed.Trials[0].Phases[0].Run.ProfileMix = nil
	mixed.Trials[0].Phases[0].Run.Config.Profiles = nil
	dimensions = assessConfidence(mixed, audit)
	if dimensions.WorkloadMix.Assessed || !strings.Contains(dimensions.WorkloadMix.Blockers[0].Action, "weighted profiles") {
		t.Fatalf("a phase without a declared mix did not block assessment: %+v", dimensions.WorkloadMix)
	}
	dimensions = assessConfidence(mixed, CampaignAudit{State: "INVALID_EVIDENCE"})
	if dimensions.WorkloadMix.Assessed || dimensions.WorkloadRepresentativeness != "NOT_ASSESSED" {
		t.Fatalf("invalid evidence was assessed: %+v", dimensions.WorkloadMix)
	}
}
