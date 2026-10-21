package capacitygoal

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestArrivalCampaignRunGuardrailsMustMatchOfflinePolicy(t *testing.T) {
	policy := generatorhealth.Guardrails{MaxProcessCPUPercent: 80, ConsecutiveSamples: 2}
	saved := arrivalcampaign.Result{SchemaVersion: 3, GeneratorGuardrails: &policy}
	run := report.Result{Generator: &generatorhealth.Summary{Guardrails: &policy}}
	if !arrivalCampaignRunGuardrailsMatch(saved, run) {
		t.Fatal("matching arrival campaign and phase guardrails were rejected")
	}
	run.Generator.Guardrails = nil
	if arrivalCampaignRunGuardrailsMatch(saved, run) {
		t.Fatal("missing phase guardrails were accepted")
	}
}

func passed(users int) report.Result {
	return report.Result{
		SchemaVersion: 3, Outcome: "PASS", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: users, Mode: "closed", MaxRPS: 100, Duration: "30s", SeparateSessions: true},
		Metrics: report.MetricSet{Total: 25, Success: 25},
		Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.17,
			ProcessSamples: 4, ProcessCPUSamples: 3, ProcessCPUValid: true, PeakProcessRSSBytes: 1024},
	}
}

func TestJourneyStartGoalUsesExactRepeatedArrivalRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	base, err := config.Parse(config.Scenario{
		Name: "arrival-goal-loopback", BaseURL: server.URL, Users: 2,
		Mode: "arrival", ArrivalRate: 1, Duration: "10s", MaxRPS: 10,
		Timeout: "2s", P95LimitMS: 5000, MaxErrorRate: 0.1, MinSamplesPerStep: 1,
		Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/", ExpectedStatus: []int{http.StatusOK}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	run, err := arrivalexplorer.Run(context.Background(), base, arrivalexplorer.Options{
		MinJourneysPerSecond: 1, MaxJourneysPerSecond: 1, TargetJourneysPerSecond: 1,
		Repeats: 2, StageDuration: 10 * time.Second, Budget: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := EvaluateJourneyStarts(run, JourneyStartGoal{PeakStartsPerSecond: 1})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" || assessment.MatchedTrials != 2 || assessment.TargetStartsPerSecond != 1 {
		t.Fatalf("LIMITED short trials were upgraded into a positive demand finding: state=%s findings=%v matched=%d err=%v", assessment.State, assessment.Findings, assessment.MatchedTrials, err)
	}
	if strings.Contains(strings.Join(assessment.Findings, " "), "VUs") && !strings.Contains(strings.Join(assessment.Findings, " "), "not") {
		t.Fatal("assessment implied a conversion to VUs")
	}

	tampered := run
	tampered.Probes = append([]arrivalexplorer.Probe(nil), run.Probes...)
	tampered.Probes[0].Runs = append([]report.Result(nil), run.Probes[0].Runs...)
	tampered.Probes[0].Runs[0].Outcome = "FAIL"
	assessment, err = EvaluateJourneyStarts(tampered, JourneyStartGoal{PeakStartsPerSecond: 1})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("an unreproducible saved failure was trusted: assessment=%+v err=%v", assessment, err)
	}
}

func TestJourneyStartGoalDoesNotInterpolateAndRejectsMixedUnits(t *testing.T) {
	saved := arrivalexplorer.Result{
		SchemaVersion: 1, Scenario: "synthetic", StageDuration: "10s", Repeats: 2,
		WorkerLimit: 4, MinJourneysPerSecond: 1, MaxJourneysPerSecond: 3,
		Probes: []arrivalexplorer.Probe{{JourneysPerSecond: 2, Status: "PASS_OBSERVED", Runs: []report.Result{}}},
	}
	assessment, err := EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 3})
	if err != nil || assessment.State != "TARGET_NOT_DIRECTLY_TESTED" {
		t.Fatalf("nearby passing rate was interpolated: %+v err=%v", assessment, err)
	}
	saved.MaxJourneysPerSecond = 2
	assessment, err = EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 2, GrowthPercent: 25, ReservePercent: 20})
	if err != nil || assessment.State != "OUTSIDE_TESTED_CEILING" || assessment.TargetStartsPerSecond != 3 {
		t.Fatalf("arrival demand growth/reserve arithmetic or ceiling was wrong: %+v err=%v", assessment, err)
	}
	for _, goal := range []JourneyStartGoal{{PeakStartsPerSecond: 0}, {PeakStartsPerSecond: 1, GrowthPercent: math.NaN()}, {PeakStartsPerSecond: 1, ReservePercent: 1001}} {
		if _, err := EvaluateJourneyStarts(saved, goal); err == nil {
			t.Fatalf("invalid scheduled-start goal accepted: %+v", goal)
		}
	}
}

