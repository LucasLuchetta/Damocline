package capacityplan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// auditFixture deliberately models a passing synthetic report with LIMITED
// measurement quality. No test helper can manufacture remote generator proof.
func auditFixture() campaign.Result {
	return auditFixtureAtUsers(2)
}

func auditFixtureAtUsers(users int) campaign.Result {
	stabilityPolicy := campaign.DefaultStabilityPolicy()
	zero := int64(0)
	phaseRun := func() report.Result {
		metricSet := func(total int) report.MetricSet {
			return report.MetricSet{
				Total: total, Success: total, P50MS: 100, P95MS: 100, P99MS: 100,
				HTTP:         report.HTTPDiagnostics{FailureClassificationVersion: 1, StatusCodes: map[int]int{200: total}, Status2xx: total},
				ResponseBody: report.ResponseBodyDiagnostics{CompleteObservations: int64(total), MinBytes: 1, MaxBytes: 1, TotalBytes: int64(total)},
			}
		}
		metrics := metricSet(80)
		metrics.P95IID95Exact, metrics.P95IID95LowerMS, metrics.P95IID95UpperMS = true, 80, 120
		trace := report.HTTPTrace{RateWaitCount: 80, RequestAttemptCount: 80, CompleteCount: 80, FirstByteCount: 80, BodyCount: 80, WriteCount: 80}
		generator := generatorhealth.Summary{Samples: 2, ObservedSeconds: 10, SamplingIntervalMS: 500, MaxSampleGapMS: 500, LogicalCPUs: 1, GOMAXPROCS: 1,
			CPUCapacityCores: 1, CPUCapacitySource: "runtime.NumCPU", CPUCapacityKnown: true, GoCPUCapacitySeconds: 10, GoCPUValid: true, GoCPUUtilizationRatio: 0.1,
			ProcessSamples: 2, ProcessCPUSamples: 2, ProcessCPUValid: true, PeakProcessRSSBytes: 1, PeakHeapAllocBytes: 1, PeakGoroutines: 1, MeanProcessCPUUtilizationRatio: 0.1}
		r := report.Result{
			SchemaVersion: report.CurrentSchemaVersion, Outcome: "PASS", Target: "http://127.0.0.1:18080", IterationsStarted: 80, IterationsCompleted: 80,
			ElapsedSeconds: 10, ObservationSeconds: 10,
			Config: report.RunConfig{Users: users, Duration: "10s", WindowDuration: "2.5s", MaxRPS: 40, P95LimitMS: 1000, MaxErrorRate: 0.1,
				Mode: "closed", ConnectionPolicy: "reuse", MinSamplesPerStep: 1, WorkloadFingerprint: "sha256:" + strings.Repeat("a", 64),
				WorkloadRevision: "workload-v1", ApplicationRevision: "app-v1", Environment: "synthetic", MaxConnectionsPerHost: 10},
			StatisticalMethods: report.CurrentStatisticalMethods(), Metrics: metrics, Journeys: metrics,
			ArrivalsAdmitted: &zero, ArrivalsCancelledBeforeAdmission: &zero, Generator: &generator,
			GeneratorIdentity: report.GeneratorIdentity{OS: "windows", Architecture: "amd64", GoVersion: "go1.26.0", RunnerIdentityMethod: "git_commit", RunnerIdentity: strings.Repeat("a", 40)},
			Steps:             []report.StepResult{{Name: "request", Method: "GET", Path: "/", Metrics: metrics, HTTPTrace: trace}},
		}
		for i := 0; i < 4; i++ {
			windowStepMetrics := metricSet(20)
			windowStepMetrics.P95IID95Exact, windowStepMetrics.P95IID95LowerMS, windowStepMetrics.P95IID95UpperMS = true, 80, 120
			windowMetrics := metricSet(20)
			windowMetrics.P95IID95Exact, windowMetrics.P95IID95LowerMS, windowMetrics.P95IID95UpperMS = true, 80, 120
			r.Windows = append(r.Windows, report.WindowResult{StartSeconds: float64(i) * 2.5, EndSeconds: float64(i+1) * 2.5,
				Metrics: windowMetrics, Journeys: windowMetrics,
				Steps: []report.WindowMetric{{Name: "request", Metrics: windowStepMetrics}}})
		}
		r.Quality = report.AssessQuality(r)
		return r
	}
	a, b := phaseRun(), phaseRun()
	e := campaign.Result{
		SchemaVersion: campaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic", Target: a.Target, StartedAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano),
		BaseUsers: users, Warmup: "0s", Stabilization: "0s", Observation: "10s", SpikeDuration: "0s", Recovery: "10s", Cooldown: "0s",
		Budget: "3m20s", Repeats: 2, Outcome: "PASS", Soak: "10s", StabilityPolicy: &stabilityPolicy,
		Objectives: &campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 1000, MaxJourneyErrorRate: 0.1,
			MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MaxDroppedArrivals: 0, MinimumObservationWindows: stabilityPolicy.MinimumWindows,
			MinimumSamplesPerWindow: stabilityPolicy.MinimumSamplesPerWindow, WindowDuration: "2.5s"},
	}
	for n := 1; n <= 2; n++ {
		phases := []campaign.Phase{
			{Name: "observation", EvidenceState: "PASS_OBSERVED", Stability: campaign.AssessStability(a), Run: a},
			{Name: "recovery", EvidenceState: "PASS_OBSERVED", Run: b},
		}
		if n == 2 {
			phases = append(phases, campaign.Phase{Name: "soak", EvidenceState: "PASS_OBSERVED", Stability: campaign.AssessStability(a), Run: a})
		}
		runID := strings.Repeat("a", 32)
		if n == 2 {
			runID = strings.Repeat("b", 32)
		}
		e.Trials = append(e.Trials, campaign.Trial{RunID: runID, Number: n, Outcome: "PASS", Phases: phases})
	}
	e.ObservationVariability = &campaign.Variability{
		Runs: 2, P95MinMS: 100, P95MaxMS: 100, ErrorRateMin: 0, ErrorRateMax: 0,
		ThroughputMinRPS: 8, ThroughputMaxRPS: 8,
		SuccessfulJourneysMinPerSec: 8, SuccessfulJourneysMaxPerSec: 8,
		RequestP95MedianMS: 100, RequestP95RobustLowerMS: 100, RequestP95RobustUpperMS: 100,
		RequestP95RobustMethod: "MEDIAN_PLUS_MINUS_3_MAD", RequestP95RobustAssumption: "INDEPENDENT_OBSERVATION_PHASES_DESCRIPTIVE_ONLY",
	}
	return e
}

