package capacityplan

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestEvaluateRequiresExactSustainedEvidence(t *testing.T) {
	goal := capacitygoal.Goal{PeakUsers: 100, GrowthPercent: 20, ReservePercent: 25}
	passing := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 100, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	result, err := evaluateWithDeclaredTestQuality(passing, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || result.TargetUsers != 150 {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
	passing.BaseUsers = 150
	result, err = evaluateWithDeclaredTestQuality(passing, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "Legacy campaign reports lack") {
		t.Fatalf("legacy campaign gained a positive demand conclusion: %+v %v", result, err)
	}
	passing.BaseUsers = 200
	result, err = evaluateWithDeclaredTestQuality(passing, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("different workload level must not be interpolated: %+v %v", result, err)
	}
}

func TestEvaluateRequiresPerStepEvidenceForEveryObservationWindow(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	evidence.Trials[0].Phases[0].Run.Windows[0].Steps = nil
	result, err := evaluateWithDeclaredTestQuality(evidence, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "per-step SLO-window evidence") {
		t.Fatalf("aggregate windows cannot hide missing per-step evidence: %+v %v", result, err)
	}
}

func TestEvaluateRequiresValidDataQualityForEveryRequiredPhase(t *testing.T) {
	goal := capacitygoal.Goal{PeakUsers: 10}
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: report.QualityLimited}
	result, err := evaluateWithDeclaredTestQuality(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "LIMITED measurement quality") {
		t.Fatalf("limited phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = nil
	result, err = evaluateWithDeclaredTestQuality(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "missing explicit data-quality evidence") {
		t.Fatalf("missing phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: "UNRECOGNIZED"}
	result, err = evaluateWithDeclaredTestQuality(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "unknown measurement quality") {
		t.Fatalf("unknown phase quality must block a positive demand result: %+v %v", result, err)
	}

	evidence.Trials[1].Phases[0].Run.Quality = &report.Quality{State: report.QualityValid}
	invalidRun := &evidence.Trials[0].Phases[1].Run
	invalidRun.Metrics.Total = -1
	invalidRun.Quality = report.AssessQuality(*invalidRun)
	result, err = evaluateWithDeclaredTestQuality(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "INVALID measurement quality") {
		t.Fatalf("invalid recovery quality must block a positive demand result: %+v %v", result, err)
	}

	// Warm-up establishes workload state but does not participate in approval.
	invalidRun.Metrics.Total = 300
	invalidRun.Quality = &report.Quality{State: report.QualityValid}
	evidence.Trials[0].Phases = append([]campaign.Phase{{Name: "warmup", Run: report.Result{Outcome: "PASS"}}}, evidence.Trials[0].Phases...)
	evidence.Trials[0].Phases[2].Run.Quality = &report.Quality{State: report.QualityValid}
	evidence.Trials[0].Phases[1].Run.Quality = &report.Quality{State: report.QualityValid}
	result, err = evaluateWithDeclaredTestQuality(evidence, goal)
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "Legacy campaign reports lack") {
		t.Fatalf("legacy campaign must remain insufficient after warm-up is excluded: %+v %v", result, err)
	}
}

func TestEvaluateRecomputesClaimedValidQuality(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	result, err := Evaluate(evidence, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "claims VALID_WITHIN_ENVELOPE measurement quality but its report fields recompute as") {
		t.Fatalf("a self-declared valid state without supporting phase evidence must be rejected: %+v %v", result, err)
	}
}

func TestEvaluateRequiresAdequateConditionalStatisticalEvidence(t *testing.T) {
	goal := capacitygoal.Goal{PeakUsers: 10}
	t.Run("zero of fifty cannot establish one percent error budget", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
		for trialIndex := range evidence.Trials {
			for phaseIndex := range evidence.Trials[trialIndex].Phases {
				run := &evidence.Trials[trialIndex].Phases[phaseIndex].Run
				run.Metrics.Total, run.Metrics.Success = 50, 50
			}
		}
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "conditional binomial bound") {
			t.Fatalf("0/50 must not establish a 1%% error budget: %+v %v", result, err)
		}
	})
	t.Run("missing percentile interval blocks", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
		evidence.Trials[0].Phases[0].Run.Steps[0].Metrics.P95IID95Approximation = false
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "per-step lacks an adequate p95 rank interval") {
			t.Fatalf("missing per-step percentile interval must block: %+v %v", result, err)
		}
	})
	t.Run("missing windows block", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
		evidence.Trials[1].Phases[0].Run.Windows = nil
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "complete, configured SLO-window evidence") {
			t.Fatalf("missing window metrics must block: %+v %v", result, err)
		}
	})
	t.Run("degraded SLO window blocks", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
		evidence.Trials[0].Phases[0].Run.Windows[0].Metrics.P95IID95UpperMS = 101
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "phase window lacks an adequate p95 rank interval") {
			t.Fatalf("a degraded window must not be hidden by aggregate metrics: %+v %v", result, err)
		}
	})
	t.Run("gapped window coverage blocks", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
		evidence.Trials[0].Phases[0].Run.Windows[0].StartSeconds = 1
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "missing, overlapping, or out-of-range SLO windows") {
			t.Fatalf("gapped window coverage must block: %+v %v", result, err)
		}
	})
	t.Run("missing repeat variability blocks", func(t *testing.T) {
		evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials()}
		result, err := evaluateWithDeclaredTestQuality(evidence, goal)
		if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "variability is not recorded") {
			t.Fatalf("missing variability must block: %+v %v", result, err)
		}
	})
}