func TestJourneyStartGoalAcceptsCalibratedExplorerEnvelopeAndRejectsUnlistedProbe(t *testing.T) {
	saved := arrivalexplorer.Result{
		SchemaVersion: 2, Scenario: "synthetic", StageDuration: "10s", Repeats: 2,
		WorkerLimit: 4, MinJourneysPerSecond: 2, MaxJourneysPerSecond: 10,
		RequestedMinJourneysPerSecond: 1, RequestedMaxJourneysPerSecond: 20,
		CalibratedProbeRates: []int{2, 5, 10},
		Probes:               []arrivalexplorer.Probe{{JourneysPerSecond: 2, Status: "PASS_OBSERVED", Runs: []report.Result{}}},
	}
	assessment, err := EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 5})
	if err != nil || assessment.State != "TARGET_NOT_DIRECTLY_TESTED" {
		t.Fatalf("valid calibrated envelope was rejected or interpolated: %+v err=%v", assessment, err)
	}
	saved.Probes[0].JourneysPerSecond = 3
	if _, err := EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 5}); err == nil {
		t.Fatal("probe outside the recorded calibrated schedule was accepted")
	}
	saved.Probes[0].JourneysPerSecond = 2
	saved.TargetJourneysPerSecond = 7
	if _, err := EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 5}); err == nil {
		t.Fatal("uncalibrated exact target inside requested range was accepted")
	}
}

func TestJourneyStartGoalAcceptsFailedExactTargetMarkerAndReassessesEvidence(t *testing.T) {
	saved := arrivalexplorer.Result{
		SchemaVersion: 1, Scenario: "synthetic", StageDuration: "10s", Repeats: 2,
		WorkerLimit: 4, MinJourneysPerSecond: 1, MaxJourneysPerSecond: 3,
		TargetJourneysPerSecond: 2, TargetTested: true,
		Probes: []arrivalexplorer.Probe{{JourneysPerSecond: 2, Status: "FAIL_OBSERVED", Runs: []report.Result{{SchemaVersion: 3, Outcome: "FAIL"}}}},
	}
	assessment, err := EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 2})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" || assessment.MatchedTrials != 1 {
		t.Fatalf("failed exact target was rejected by its marker instead of reassessed: assessment=%+v err=%v", assessment, err)
	}

	saved.Probes[0] = arrivalexplorer.Probe{JourneysPerSecond: 2, Status: "ABORTED", Reason: "deadline", Runs: []report.Result{}}
	assessment, err = EvaluateJourneyStarts(saved, JourneyStartGoal{PeakStartsPerSecond: 2})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" || assessment.MatchedTrials != 0 {
		t.Fatalf("interrupted exact target was rejected instead of reported as incomplete: assessment=%+v err=%v", assessment, err)
	}
}

func TestArrivalCampaignDemandRequiresExactBaseAndConsistentArtifact(t *testing.T) {
	saved := arrivalcampaign.Result{SchemaVersion: 1, Scenario: "synthetic", Target: "https://app.example.com", StartedAt: time.Now().UTC().Format(time.RFC3339), WorkloadFingerprint: "fingerprint", WorkerLimit: 2,
		RequestTimeout: "2s", RequestRateCeiling: 10, P95LimitMS: 500, MaxErrorRate: .1, MinSamplesPerStep: 1,
		BaseStartsPerSecond: 3, Warmup: "0s", Observation: "30s", Recovery: "10s", Budget: "4m", Repeats: 2,
		Trials: []arrivalcampaign.Trial{{Number: 1, Outcome: "PASS", Phases: []arrivalcampaign.Phase{{Name: "observation"}, {Name: "recovery"}}}}, Outcome: "PASS"}
	assessment, err := EvaluateJourneyCampaign(saved, JourneyStartGoal{PeakStartsPerSecond: 2})
	if err != nil || assessment.State != "TARGET_NOT_DIRECTLY_TESTED" {
		t.Fatalf("campaign interpolated an untested exact rate: %+v err=%v", assessment, err)
	}
	saved.BaseStartsPerSecond = 2
	if _, err := EvaluateJourneyCampaign(saved, JourneyStartGoal{PeakStartsPerSecond: 2}); err == nil {
		t.Fatal("campaign with absent native phase evidence was accepted")
	}
}