func TestAuditRecomputesIndependentObservationSummaries(t *testing.T) {
	e := auditFixture()
	a := AuditCampaign(e)
	if a.Repeatability.Status != "CONDITIONAL_INTERVAL" || a.Repeatability.EvaluatedTrials != 2 || a.Repeatability.PassingTrials != 2 || a.Repeatability.ExcludedTrials != 0 || a.Repeatability.Lower95 < 0.15 || a.Repeatability.Lower95 > 0.16 || a.Repeatability.Upper95 != 1 {
		t.Fatalf("repeat pass uncertainty was not derived from terminal trial outcomes: %+v", a.Repeatability)
	}
	if a.SpreadConsistency != "MATCHED" || len(a.ObservationTrials) != 2 || a.ObservationSpread == nil || a.ObservationSpread.Runs != 2 {
		t.Fatalf("saved observation summary was not recomputed: %+v", a)
	}
	first := a.ObservationTrials[0]
	if first.Requests != 80 || first.RequestP95MS != 100 || first.CompletedJourneys != 80 || first.JourneyP95MS != 100 || first.SuccessfulJourneysPerSec != 8 {
		t.Fatalf("observation trial metrics were not derived from saved phase report: %+v", first)
	}
	e.ObservationVariability.P95MaxMS = 101
	a = AuditCampaign(e)
	if a.State != "INVALID_EVIDENCE" || a.SpreadConsistency != "MISMATCHED" {
		t.Fatalf("altered persisted observation summary was trusted: %+v", a)
	}
	e = auditFixture()
	e.ObservationVariability = nil
	a = AuditCampaign(e)
	if a.State == "INVALID_EVIDENCE" || a.SpreadConsistency != "NOT_RECORDED" || len(a.ObservationTrials) != 2 {
		t.Fatalf("missing legacy summary should retain recomputable observations: state=%s findings=%v", a.State, a.Findings)
	}
}

