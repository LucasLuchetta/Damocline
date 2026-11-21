package report

import (
	"math"
	"testing"
)

func TestFitMixChiSquareThresholds(t *testing.T) {
	// chi-square 3.841459 with one degree of freedom has p = 0.05.
	if p := chiSquareSurvival(3.841459, 1); math.Abs(p-0.05) > 1e-6 {
		t.Fatalf("df=1 survival = %v", p)
	}
	// chi-square 9.210340 with two degrees of freedom has p = 0.01.
	if p := chiSquareSurvival(9.210340, 2); math.Abs(p-0.01) > 1e-6 {
		t.Fatalf("df=2 survival = %v", p)
	}
	if p := chiSquareSurvival(30, 10); math.Abs(p-0.000857) > 1e-5 {
		t.Fatalf("df=10 survival = %v", p)
	}
	weights := []float64{60, 25, 15}
	if fit := FitMix([]int64{600, 250, 150}, weights, MixExecutedShareTolerance); fit.State != MixMatched || fit.ChiSquare != 0 || fit.PValue != 1 {
		t.Fatalf("exact mix: %+v", fit)
	}
	if fit := FitMix([]int64{400, 400, 200}, weights, MixExecutedShareTolerance); fit.State != MixDeviated {
		t.Fatalf("deviated mix: %+v", fit)
	}
	if fit := FitMix([]int64{20, 8, 2}, weights, MixExecutedShareTolerance); fit.State != MixInsufficientSamples {
		t.Fatalf("small mix: %+v", fit)
	}
	// Huge samples: significant but within the share tolerance stays matched.
	if fit := FitMix([]int64{6_010_000, 2_495_000, 1_495_000}, weights, MixExecutedShareTolerance); fit.PValue >= MixFitAlpha || fit.State != MixMatched {
		t.Fatalf("negligible large-sample deviation: %+v", fit)
	}
}

func TestProfileMixSchemaGating(t *testing.T) {
	r := Result{SchemaVersion: ProfileMixSchemaVersion - 1, Config: RunConfig{Profiles: []ProfileConfig{{Name: "a", Weight: 1, Writes: true}}}}
	if ValidProfileMix(r) == nil {
		t.Fatal("pre-v40 report carried v40 workflow declarations")
	}
	r.Config.Profiles[0].Writes = false
	r.ProfileMix = &ProfileMixEvidence{}
	if ValidProfileMix(r) == nil {
		t.Fatal("pre-v40 report carried profile_mix")
	}
	r = Result{SchemaVersion: ProfileMixSchemaVersion, Config: RunConfig{Profiles: []ProfileConfig{{Name: "a", Weight: 1, DataVariability: DataVariabilityFixed}}}}
	if ValidProfileMix(r) == nil {
		t.Fatal("v40 weighted run without profile_mix was accepted")
	}
	r.ProfileMix = ComputeProfileMix(r)
	if err := ValidProfileMix(r); err != nil {
		t.Fatal(err)
	}
	r.Config.ProfileMixReferenceCounts = []ProfileMixCount{{Name: "a", Count: 3}}
	if ValidProfileMix(r) == nil {
		t.Fatal("reference counts without a non-assumption basis were accepted")
	}
}
