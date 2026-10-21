package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestCampaignCapacityReportSnapshotAndVerdictSeparation(t *testing.T) {
	policy := campaign.DefaultStabilityPolicy()
	guardrails := generatorhealth.Guardrails{}
	assessment := capacityplan.Assessment{
		SchemaVersion: 3,
		Audit: capacityplan.CampaignAudit{
			SchemaVersion: campaign.CurrentSchemaVersion, EvidenceID: "sha256:campaign-fixture", ScenarioFingerprint: "sha256:workload-fixture",
			Objectives: &campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 500, MaxJourneyErrorRate: .01, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MaxDroppedArrivals: 0, MinimumObservationWindows: 3, MinimumSamplesPerWindow: 20, WindowDuration: "2.5s"},
			Scenario:   "synthetic local fixture", Target: "http://127.0.0.1:18080", StartedAt: "2026-10-19T12:00:00Z",
			DemandTargetUsers: 3, TestedBaseUsers: 2, HostType: "NOT_RECORDED", PassingLevels: []int{2}, FailingLevels: []int{},
			TestedDuration: "20s", Confidence: "NOT_ESTABLISHED", LimitingWorkflows: []string{},
			ObservationTrials: []capacityplan.ObservationTrial{{Trial: 1, Requests: 100, RequestP95MS: 125, RequestP99MS: 300, RequestP99IntervalState: "INSUFFICIENT_SAMPLES", CompletedJourneys: 50, JourneyP95MS: 250, JourneyP99MS: 450, JourneyP99IntervalState: "INSUFFICIENT_SAMPLES", SuccessfulJourneysPerSec: 5}},
			ObservationSpread: &campaign.Variability{Runs: 1, P95MinMS: 125, P95MaxMS: 125, RequestP95MedianMS: 125, RequestP95RobustLowerMS: 125, RequestP95RobustUpperMS: 125, RequestP95RobustMethod: "MEDIAN_PLUS_MINUS_3_MAD"},
			SpreadConsistency: "MATCHED", Repeatability: capacityplan.RepeatabilityEvidence{Status: "CONDITIONAL_INTERVAL", EvaluatedTrials: 2, PassingTrials: 2, Lower95: 0.158, Upper95: 1, Method: "EXACT_CLOPPER_PEARSON_BINOMIAL", Assumption: "INDEPENDENT_STATIONARY_REPEAT_TRIALS"},
			Unknowns: []string{"Physical host identity is not verified."}, State: "OBSERVED_PASS_ONLY", MeasurementQuality: report.QualityLimited, ObservedOutcome: "PASS", RequiredPhases: 2,
			Findings: []string{"Observed phase SLOs passed; sustainable capacity remains unproven."},
			Phases:   []capacityplan.PhaseAudit{{Trial: 1, Name: "observation", EvidenceID: "sha256:phase-fixture", StartedAt: "2026-10-19T12:00:00Z", ObservationSecs: 10, RunFingerprint: "sha256:workload-fixture", EvidenceState: "PASS_OBSERVED", Measurement: report.QualityLimited, Stability: "NO_LARGE_DRIFT_DETECTED"}},
		},
		Confidence:   capacityplan.ConfidenceDimensions{Measurement: "LIMITED", WorkloadRepresentativeness: "NOT_ASSESSED", Decision: "NOT_ESTABLISHED", Findings: []string{"The workload mix is not independently validated."}},
		DemandInputs: &capacitygoal.Goal{PeakUsers: 2, GrowthPercent: 25, ReservePercent: 10},
		Demand:       &capacityplan.Result{State: "INSUFFICIENT_EVIDENCE", TargetUsers: 3, TestedUsers: 2, Findings: []string{"The exact demand target was not tested."}},
		State:        "INSUFFICIENT_EVIDENCE",
	}
	phase := report.Result{Target: "http://127.0.0.1:18080", Metrics: report.MetricSet{Total: 100, P95MS: 125, P99MS: 300}, Journeys: report.MetricSet{Total: 50, P95MS: 250, P99MS: 450}, Config: report.RunConfig{Users: 2, Duration: "10s", WindowDuration: "2.5s", Mode: "closed", MaxRPS: 20, P95LimitMS: 500, MaxErrorRate: .01, MinSamplesPerStep: 1, RequestTimeout: "2s", ConnectionPolicy: "reuse", WorkloadFingerprint: "sha256:workload-fixture"}}
	evidence := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: assessment.Audit.Scenario, Target: assessment.Audit.Target,
		BaseUsers: 2, Warmup: "0s", Stabilization: "0s", Observation: "10s", SpikeUsers: 0, SpikeDuration: "0s", Recovery: "10s", Soak: "0s", Cooldown: "0s",
		Budget: "3m20s", Repeats: 2, Outcome: "PASS", DeclaredEnvironment: "development", StabilityPolicy: &policy, GeneratorGuardrails: &guardrails,
		Objectives: &campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 500, MaxJourneyErrorRate: .01, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MinimumObservationWindows: 3, MinimumSamplesPerWindow: 20, WindowDuration: "2.5s"},
		Trials:     []campaign.Trial{{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: phase}}}},
	}
	got := renderCampaignCapacityReport(assessment, "campaign.json", "capacity.md", evidence)
	if strings.Contains(got, "**Assessment:** OBSERVED_PASS_ONLY") || !strings.Contains(got, "**Assessment:** INSUFFICIENT_EVIDENCE") || !strings.Contains(got, "**Observed campaign:** OBSERVED_PASS_ONLY") {
		t.Fatalf("executive and observed verdicts were conflated: %s", got)
	}
	for _, expected := range []string{"Request p99 interval", "Journey p99 interval", "INSUFFICIENT_SAMPLES", "intervals require at least 1000 observations", "assume IID stationary latency samples", "Repeat-level request p95", "median +/- 3 MAD band", "not a confidence interval"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("capacity report omitted p99 limitation %q", expected)
		}
	}
	path := filepath.Join("testdata", "capacity_report.golden.md")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("capacity Markdown report changed:\n%s", got)
	}
}

func TestCampaignCollectionCapacityReportShowsTestedBoundary(t *testing.T) {
	highest, firstFailing := 4, 8
	assessment := capacityplan.CampaignCollectionAssessment{
		State: "OBSERVED_LEVELS_ONLY", Scenario: "synthetic", PassingLevels: []int{2, 4}, FailingLevels: []int{8},
		HighestPassingLevel: &highest, FirstFailingLevel: &firstFailing, UpperBound: "BRACKETED_BY_TESTED_LEVELS",
		ObservationPattern: "PASS_BELOW_FAIL", Levels: []capacityplan.CampaignLevelAssessment{}, Findings: []string{}, InconclusiveLevels: []int{},
	}
	got := renderCampaignCollectionCapacityReport(assessment, nil, "capacity.md", nil)
	for _, want := range []string{"Highest passing tested level:** 4 active users (tested)", "First failing tested level:** 8 active users (tested)", "Upper bound:** BRACKETED_BY_TESTED_LEVELS"} {
		if !strings.Contains(got, want) {
			t.Fatalf("collection report does not contain %q:\n%s", want, got)
		}
	}
}