func TestAuditPreservesScenarioIdentityAndRequiresConsistency(t *testing.T) {
	e := auditFixture()
	a := AuditCampaign(e)
	if a.IdentityStatus != "CONSISTENT" || a.WorkloadRevision != "workload-v1" || a.ApplicationRevision != "app-v1" || a.DeclaredEnvironment != "synthetic" {
		t.Fatalf("scenario identity was not summarized from required phase evidence: %+v", a)
	}
	e.Trials[1].Phases[0].Run.Config.ApplicationRevision = "app-v2"
	a = AuditCampaign(e)
	if a.State != "INCONCLUSIVE" || a.IdentityStatus != "CHANGED" || a.ApplicationRevision != "" {
		t.Fatalf("campaign with changed application identity was not kept inconclusive: state=%s identity=%s revision=%q", a.State, a.IdentityStatus, a.ApplicationRevision)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].Run.Config.Environment = ""
	a = AuditCampaign(e)
	if a.State != "INCONCLUSIVE" || a.IdentityStatus != "UNKNOWN" {
		t.Fatalf("missing phase identity was not kept unknown: state=%s identity=%s", a.State, a.IdentityStatus)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].Run.Config.Environment = ""
	e.Trials[1].Phases[0].Run.Config.ApplicationRevision = "app-v2"
	a = AuditCampaign(e)
	if a.IdentityStatus != "CHANGED" || a.ApplicationRevision != "" || a.DeclaredEnvironment != "" || !strings.Contains(strings.Join(a.Findings, " "), "also lack") {
		t.Fatalf("identity change was hidden by missing metadata: status=%s application=%q environment=%q", a.IdentityStatus, a.ApplicationRevision, a.DeclaredEnvironment)
	}
}

func TestAuditRejectsUnsupportedEmbeddedRunReportSchemas(t *testing.T) {
	for _, schema := range []int{0, report.CurrentSchemaVersion + 1} {
		t.Run(fmt.Sprintf("schema_%d", schema), func(t *testing.T) {
			e := auditFixture()
			e.Trials[0].Phases[0].Run.SchemaVersion = schema
			a := AuditCampaign(e)
			if a.State != "INVALID_EVIDENCE" {
				t.Fatalf("unsupported embedded run schema %d was accepted: state=%s findings=%v", schema, a.State, a.Findings)
			}
		})
	}
}