func TestCurrentArrivalCampaignRejectsDuplicateTrialRunIDs(t *testing.T) {
	saved := arrivalcampaign.Result{
		SchemaVersion: arrivalcampaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic", Target: "https://app.example.com",
		StartedAt: time.Now().UTC().Format(time.RFC3339), WorkloadFingerprint: "fingerprint", WorkerLimit: 2,
		RequestTimeout: "2s", RequestRateCeiling: 10, P95LimitMS: 500, MaxErrorRate: .1, MinSamplesPerStep: 1,
		WindowDuration: "10s", BaseStartsPerSecond: 2, Warmup: "0s", Observation: "30s", Recovery: "10s",
		Cooldown: "0s", Budget: "4m", Repeats: 2, Outcome: "PASS",
		Trials: []arrivalcampaign.Trial{{Number: 1, RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Number: 2, RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	}
	if _, err := EvaluateJourneyCampaign(saved, JourneyStartGoal{PeakStartsPerSecond: 2}); err == nil || !strings.Contains(err.Error(), "distinct run IDs") {
		t.Fatalf("duplicate trial run IDs accepted: %v", err)
	}
}

func TestArrivalCampaignCheckpointIsAuditedAsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	base, err := config.Parse(config.Scenario{
		Name: "checkpoint", BaseURL: server.URL, Users: 2, Mode: "arrival", ArrivalRate: 1,
		Duration: "10s", WindowDuration: "2s", MaxRPS: 10, Timeout: "2s", P95LimitMS: 1000, MaxErrorRate: .1,
		MinSamplesPerStep: 1, Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	options := arrivalcampaign.Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	var checkpoint arrivalcampaign.Result
	_, err = arrivalcampaign.WithRunnerProgress(context.Background(), base, options, func(_ context.Context, phaseBase config.Validated) report.Result {
		run := report.Result{
			SchemaVersion: report.CurrentSchemaVersion, GeneratorIdentity: report.GeneratorIdentity{OS: "linux", Architecture: "amd64", GoVersion: "go1.24.0", RunnerIdentityMethod: "git_commit", RunnerIdentity: strings.Repeat("a", 40)}, Name: phaseBase.Scenario.Name,
			Target: phaseBase.Base.Scheme + "://" + phaseBase.Base.Host, StartedAt: time.Now().UTC().Format(time.RFC3339), Outcome: "PASS",
			Config: report.RunConfig{Mode: "arrival", ArrivalRate: phaseBase.Scenario.ArrivalRate, Users: phaseBase.Scenario.Users,
				Duration: phaseBase.Scenario.Duration, WorkloadFingerprint: phaseBase.WorkloadFingerprint, MaxRPS: phaseBase.Scenario.MaxRPS,
				P95LimitMS: phaseBase.Scenario.P95LimitMS, MaxErrorRate: phaseBase.Scenario.MaxErrorRate,
				MinSamplesPerStep: phaseBase.Scenario.MinSamplesPerStep, RequestTimeout: phaseBase.Timeout.String(), WindowDuration: phaseBase.Window.String()},
		}
		return run
	}, func(snapshot arrivalcampaign.Result) error {
		if snapshot.ExecutionState == "RUNNING" && snapshot.PendingTrial != nil && len(snapshot.PendingTrial.Phases) > 0 {
			checkpoint = snapshot
		}
		return nil
	})
	if err != nil || checkpoint.ExecutionState != "RUNNING" || checkpoint.PendingTrial == nil {
		t.Fatalf("campaign did not produce an in-progress checkpoint: checkpoint=%+v err=%v", checkpoint, err)
	}
	assessment, err := EvaluateJourneyCampaign(checkpoint, JourneyStartGoal{PeakStartsPerSecond: 1})
	if err != nil || assessment.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("in-progress artifact was not retained as incomplete evidence: assessment=%+v err=%v", assessment, err)
	}
	malformedMetrics := checkpoint
	pendingWithMetrics := *checkpoint.PendingTrial
	pendingWithMetrics.Phases = append([]arrivalcampaign.Phase(nil), pendingWithMetrics.Phases...)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	invalidTimeline := appmetrics.Timeline{SchemaVersion: appmetrics.TimelineSchemaVersion, Source: "http://127.0.0.1", Interval: "10s",
		StartedAt: stamp, FinishedAt: stamp, Note: "test", Samples: []appmetrics.TimedSample{{StartedAt: stamp, FinishedAt: stamp, DurationMS: -1}}}
	pendingWithMetrics.Phases[0].Run.ApplicationMetrics = &invalidTimeline
	malformedMetrics.PendingTrial = &pendingWithMetrics
	if _, err := EvaluateJourneyCampaign(malformedMetrics, JourneyStartGoal{PeakStartsPerSecond: 1}); err == nil || !strings.Contains(err.Error(), "invalid application-metrics timeline") {
		t.Fatalf("offline arrival-campaign audit accepted malformed timeline evidence: %v", err)
	}
	duplicate := checkpoint
	duplicate.Trials = append([]arrivalcampaign.Trial(nil), checkpoint.Trials...)
	duplicate.Trials = append(duplicate.Trials, arrivalcampaign.Trial{Number: 1, RunID: checkpoint.PendingTrial.RunID})
	duplicatePending := *checkpoint.PendingTrial
	duplicatePending.Number = 2
	duplicate.PendingTrial = &duplicatePending
	if _, err := EvaluateJourneyCampaign(duplicate, JourneyStartGoal{PeakStartsPerSecond: 1}); err == nil || !strings.Contains(err.Error(), "distinct run IDs") {
		t.Fatalf("pending trial reused a previous trial ID: %v", err)
	}
	malformed := checkpoint
	pending := *checkpoint.PendingTrial
	pending.RunID = strings.Repeat("z", 32)
	malformed.PendingTrial = &pending
	if _, err := EvaluateJourneyCampaign(malformed, JourneyStartGoal{PeakStartsPerSecond: 1}); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed pending trial ID was accepted: %v", err)
	}
}

func TestSavedLongRunCampaignRequiresStagingAndExplicitBoundedBudget(t *testing.T) {
	saved := arrivalcampaign.Result{
		SchemaVersion: arrivalcampaign.CurrentSchemaVersion, Scenario: "synthetic", Target: "https://app.example.com", Cooldown: "0s",
		DeclaredEnvironment: "staging", LongRun: true, BaseStartsPerSecond: 1, RequestRateCeiling: 100,
		RequestTimeout: "2s", Warmup: "0s", Observation: "30m", Recovery: "45s", Budget: "70m", Repeats: 2,
	}
	if _, err := savedOptions(saved); err != nil {
		t.Fatalf("valid saved staging campaign rejected: %v", err)
	}
	saved.DeclaredEnvironment = "production"
	if _, err := savedOptions(saved); err == nil {
		t.Fatal("offline reader accepted a long-run campaign without a staging declaration")
	}
	saved.DeclaredEnvironment = "staging"
	saved.Budget = (arrivalcampaign.LongRunWallTimeLimit + time.Minute).String()
	if _, err := savedOptions(saved); err == nil {
		t.Fatal("offline reader accepted a campaign above the 2h wall-time ceiling")
	}
}

func TestArrivalCampaignFailureIsNotSLOMissWithoutValidGeneratorEvidence(t *testing.T) {
	run := report.Result{Outcome: "FAIL", Config: report.RunConfig{P95LimitMS: 100, MaxErrorRate: .1},
		Metrics: report.MetricSet{Total: 100, Success: 90, Failures: 10, P95MS: 500}}
	if campaignHasAdequateSLOFailure(run, "FAIL_OBSERVED", true) {
		t.Fatal("a failing native outcome without valid measurement and generator evidence was called an application SLO miss")
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