func TestEvaluateRejectsIncompletePassingCampaign(t *testing.T) {
	result, err := Evaluate(campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 20, Repeats: 2}, capacitygoal.Goal{PeakUsers: 20})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("unexpected: %+v %v", result, err)
	}
}

func TestSchemaThreeRequiresExactStageSequenceAndClassification(t *testing.T) {
	makeEvidence := func() campaign.Result {
		evidence := campaign.Result{
			SchemaVersion: 3, Scenario: "synthetic", Target: "http://127.0.0.1", Outcome: "PASS",
			BaseUsers: 10, Repeats: 2, Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Budget: "2m20s",
			Trials: passingTrials(), ObservationVariability: validVariability(),
		}
		for trialIndex := range evidence.Trials {
			for phaseIndex := range evidence.Trials[trialIndex].Phases {
				phase := &evidence.Trials[trialIndex].Phases[phaseIndex]
				phase.Run.Config.Users = 10
				phase.Run.Config.Duration = "10s"
				phase.Run.Config.WindowDuration = "2.5s"
				phase.Run.Target = evidence.Target
				phase.EvidenceState = "PASS_OBSERVED"
				phase.Run.Metrics.Total, phase.Run.Metrics.Success = 1200, 1200
				phase.Run.Steps[0].Metrics.Total, phase.Run.Steps[0].Metrics.Success = 1200, 1200
				phase.Run.Journeys.Total, phase.Run.Journeys.Success = 1200, 1200
				phase.Run.Windows = nil
				for windowIndex := 0; windowIndex < 4; windowIndex++ {
					metrics := validStatisticalRun().Metrics
					metrics.Total, metrics.Success = 300, 300
					journeys := metrics
					journeys.Total, journeys.Success = 300, 300
					phase.Run.Windows = append(phase.Run.Windows, report.WindowResult{StartSeconds: float64(windowIndex) * 2.5, EndSeconds: float64(windowIndex+1) * 2.5, Metrics: metrics, Journeys: journeys, Steps: []report.WindowMetric{{Name: "step", Metrics: metrics}}})
				}
				if phase.Name == "observation" {
					phase.Stability = "NO_LARGE_DRIFT_DETECTED"
				}
			}
		}
		return evidence
	}
	if err := validateEvidence(makeEvidence()); err != nil {
		t.Fatalf("valid staged evidence rejected: %v", err)
	}
	path := filepath.Join(t.TempDir(), "campaign.json")
	if err := report.WriteAnyJSON(path, makeEvidence()); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadCampaign(path)
	if err != nil || validateEvidence(restored) != nil {
		t.Fatalf("schema-v3 report did not round-trip: %v", err)
	}
	positive, err := evaluateWithDeclaredTestQuality(makeEvidence(), capacitygoal.Goal{PeakUsers: 10})
	if err != nil || positive.State != "GOAL_MET_UNDER_TESTED_CONDITIONS" {
		t.Fatalf("complete schema-v3 fixture was not usable: %+v %v", positive, err)
	}
	legacyV2 := makeEvidence()
	legacyV2.SchemaVersion = 2
	legacyPositive, err := evaluateWithDeclaredTestQuality(legacyV2, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || legacyPositive.State != "GOAL_MET_UNDER_TESTED_CONDITIONS" {
		t.Fatalf("schema-v2 staged campaign compatibility failed: %+v %v", legacyPositive, err)
	}
	v3 := makeEvidence()
	v3.SchemaVersion = 3
	v3.ObservationVariability.SuccessfulJourneysMinPerSec = 4
	v3.ObservationVariability.SuccessfulJourneysMaxPerSec = 5
	currentPositive, err := evaluateWithDeclaredTestQuality(v3, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || currentPositive.State != "GOAL_MET_UNDER_TESTED_CONDITIONS" {
		t.Fatalf("complete schema-v3 evidence rejected: %+v %v", currentPositive, err)
	}
	v3.ObservationVariability.SuccessfulJourneysMinPerSec = math.NaN()
	malformed, err := evaluateWithDeclaredTestQuality(v3, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || malformed.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(malformed.Findings, "journey throughput spread is missing or malformed") {
		t.Fatalf("malformed schema-v3 journey throughput accepted: %+v %v", malformed, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*campaign.Result)
	}{
		{"duplicate phase", func(r *campaign.Result) { r.Trials[0].Phases[1].Name = "observation" }},
		{"wrong load", func(r *campaign.Result) { r.Trials[0].Phases[0].Run.Config.Users = 11 }},
		{"wrong duration", func(r *campaign.Result) { r.Trials[0].Phases[0].Run.Config.Duration = "9s" }},
		{"wrong target", func(r *campaign.Result) { r.Trials[0].Phases[0].Run.Target = "http://127.0.0.2" }},
		{"false classification", func(r *campaign.Result) { r.Trials[0].Phases[0].EvidenceState = "FAIL_OBSERVED" }},
		{"false stability", func(r *campaign.Result) { r.Trials[0].Phases[0].Run.Windows[3].Metrics.P95MS = 40 }},
		{"missing drain budget", func(r *campaign.Result) { r.Budget = "10s" }},
		{"wrong trial number", func(r *campaign.Result) { r.Trials[1].Number = 1 }},
		{"aborted warmup", func(r *campaign.Result) {
			r.Warmup = "10s"
			r.Trials[0].Phases = append([]campaign.Phase{{Name: "warmup", EvidenceState: "INSUFFICIENT_EVIDENCE", Run: report.Result{Outcome: "ABORTED", Config: report.RunConfig{Users: 10, Duration: "10s"}, Target: r.Target}}}, r.Trials[0].Phases...)
			r.Trials[1].Phases = append([]campaign.Phase{{Name: "warmup", EvidenceState: "INSUFFICIENT_EVIDENCE", Run: report.Result{Outcome: "ABORTED", Config: report.RunConfig{Users: 10, Duration: "10s"}, Target: r.Target}}}, r.Trials[1].Phases...)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := makeEvidence()
			tc.mutate(&evidence)
			if err := validateEvidence(evidence); err == nil {
				t.Fatal("malformed campaign accepted")
			}
		})
	}
}

func TestSchemaFourRequiresFinalSoakEvidence(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: 4, Scenario: "synthetic", Target: "http://127.0.0.1", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Warmup: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Soak: "10s", Budget: "3m20s", Trials: passingTrials(), ObservationVariability: validVariability()}
	for i := range evidence.Trials {
		for j := range evidence.Trials[i].Phases {
			p := &evidence.Trials[i].Phases[j]
			p.Run.Config.Users, p.Run.Config.Duration, p.Run.Config.WindowDuration = 10, "10s", "2.5s"
			p.Run.Target = evidence.Target
			p.Run.Outcome = "PASS"
			p.EvidenceState = "PASS_OBSERVED"
			p.Run.Metrics, p.Run.Journeys = validStatisticalRun().Metrics, validStatisticalRun().Metrics
			p.Run.Steps = []report.StepResult{{Name: "step", Metrics: validStatisticalRun().Metrics}}
			p.Run.Windows = nil
			for k := 0; k < 4; k++ {
				m := validStatisticalRun().Metrics
				m.Total, m.Success = 300, 300
				p.Run.Windows = append(p.Run.Windows, report.WindowResult{StartSeconds: float64(k) * 2.5, EndSeconds: float64(k+1) * 2.5, Metrics: m, Journeys: m, Steps: []report.WindowMetric{{Name: "step", Metrics: m}}})
			}
			if p.Name == "observation" || p.Name == "soak" {
				p.Stability = "NO_LARGE_DRIFT_DETECTED"
			}
		}
	}
	soak := campaign.Phase{Name: "soak", EvidenceState: "PASS_OBSERVED", Stability: "NO_LARGE_DRIFT_DETECTED", Run: evidence.Trials[1].Phases[0].Run}
	soak.Run.Config.Duration = "10s"
	evidence.Trials[1].Phases = append(evidence.Trials[1].Phases, soak)
	if err := validateEvidence(evidence); err != nil {
		t.Fatalf("valid schema-v4 soak evidence rejected: %v", err)
	}
	missing := evidence
	missing.Trials = append([]campaign.Trial(nil), evidence.Trials...)
	missing.Trials[1].Phases = append([]campaign.Phase(nil), evidence.Trials[1].Phases[:2]...)
	if err := validateEvidence(missing); err == nil {
		t.Fatal("passing schema-v4 campaign without final soak accepted")
	}
}

func TestWeightedProfilesRequirePerWindowJourneyEvidence(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	for trialIndex := range evidence.Trials {
		for phaseIndex := range evidence.Trials[trialIndex].Phases {
			run := &evidence.Trials[trialIndex].Phases[phaseIndex].Run
			run.Config.Profiles = []report.ProfileConfig{{Name: "minority"}}
		}
	}
	result, err := evaluateWithDeclaredTestQuality(evidence, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "per-profile journey-window evidence") {
		t.Fatalf("missing weighted-profile window evidence must block positive demand: %+v %v", result, err)
	}
}

func TestCompletedJourneyLatencyCanBlockOtherwiseHealthyRequestMetrics(t *testing.T) {
	evidence := campaign.Result{SchemaVersion: 1, Scenario: "synthetic", Outcome: "PASS", BaseUsers: 10, Repeats: 2, Trials: passingTrials(), ObservationVariability: validVariability()}
	for trialIndex := range evidence.Trials {
		for phaseIndex := range evidence.Trials[trialIndex].Phases {
			run := &evidence.Trials[trialIndex].Phases[phaseIndex].Run
			run.Journeys.P95IID95UpperMS = 150
		}
	}
	result, err := evaluateWithDeclaredTestQuality(evidence, capacitygoal.Goal{PeakUsers: 10})
	if err != nil || result.State != "INSUFFICIENT_EVIDENCE" || !containsFinding(result.Findings, "completed journeys") {
		t.Fatalf("slow completed journey must block an otherwise passing request report: %+v %v", result, err)
	}
}

func passingTrials() []campaign.Trial {
	passed := validStatisticalRun()
	return []campaign.Trial{
		{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: passed}, {Name: "recovery", Run: passed}}},
		{Number: 2, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: passed}, {Name: "recovery", Run: passed}}},
	}
}