func TestHistoricalPassCannotOverrideRecomputedJourneySLOFailure(t *testing.T) {
	evidence := auditFixture()
	evidence.SchemaVersion = 2
	evidence.ExecutionState = ""
	evidence.Stabilization = ""
	evidence.Soak = ""
	evidence.Cooldown = ""
	evidence.StabilityPolicy = nil
	evidence.Objectives = nil
	evidence.CalibrationRequired = false
	evidence.Calibration = nil
	evidence.CalibrationSource = ""
	for trialIndex := range evidence.Trials {
		trial := &evidence.Trials[trialIndex]
		trial.RunID = ""
		trial.Phases = append([]campaign.Phase(nil), trial.Phases...)
		if trialIndex == 1 {
			trial.Phases = trial.Phases[:2]
		}
		for phaseIndex := range trial.Phases {
			phase := &trial.Phases[phaseIndex]
			for _, metrics := range []*report.MetricSet{&phase.Run.Journeys} {
				metrics.Failures = metrics.Total / 5
				metrics.Success = metrics.Total - metrics.Failures
			}
			for windowIndex := range phase.Run.Windows {
				metrics := &phase.Run.Windows[windowIndex].Journeys
				metrics.Failures = metrics.Total / 5
				metrics.Success = metrics.Total - metrics.Failures
			}
			phase.Run.Quality = report.AssessQuality(phase.Run)
		}
	}

	if err := ValidateSavedCampaign(evidence); err != nil {
		t.Fatalf("historical campaign structure rejected before recomputed audit: %v", err)
	}
	audit := AuditCampaign(evidence)
	if audit.State != "INCONCLUSIVE" || !containsFinding(audit.Findings, "historical PASS_OBSERVED classification conflicts with the current recomputed FAIL_OBSERVED result") {
		t.Fatalf("historical PASS overrode current journey SLO evidence: state=%s findings=%v", audit.State, audit.Findings)
	}
}

func TestAuditRecomputesRobustSpreadFromTrialP95WithoutUpgradingVerdict(t *testing.T) {
	e := auditFixture()
	template := e.Trials[0]
	e.Trials = make([]campaign.Trial, 5)
	for i := range e.Trials {
		e.Trials[i] = template
		e.Trials[i].Phases = append([]campaign.Phase(nil), template.Phases...)
	}
	for trial, p95 := range []float64{100, 110, 120, 130, 200} {
		e.Trials[trial].Number = trial + 1
		e.Trials[trial].RunID = strings.Repeat(string(rune('a'+trial)), 32)
		for phaseIndex := range e.Trials[trial].Phases {
			run := &e.Trials[trial].Phases[phaseIndex].Run
			if e.Trials[trial].Phases[phaseIndex].Name != "observation" {
				continue
			}
			run.Metrics.P95MS = p95
			run.Metrics.P95IID95Exact = false
			run.Metrics.P95IID95LowerMS = 0
			run.Metrics.P95IID95UpperMS = 0
			run.Quality = report.AssessQuality(*run)
		}
	}
	e.ObservationVariability = nil
	a := AuditCampaign(e)
	if a.ObservationSpread == nil || a.ObservationSpread.RequestP95MedianMS != 120 || a.ObservationSpread.RequestP95MADMS != 10 || a.ObservationSpread.RequestP95RobustLowerMS != 90 || a.ObservationSpread.RequestP95RobustUpperMS != 150 {
		t.Fatalf("robust repeat spread was not recomputed from independent trial summaries: %+v", a.ObservationSpread)
	}
	if a.Repeatability.EvaluatedTrials != 5 || a.Repeatability.Method != "EXACT_CLOPPER_PEARSON_BINOMIAL" || a.Repeatability.Assumption != "INDEPENDENT_STATIONARY_REPEAT_TRIALS" {
		t.Fatalf("descriptive spread changed the conditional pass interval: repeat=%+v", a.Repeatability)
	}
}

func TestAuditMatchesPersistedRobustSpreadForCurrentCampaignSchema(t *testing.T) {
	e := auditFixture()
	recomputed := AuditCampaign(e)
	if recomputed.ObservationSpread == nil || recomputed.ObservationSpread.RequestP95RobustMethod == "" {
		t.Fatalf("fixture did not produce a current-schema robust summary: %+v", recomputed.ObservationSpread)
	}
	e.ObservationVariability = recomputed.ObservationSpread
	a := AuditCampaign(e)
	if a.SpreadConsistency != "MATCHED" || a.State == "INVALID_EVIDENCE" {
		t.Fatalf("valid persisted schema-v14 robust spread was rejected: consistency=%s state=%s spread=%+v", a.SpreadConsistency, a.State, a.ObservationSpread)
	}
}

