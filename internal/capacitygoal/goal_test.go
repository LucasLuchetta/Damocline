package capacitygoal

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func passed(users int) report.Result {
	return report.Result{
		SchemaVersion: 3, Outcome: "PASS", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: users, Mode: "closed", MaxRPS: 100, Duration: "30s", SeparateSessions: true},
		Metrics: report.MetricSet{Total: 25, Success: 25},
		Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.17,
			ProcessSamples: 4, ProcessCPUSamples: 3, ProcessCPUValid: true, PeakProcessRSSBytes: 1024},
	}
}
func exploration(user int) explorer.Result {
	return explorer.Result{
		SchemaVersion: 1, Scenario: "Synthetic validation", StageDuration: "30s",
		MinUsers: 1, MaxUsers: 50, Repeats: 2, Outcome: "BRACKETED",
		Probes: []explorer.Probe{{Users: user, Status: "PASS", Runs: []report.Result{passed(user), passed(user)}}},
	}
}
func TestTargetMathAccountsForGrowthAndReserve(t *testing.T) {
	a, err := Evaluate(exploration(30), Goal{PeakUsers: 20, GrowthPercent: 25, ReservePercent: 20})
	if err != nil || a.TargetUsers != 30 || a.State != "SHORT_TRIAL_TARGET_PASSED" || a.MatchedTrials != 2 {
		t.Fatalf("unexpected short trial interpretation: %+v err=%v", a, err)
	}
	if !strings.Contains(strings.Join(a.Findings, " "), "Sustained capacity") {
		t.Fatal("must not claim sustained capacity from two short trials")
	}
}
func TestOutsideCeilingDoesNotInventCapacity(t *testing.T) {
	a, err := Evaluate(exploration(10), Goal{PeakUsers: 90})
	if err != nil || a.State != "OUTSIDE_TESTED_CEILING" || a.TargetUsers != 90 {
		t.Fatalf("should request wider tests, not extrapolate: %+v %v", a, err)
	}
}
func TestHigherPassingLevelDoesNotReplaceDirectTargetTest(t *testing.T) {
	a, err := Evaluate(exploration(40), Goal{PeakUsers: 30})
	if err != nil || a.State != "TARGET_NOT_DIRECTLY_TESTED" {
		t.Fatalf("must not silently interpolate from a different VU level: %+v %v", a, err)
	}
}
func TestGeneratorSaturationBlocksBusinessClaim(t *testing.T) {
	ex := exploration(20)
	ex.Probes[0].Runs[1].Generator.GoCPUUtilizationRatio = 0.95
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("generator-limited result must not validate demand: %+v %v", a, err)
	}
}

func TestProcessGeneratorSaturationBlocksBusinessClaim(t *testing.T) {
	ex := exploration(20)
	ex.Probes[0].Runs[1].Generator.MeanProcessCPUUtilizationRatio = 0.95
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("process-level generator saturation must block the business claim: %+v %v", a, err)
	}
}

func TestCancelledArrivalsAndJourneysBlockExactTargetClaim(t *testing.T) {
	cancelled := int64(1)
	for _, mutate := range []func(*report.Result){
		func(r *report.Result) { r.ArrivalsCancelledBeforeAdmission = &cancelled },
		func(r *report.Result) { r.Journeys.Cancelled = 1 },
		func(r *report.Result) { r.Quality = &report.Quality{State: report.QualityInvalid} },
		func(r *report.Result) { r.SchemaVersion = report.CurrentSchemaVersion },
	} {
		ex := exploration(20)
		mutate(&ex.Probes[0].Runs[1])
		a, err := Evaluate(ex, Goal{PeakUsers: 20})
		if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
			t.Fatalf("incomplete or invalid delivery supported an exact-target claim: %+v err=%v", a, err)
		}
	}
}
func TestClientRateCeilingBlocksClaim(t *testing.T) {
	ex := exploration(20)
	for i := range ex.Probes[0].Runs {
		ex.Probes[0].Runs[i].Config.MaxRPS = 1
		ex.Probes[0].Runs[i].Metrics.Total = 30 // 1 RPS achieved of 1 configured RPS
		ex.Probes[0].Runs[i].Metrics.Success = 30
	}
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("client rate cap must prevent passing inference: %+v %v", a, err)
	}
}
func TestFailedStageDoesNotAutomaticallyJustifyBiggerServer(t *testing.T) {
	ex := exploration(20)
	ex.Probes[0].Status = "FAIL"
	ex.Probes[0].Runs = []report.Result{passed(20)}
	ex.Probes[0].Runs[0].Outcome = "FAIL"
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "SLO_MISSED_AT_TESTED_TARGET" {
		t.Fatalf("failure interpretation invalid: %+v %v", a, err)
	}
	if !strings.Contains(strings.Join(a.Findings, " "), "does not identify a hardware bottleneck") {
		t.Fatal("must not recommend a bigger server from response failure")
	}
}
func TestMissingGeneratorEvidenceDoesNotValidateDemand(t *testing.T) {
	ex := exploration(20)
	ex.Probes[0].Runs[0].Generator = nil
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("uncalibrated generator cannot validate demand: %+v %v", a, err)
	}
}

func TestMissingProcessHealthDoesNotValidateDemand(t *testing.T) {
	ex := exploration(20)
	ex.Probes[0].Runs[1].Generator.ProcessCPUValid = false
	a, err := Evaluate(ex, Goal{PeakUsers: 20})
	if err != nil || a.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("missing process health must not validate the target: %+v %v", a, err)
	}
}
func TestMalformedAndInvalidGoalAreRejected(t *testing.T) {
	for _, goal := range []Goal{{PeakUsers: 0}, {PeakUsers: 1000001}, {PeakUsers: 3, GrowthPercent: -1}, {PeakUsers: 4, ReservePercent: 1001}} {
		if _, err := Evaluate(exploration(20), goal); err == nil {
			t.Fatalf("invalid goal accepted: %+v", goal)
		}
	}
	ex := exploration(20)
	ex.Repeats = 1
	if _, err := Evaluate(ex, Goal{PeakUsers: 20}); err == nil {
		t.Fatal("single trial is insufficient")
	}
}
