package explorer

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func extendedBase(t *testing.T) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{
		Name: "extended synthetic", BaseURL: "http://127.0.0.1:18080",
		Users: 40, Duration: "10s", MaxRPS: 500, P95LimitMS: 1000,
		Steps: []config.Step{{Name: "local", Path: "/"}},
	}, config.Options{Authorized: true, ExtendedEnvelope: &config.ExtendedEnvelopeRequest{Acknowledged: true, TargetClass: config.TargetClassLab}})
	if err != nil {
		t.Fatal(err)
	}
	v.Envelope.CalibrationCoverage = "READY"
	return v
}

func coveredSet(levels ...int) func(int) bool {
	return func(users int) bool { return slices.Contains(levels, users) }
}

func extendedOptions(covered func(int) bool) Options {
	return Options{MinUsers: 40, MaxUsers: 80, Repeats: 2, StageDuration: 10 * time.Second,
		UserCeiling: report.ExtendedMaxUsers, Extended: true, TargetClass: config.TargetClassLab, LevelCovered: covered}
}

// failAbove passes every level up to limit and fails above it.
func failAbove(limit int, probed *[]int) Runner {
	return func(_ context.Context, v config.Validated) report.Result {
		if len(*probed) == 0 || (*probed)[len(*probed)-1] != v.Scenario.Users {
			*probed = append(*probed, v.Scenario.Users)
		}
		if v.Scenario.Users > limit {
			return report.Result{Outcome: "FAIL"}
		}
		return report.Result{Outcome: "PASS"}
	}
}

func TestStandardExplorationCeilingIsUnchanged(t *testing.T) {
	if err := Validate(Options{MinUsers: 40, MaxUsers: 51, Repeats: 2, StageDuration: 10 * time.Second}); err == nil {
		t.Fatal("standard exploration accepted 51 VUs")
	}
	if err := Validate(Options{MinUsers: 40, MaxUsers: 80, Repeats: 2, StageDuration: 10 * time.Second, UserCeiling: report.ExtendedMaxUsers}); err == nil {
		t.Fatal("ceiling raised without the extended envelope")
	}
	if err := Validate(extendedOptions(nil)); err != nil {
		t.Fatalf("extended options rejected: %v", err)
	}
	opt := extendedOptions(nil)
	opt.MaxUsers = report.ExtendedMaxUsers + 1
	if err := Validate(opt); err == nil {
		t.Fatal("extended exploration accepted a level above the hard maximum")
	}
}

func TestExtendedExplorationRequiresMatchingEnvelopeAndCoverageCheck(t *testing.T) {
	v := extendedBase(t)
	var probed []int
	if _, err := WithRunner(context.Background(), v, extendedOptions(nil), failAbove(80, &probed)); err == nil {
		t.Fatal("extended exploration ran without a refinement coverage check")
	}
	standard := v
	standard.Envelope = config.EnvelopeRecord{Mode: config.EnvelopeStandard}
	if _, err := WithRunner(context.Background(), standard, extendedOptions(coveredSet(40, 80)), failAbove(80, &probed)); err == nil {
		t.Fatal("extended options accepted a standard scenario envelope")
	}
	if len(probed) != 0 {
		t.Fatalf("refused explorations probed %v", probed)
	}
}

func TestExtendedExplorationRecordsEnvelope(t *testing.T) {
	var probed []int
	r, err := WithRunner(context.Background(), extendedBase(t), extendedOptions(coveredSet(40, 80)), failAbove(80, &probed))
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "PASSED_TO_CONFIGURED_CEILING" || !slices.Equal(probed, []int{40, 80}) {
		t.Fatalf("outcome=%s probed=%v", r.Outcome, probed)
	}
	if r.SchemaVersion != EnvelopeSchemaVersion || r.LoadEnvelope == nil || r.LoadEnvelope.Mode != report.EnvelopeModeExtended || !r.LoadEnvelope.ExceedsStandard {
		t.Fatalf("envelope not recorded: schema=%d envelope=%+v", r.SchemaVersion, r.LoadEnvelope)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "above the standard 50 VU ceiling") {
		t.Fatalf("notes do not state the extended envelope: %v", r.Notes)
	}
}