func TestCurrentCampaignRequiresPhaseControlsToMatchPredeclaredObjectives(t *testing.T) {
	evidence := auditFixture()
	if err := validateEvidence(evidence); err != nil {
		state, reason := campaign.ClassifyPhase(evidence, evidence.Trials[0].Phases[0].Run, "observation")
		t.Fatalf("valid current objective contract was rejected: %v classification=%s/%s", err, state, reason)
	}
	evidence.Trials[0].Phases[0].Run.Config.P95LimitMS++
	if err := validateEvidence(evidence); err == nil || !strings.Contains(err.Error(), "do not match predeclared objectives") {
		t.Fatalf("phase SLO drift from the predeclared objective was accepted: %v", err)
	}
	evidence = auditFixture()
	evidence.Objectives = nil
	if err := validateEvidence(evidence); err == nil || !strings.Contains(err.Error(), "objective contract is missing") {
		t.Fatalf("current campaign without predeclared objectives was accepted: %v", err)
	}
}

func TestRepeatabilityIntervalExcludesIncompleteTrials(t *testing.T) {
	e := auditFixture()
	e.Trials[1].Outcome = "ABORTED"
	repeatability := summarizeRepeatability(e.Trials)
	if repeatability.Status != "CONDITIONAL_INTERVAL" || repeatability.EvaluatedTrials != 1 || repeatability.PassingTrials != 1 || repeatability.ExcludedTrials != 1 || repeatability.Lower95 > .03 || repeatability.Upper95 != 1 {
		t.Fatalf("incomplete trial was counted as a pass/fail result: %+v", repeatability)
	}
	repeatability = summarizeRepeatability([]campaign.Trial{{Outcome: "IN_PROGRESS"}})
	if repeatability.Status != "INSUFFICIENT_EVIDENCE" || repeatability.EvaluatedTrials != 0 || repeatability.ExcludedTrials != 1 {
		t.Fatalf("pending trial should not receive a confidence interval: %+v", repeatability)
	}
}

func TestAuditCampaignObservationalPassDoesNotCertifyCapacity(t *testing.T) {
	e := auditFixture()
	a := AuditCampaign(e)
	if a.State != "OBSERVED_PASS_ONLY" || a.MeasurementQuality != report.QualityLimited || a.RequiredPhases != 5 {
		t.Fatalf("unexpected conservative verdict: %+v", a)
	}
	if !strings.Contains(strings.Join(a.Findings, " "), "not a validated capacity ceiling") {
		t.Fatalf("missing explicit certification limitation: %+v", a)
	}
}

func TestRunningCampaignCheckpointRemainsAuditableButInconclusive(t *testing.T) {
	evidence := auditFixture()
	pending := evidence.Trials[1]
	pending.Phases = pending.Phases[:1]
	pending.Outcome = "IN_PROGRESS"
	evidence.Trials = evidence.Trials[:1]
	evidence.PendingTrial = &pending
	evidence.ExecutionState = "RUNNING"
	evidence.Outcome = "INCONCLUSIVE"
	if err := validateEvidence(evidence); err != nil {
		t.Fatalf("valid in-progress checkpoint was rejected: %v", err)
	}
	audit := AuditCampaign(evidence)
	if audit.State != "INCONCLUSIVE" || audit.ExecutionState != "RUNNING" || audit.ObservedOutcome != "INCONCLUSIVE" || audit.RequiredPhases != 3 || audit.EvidenceID != evidenceDigest(evidence) {
		t.Fatalf("checkpoint did not preserve partial evidence without a positive verdict: %+v", audit)
	}
	if !strings.Contains(strings.Join(audit.Findings, " "), "final campaign and pending-trial outcomes are unknown") {
		t.Fatalf("checkpoint limitation was not reported: %+v", audit.Findings)
	}
	evidence.PendingTrial.RunID = evidence.Trials[0].RunID
	if err := validateEvidence(evidence); err == nil || !strings.Contains(err.Error(), "distinct valid run IDs") {
		t.Fatalf("checkpoint accepted a duplicate pending trial ID: %v", err)
	}
}