func validStatisticalRun() report.Result {
	metrics := report.MetricSet{
		Total: 300, Success: 300, P95MS: 30,
		P95IID95LowerMS: 20, P95IID95UpperMS: 50, P95IID95Approximation: true,
	}
	return report.Result{
		Outcome: "PASS", ObservationSeconds: 10,
		Config:  report.RunConfig{P95LimitMS: 100, MaxErrorRate: 0.01, MinSamplesPerStep: 1, WindowDuration: "10s"},
		Metrics: metrics, Steps: []report.StepResult{{Name: "step", Metrics: metrics}},
		Windows: []report.WindowResult{{StartSeconds: 0, EndSeconds: 10, Metrics: metrics,
			Journeys: metrics, Steps: []report.WindowMetric{{Name: "step", Metrics: metrics}}}},
		SchemaVersion: report.CurrentSchemaVersion,
		Journeys:      metrics,
		Quality:       &report.Quality{State: report.QualityValid},
	}
}

func validVariability() *campaign.Variability {
	return &campaign.Variability{Runs: 2, P95MinMS: 20, P95MaxMS: 30, ErrorRateMin: 0, ErrorRateMax: 0, ThroughputMinRPS: 20, ThroughputMaxRPS: 30}
}

func evaluateWithDeclaredTestQuality(evidence campaign.Result, goal capacitygoal.Goal) (Result, error) {
	return evaluateWithQuality(evidence, goal, func(run report.Result) *report.Quality {
		return run.Quality
	})
}

func containsFinding(findings []string, part string) bool {
	for _, finding := range findings {
		if strings.Contains(finding, part) {
			return true
		}
	}
	return false
}