func TestExtendedRefinementNeverProbesUncalibratedLevels(t *testing.T) {
	var probed []int
	r, err := WithRunner(context.Background(), extendedBase(t), extendedOptions(coveredSet(40, 80)), failAbove(60, &probed))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(probed, []int{40, 80}) || r.Outcome != "INCONCLUSIVE" || !strings.HasPrefix(r.Reason, UncalibratedRefinementReason) || !strings.Contains(r.Reason, "40-80") {
		t.Fatalf("uncalibrated refinement handled incorrectly: probed=%v outcome=%s reason=%q", probed, r.Outcome, r.Reason)
	}
	if r.HighestPassingUsers != nil || r.FirstFailingUsers != nil {
		t.Fatal("an unrefined extended bracket was reported as BRACKETED evidence")
	}
}

func TestExtendedRefinementUsesNearestCalibratedLevels(t *testing.T) {
	var probed []int
	r, err := WithRunner(context.Background(), extendedBase(t), extendedOptions(coveredSet(40, 55, 70, 80)), failAbove(60, &probed))
	if err != nil {
		t.Fatal(err)
	}
	// Midpoint 60 is uncalibrated: 55 is probed (closest, lower on ties),
	// then 70 inside 55-80 fails, leaving 55-70 without calibrated levels.
	if !slices.Equal(probed, []int{40, 80, 55, 70}) || r.Outcome != "INCONCLUSIVE" || !strings.Contains(r.Reason, "55-70") {
		t.Fatalf("probed=%v outcome=%s reason=%q", probed, r.Outcome, r.Reason)
	}
	every := func(int) bool { return true }
	probed = nil
	r, err = WithRunner(context.Background(), extendedBase(t), extendedOptions(every), failAbove(60, &probed))
	if err != nil || r.Outcome != "BRACKETED" || *r.HighestPassingUsers != 60 || *r.FirstFailingUsers != 61 {
		t.Fatalf("fully calibrated bracket did not refine: %+v %v", r, err)
	}
}

func TestExtendedGuardrailStopIsGeneratorLimited(t *testing.T) {
	r, err := WithRunner(context.Background(), extendedBase(t), extendedOptions(coveredSet(40, 80)), func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "ABORTED", Envelope: &report.Envelope{Mode: report.EnvelopeModeExtended, StopReason: report.EnvelopeStopGeneratorLimited}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "INCONCLUSIVE" || r.Probes[0].Status != "INCONCLUSIVE" || r.Probes[0].Reason != GeneratorLimitedReason || r.HighestPassingUsers != nil || r.FirstFailingUsers != nil {
		t.Fatalf("guardrail stop was not a generator-limited non-passing probe: %+v", r)
	}
}

func TestValidateEnvelopeRejectsInconsistentEvidence(t *testing.T) {
	legacy := Result{SchemaVersion: 2, MaxUsers: 80}
	if ValidateEnvelope(legacy) == nil {
		t.Fatal("schema 2 exploration above 50 VUs was accepted")
	}
	ok := Result{SchemaVersion: 3, MaxUsers: 80, LoadEnvelope: report.NewAggregateEnvelope(true, "lab", 80)}
	if err := ValidateEnvelope(ok); err != nil {
		t.Fatalf("valid extended envelope rejected: %v", err)
	}
	relabelled := ok
	relabelled.LoadEnvelope = report.NewAggregateEnvelope(false, "", 80)
	if ValidateEnvelope(relabelled) == nil {
		t.Fatal("80-VU exploration relabelled standard was accepted")
	}
	understated := ok
	understated.LoadEnvelope = report.NewAggregateEnvelope(true, "lab", 40)
	if ValidateEnvelope(understated) == nil {
		t.Fatal("extended envelope that hides levels above the standard ceiling was accepted")
	}
	mismatchedRun := ok
	mismatchedRun.Probes = []Probe{{Users: 80, Status: "PASS", Runs: []report.Result{{SchemaVersion: report.CurrentSchemaVersion}}}}
	if ValidateEnvelope(mismatchedRun) == nil {
		t.Fatal("embedded run without an extended envelope was accepted")
	}
}