func TestSchemaEightRequiresRecoveryAfterObservedSpikeFailure(t *testing.T) {
	evidence := auditFixture()
	evidence.SchemaVersion = 8
	evidence.Stabilization = ""
	evidence.Cooldown = ""
	evidence.Outcome = "FAIL"
	evidence.SpikeUsers = 4
	evidence.SpikeDuration = "10s"
	evidence.Soak = ""
	options := campaign.Options{Observation: 10 * time.Second, SpikeUsers: 4, SpikeDuration: 10 * time.Second,
		Recovery: 10 * time.Second, Repeats: 2}
	evidence.Budget = campaign.PlannedBudget(options).String()
	trial := campaign.Trial{Number: 1, Outcome: "FAIL"}
	observationRun := evidence.Trials[0].Phases[0].Run
	observationRun.Config.Duration = "10s"
	observationState, _ := campaign.ClassifyEvidence(observationRun)
	observation := campaign.Phase{Name: "observation", Run: observationRun,
		EvidenceState: observationState, Stability: campaign.AssessStability(observationRun)}
	spikeRun := observationRun
	spikeRun.Config.Users, spikeRun.Outcome = 4, "FAIL"
	spikeState, _ := campaign.ClassifyEvidence(spikeRun)
	spike := campaign.Phase{Name: "spike", Run: spikeRun, EvidenceState: spikeState}
	recoveryRun := observationRun
	recoveryState, _ := campaign.ClassifyEvidence(recoveryRun)
	recovery := campaign.Phase{Name: "recovery", Run: recoveryRun, EvidenceState: recoveryState}
	trial.Phases = []campaign.Phase{observation, spike, recovery}
	evidence.Trials = []campaign.Trial{trial}
	if err := validatePartialStageEvidence(evidence); err != nil {
		t.Fatalf("failed spike with passing recovery should remain a valid failed trial: %v", err)
	}
	evidence.Trials[0].Phases = evidence.Trials[0].Phases[:2]
	if err := validatePartialStageEvidence(evidence); err == nil || !strings.Contains(err.Error(), "missing its recovery observation") {
		t.Fatalf("failed spike without recovery was accepted: %v", err)
	}
}

func TestAssessmentBindsDigestsAndOptionalDemand(t *testing.T) {
	e := auditFixture()
	first, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Audit.EvidenceID == "" || first.Audit.EvidenceID != second.Audit.EvidenceID || first.Audit.Phases[0].EvidenceID != second.Audit.Phases[0].EvidenceID {
		t.Fatalf("evidence digests must be stable and present: %+v", first.Audit)
	}
	if first.Audit.TestedDuration != "50s" || first.Audit.Confidence != "NOT_ESTABLISHED" || first.Demand == nil || first.Audit.DemandTargetUsers != 2 {
		t.Fatalf("assessment is missing campaign and demand summary: %+v", first)
	}
	if first.Confidence.Measurement != "LIMITED" || first.Confidence.WorkloadRepresentativeness != "NOT_ASSESSED" || first.Confidence.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("confidence dimensions were collapsed or overstated: %+v", first.Confidence)
	}
	var roundTrip Assessment
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &roundTrip); err != nil || roundTrip.Audit.EvidenceID != first.Audit.EvidenceID || roundTrip.Demand == nil {
		t.Fatalf("assessment JSON lost evidence links: %v %+v", err, roundTrip)
	}
	e.Trials[0].Phases[0].Run.Metrics.Success--
	changed := AuditCampaign(e)
	if changed.EvidenceID == first.Audit.EvidenceID {
		t.Fatal("changed campaign content must produce a different evidence ID")
	}
}

func TestConfidenceDimensionsDoNotCompensateForEachOther(t *testing.T) {
	e := auditFixture()
	for trialIndex := range e.Trials {
		for phaseIndex := range e.Trials[trialIndex].Phases {
			phase := &e.Trials[trialIndex].Phases[phaseIndex]
			phase.Run.Config.ProfileMixBasis = "sanitized_analytics"
			phase.Run.Config.ProfileMixReference = "fixture-week-v1"
		}
	}
	audit := CampaignAudit{State: "OBSERVED_PASS_ONLY", MeasurementQuality: report.QualityValid}
	dimensions := assessConfidence(e, audit)
	if dimensions.Measurement != "CONTROLLED_ENVELOPE_ONLY" || dimensions.WorkloadRepresentativeness != "DECLARED_UNVERIFIED" || dimensions.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("strong measurement evidence upgraded workload or decision confidence: %+v", dimensions)
	}
	e.Trials[1].Phases[1].Run.Config.ProfileMixReference = "different-reference"
	dimensions = assessConfidence(e, audit)
	if dimensions.WorkloadRepresentativeness != "INCONSISTENT_DECLARATION" || dimensions.Decision != "NOT_ESTABLISHED" {
		t.Fatalf("conflicting declarations were treated as verified: %+v", dimensions)
	}
	audit.MeasurementQuality = report.QualityLimited
	dimensions = assessConfidence(e, audit)
	if dimensions.Measurement != "LIMITED" || dimensions.WorkloadRepresentativeness != "INCONSISTENT_DECLARATION" {
		t.Fatalf("limited measurement state was not kept independent: %+v", dimensions)
	}
}

func TestWorkflowBreachDiagnosisUsesOnlyExceededObjectives(t *testing.T) {
	profiles := []report.ProfileResult{{Name: "checkout", Journeys: report.MetricSet{Total: 10, Success: 8, Failures: 2, P95MS: 900}}}
	breaches := workflowBreaches(2, "observation", 4, profiles, report.MetricSet{}, 1000, 0.1)
	if len(breaches) != 1 || breaches[0].Workflow != "checkout" || breaches[0].Trial != 2 || breaches[0].TestedUsers != 4 || breaches[0].Phase != "observation" || len(breaches[0].Reasons) != 1 || breaches[0].Reasons[0] != "ERROR_RATE_ABOVE_LIMIT" || breaches[0].ErrorRate != 0.2 {
		t.Fatalf("expected traced workflow error-rate breach: %+v", breaches)
	}
	profiles[0].Journeys = report.MetricSet{Total: 100, Success: 99, Failures: 1, P95MS: 900}
	if got := workflowBreaches(1, "observation", 2, profiles, report.MetricSet{}, 1000, 0.1); len(got) != 0 {
		t.Fatalf("an allowed isolated error was mislabeled as a workflow breach: %+v", got)
	}
	profiles[0].Journeys.P95MS = 1200
	got := workflowBreaches(1, "spike", 8, profiles, report.MetricSet{}, 1000, 0.1)
	if len(got) != 1 || len(got[0].Reasons) != 1 || got[0].Reasons[0] != "P95_ABOVE_LIMIT" {
		t.Fatalf("latency-only breach was not identified by phase: %+v", got)
	}
}

func TestAuditCampaignValidatesWorkloadSourceIdentity(t *testing.T) {
	e := auditFixture()
	fingerprint := e.Trials[0].Phases[0].Run.Config.WorkloadFingerprint
	e.WorkloadSource = &campaign.WorkloadSource{Type: "campaign_profile", Name: "checkout", SchemaVersion: 1,
		FileSHA256: "sha256:" + strings.Repeat("b", 64), SemanticFingerprint: fingerprint}
	if err := validateEvidence(e); err != nil {
		t.Fatalf("valid source identity rejected: %v", err)
	}
	if got := AuditCampaign(e); got.State == "INVALID_EVIDENCE" || got.WorkloadSource == nil || got.WorkloadSource.SemanticFingerprint != fingerprint {
		t.Fatalf("valid source identity was not retained by audit: %+v", got)
	}
	cases := []struct {
		name   string
		mutate func(*campaign.WorkloadSource)
	}{
		{"unsupported type", func(s *campaign.WorkloadSource) { s.Type = "other" }},
		{"invalid file digest", func(s *campaign.WorkloadSource) { s.FileSHA256 = "sha256:short" }},
		{"invalid semantic digest", func(s *campaign.WorkloadSource) { s.SemanticFingerprint = "sha256:" + strings.Repeat("c", 64) }},
		{"invalid profile name", func(s *campaign.WorkloadSource) { s.Name = "../profile" }},
		{"invalid profile schema", func(s *campaign.WorkloadSource) { s.SchemaVersion = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := auditFixture()
			source := *e.WorkloadSource
			tc.mutate(&source)
			candidate.WorkloadSource = &source
			if err := validateEvidence(candidate); err == nil {
				t.Fatal("malformed source identity accepted")
			}
			if got := AuditCampaign(candidate); got.State != "INVALID_EVIDENCE" {
				t.Fatalf("malformed source identity was not invalidated: %+v", got)
			}
		})
	}
	legacy := auditFixture()
	legacy.SchemaVersion = 14
	legacy.WorkloadSource = e.WorkloadSource
	if err := validateEvidence(legacy); err == nil {
		t.Fatal("accepted v15 source metadata under a historical schema")
	}
}

func TestAuditCampaignRejectsAlteredQualityAndStageLabels(t *testing.T) {
	e := auditFixture()
	e.Trials[0].Phases[0].Run.Quality = &report.Quality{State: report.QualityValid}
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("self-declared quality promoted evidence: %+v", got)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].EvidenceState = "FAIL_OBSERVED"
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("altered phase classification accepted: %+v", got)
	}
	e = auditFixture()
	e.Trials[0].Phases[0].Run.Windows[1].Metrics.P95MS = 200
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("altered stability evidence accepted: %+v", got)
	}
}

func TestAssessmentPreservesInvalidEvidenceStateWithDemandGoal(t *testing.T) {
	e := auditFixture()
	e.Trials[0].Phases[0].EvidenceState = "ALTERED"
	assessment, err := BuildAssessment(e, &capacitygoal.Goal{PeakUsers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "INVALID_EVIDENCE" || assessment.Audit.State != "INVALID_EVIDENCE" || assessment.Confidence.Measurement != "INVALID" || assessment.Confidence.WorkloadRepresentativeness != "NOT_ASSESSED" || assessment.Confidence.Decision != "INVALID_EVIDENCE" || assessment.Demand == nil || assessment.Demand.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("demand assessment must not mask invalid source evidence: %+v", assessment)
	}
}

func TestAuditCampaignRejectsMissingTrialsOrInvalidDelivery(t *testing.T) {
	e := auditFixture()
	e.Trials = nil
	if got := AuditCampaign(e); got.State != "INVALID_EVIDENCE" {
		t.Fatalf("incomplete passing campaign accepted: %+v", got)
	}
	e = auditFixture()
	e.Outcome = "INCONCLUSIVE"
	e.Trials = e.Trials[:1]
	e.Trials[0].Outcome = "INCONCLUSIVE"
	e.Trials[0].Phases = e.Trials[0].Phases[:1]
	e.Trials[0].Phases[0].Run.ArrivalsDropped = 1
	e.Trials[0].Phases[0].Run.Outcome = "FAIL"
	e.Trials[0].Phases[0].EvidenceState = "INVALID_DELIVERY"
	_, e.ObservationVariability = recomputeObservationEvidence(e)
	if got := AuditCampaign(e); got.State != "INCONCLUSIVE" {
		t.Fatalf("invalid delivery became a server limit: %+v", got)
	}
}
