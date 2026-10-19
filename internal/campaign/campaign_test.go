package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func scenario(t *testing.T) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{Name: "local", BaseURL: "http://127.0.0.1:8080", Users: 1, Duration: "10s", MaxRPS: 10, P95LimitMS: 1000, Steps: []config.Step{{Name: "page", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCampaignSeparatesWarmupObservationAndRecovery(t *testing.T) {
	var durations []time.Duration
	result, err := WithRunner(context.Background(), scenario(t), Options{Warmup: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		durations = append(durations, v.Duration)
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(result.Trials) != 2 || len(durations) != 6 || result.ObservationVariability == nil || result.ObservationVariability.Runs != 2 || result.ObservationVariability.SuccessfulJourneysMinPerSec != 8 || result.ObservationVariability.SuccessfulJourneysMaxPerSec != 8 {
		t.Fatalf("unexpected campaign: %+v err=%v durations=%v", result, err, durations)
	}
}

func TestCampaignRequiredCalibrationBlocksBeforeRunner(t *testing.T) {
	calls := 0
	envelope := &calibration.ClosedLoopEnvelope{Status: "BLOCKED", Levels: []calibration.ClosedLoopAssessment{{Users: 1, Status: "BLOCKED", Reason: "no closed-loop profile"}}}
	_, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
		RequireCalibration: true, Calibration: envelope, CalibrationSource: "reference.json"}, func(context.Context, config.Validated) report.Result {
		calls++
		return report.Result{}
	})
	if err == nil || calls != 0 {
		t.Fatalf("required calibration did not block before runner: calls=%d err=%v", calls, err)
	}
}

func TestStabilizationRunsBeforeObservationAndDriftStopsCampaign(t *testing.T) {
	options := Options{Warmup: 10 * time.Second, Stabilization: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	var phases []string
	result, err := WithRunner(context.Background(), scenario(t), options, func(_ context.Context, v config.Validated) report.Result {
		phases = append(phases, v.Duration.String())
		run := stableResult(v)
		if len(phases) == 2 {
			run.Windows[3].Metrics.P95MS = 150
		}
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(phases) != 2 || phases[0] != "10s" || phases[1] != "10s" ||
		len(result.Trials[0].Phases) != 2 || result.Trials[0].Phases[1].Name != "stabilization" || result.Trials[0].Phases[1].Stability != "NON_STATIONARY" {
		t.Fatalf("stabilization did not gate observation: phases=%v result=%+v err=%v", phases, result, err)
	}
}

func TestStabilizationAddsPhaseDrainToBudget(t *testing.T) {
	options := Options{Stabilization: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	if got, want := PlannedBudget(options), 4*time.Minute; got != want {
		t.Fatalf("stabilization drain missing from budget: got %s want %s", got, want)
	}
}

func TestStabilizationCancellationDoesNotStartObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	result, err := WithRunner(ctx, scenario(t), Options{Stabilization: 10 * time.Second, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(ctx context.Context, v config.Validated) report.Result {
		calls++
		cancel()
		<-ctx.Done()
		return report.Result{Outcome: "ABORTED", Config: report.RunConfig{Users: v.Scenario.Users, Duration: v.Duration.String()}, Target: v.Base.Scheme + "://" + v.Base.Host}
	})
	if err != nil || result.Outcome != "ABORTED" || calls != 1 || len(result.Trials) != 1 || len(result.Trials[0].Phases) != 1 || result.Trials[0].Phases[0].Name != "stabilization" {
		t.Fatalf("stabilization cancellation did not stop before observation: calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestCooldownSeparatesTrialsAndPersistsDistinctRunIDs(t *testing.T) {
	cooldown := 25 * time.Millisecond
	var trialStarts []time.Time
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{
		Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2, Cooldown: cooldown,
	}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if calls%2 == 1 {
			trialStarts = append(trialStarts, time.Now())
		}
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || result.Cooldown != cooldown.String() || len(result.Trials) != 2 ||
		result.Trials[0].RunID == "" || result.Trials[0].RunID == result.Trials[1].RunID || len(trialStarts) != 2 || trialStarts[1].Sub(trialStarts[0]) < cooldown {
		t.Fatalf("cooldown or independent trial identity was not preserved: starts=%v result=%+v err=%v", trialStarts, result, err)
	}
}

func TestProgressCheckpointsRetainPendingPhasesAndFinalState(t *testing.T) {
	var snapshots []Result
	runnerCalls := 0
	result, err := WithRunnerProgress(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		runnerCalls++
		return stableResult(v)
	}, func(snapshot Result) error {
		snapshots = append(snapshots, snapshot)
		return nil
	})
	if err != nil || result.Outcome != "PASS" || result.ExecutionState != "COMPLETED" || result.PendingTrial != nil || len(result.Trials) != 2 {
		t.Fatalf("campaign did not finish with a complete durable report: %+v err=%v", result, err)
	}
	if runnerCalls != 4 || len(snapshots) < 7 || snapshots[len(snapshots)-1].ExecutionState != "COMPLETED" {
		t.Fatalf("unexpected progress snapshots: calls=%d count=%d last=%+v", runnerCalls, len(snapshots), snapshots[len(snapshots)-1])
	}
	foundRetainedPhase := false
	for _, snapshot := range snapshots[:len(snapshots)-1] {
		if snapshot.ExecutionState != "RUNNING" || snapshot.Outcome != "INCONCLUSIVE" || snapshot.PendingTrial == nil || snapshot.PendingTrial.Outcome != "IN_PROGRESS" {
			t.Fatalf("running checkpoint overstated completion: %+v", snapshot)
		}
		if len(snapshot.PendingTrial.Phases) == 1 {
			foundRetainedPhase = true
		}
	}
	if !foundRetainedPhase {
		t.Fatal("no checkpoint retained a completed phase from the active trial")
	}
}

func TestProgressPersistenceFailureStopsBeforeStartingPhases(t *testing.T) {
	runnerCalls := 0
	result, err := WithRunnerProgress(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		runnerCalls++
		return stableResult(v)
	}, func(Result) error { return errors.New("disk full") })
	if err == nil || !strings.Contains(err.Error(), "persist campaign checkpoint") || runnerCalls != 0 || result.Outcome != "INCONCLUSIVE" {
		t.Fatalf("checkpoint write failure did not stop traffic: calls=%d result=%+v err=%v", runnerCalls, result, err)
	}
}

func TestCancellationDuringCooldownPersistsAbortedTrial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstTrialDone := make(chan struct{})
	go func() {
		<-firstTrialDone
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	calls := 0
	result, err := WithRunner(ctx, scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 3, Cooldown: 100 * time.Millisecond}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if calls%2 == 0 {
			close(firstTrialDone)
		}
		return stableResult(v)
	})
	if err != nil || result.Outcome != "ABORTED" || len(result.Trials) != 2 || result.Trials[1].Outcome != "ABORTED" || result.Trials[1].AbortReason == "" || len(result.Trials[1].Phases) != 0 || calls != 2 {
		t.Fatalf("cooldown cancellation was not retained as an aborted trial: %+v calls=%d err=%v", result, calls, err)
	}
}

func TestCampaignRunsRecoveryAfterObservedSpikeFailure(t *testing.T) {
	var users []int
	result, err := WithRunner(context.Background(), scenario(t), Options{
		Observation: 10 * time.Second, SpikeUsers: 2, SpikeDuration: 10 * time.Second,
		Recovery: 10 * time.Second, Repeats: 2,
	}, func(_ context.Context, v config.Validated) report.Result {
		users = append(users, v.Scenario.Users)
		run := stableResult(v)
		if v.Scenario.Users == 2 {
			run.Outcome = "FAIL"
		}
		return run
	})
	if err != nil || result.Outcome != "FAIL" || len(users) != 3 || users[0] != 1 || users[1] != 2 || users[2] != 1 {
		t.Fatalf("spike failure did not retain its recovery phase: outcome=%s users=%v err=%v", result.Outcome, users, err)
	}
	phases := result.Trials[0].Phases
	if len(phases) != 3 || phases[0].Name != "observation" || phases[1].Name != "spike" || phases[1].EvidenceState != "FAIL_OBSERVED" || phases[2].Name != "recovery" || phases[2].Run.Outcome != "PASS" {
		t.Fatalf("campaign did not preserve the failed spike and passing recovery evidence: %+v", phases)
	}
}

func TestCampaignRunsStableSoakOnceAfterIndependentTrials(t *testing.T) {
	var phases []string
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		phases = append(phases, v.Duration.String())
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(phases) != 5 || len(result.Trials[1].Phases) != 3 || result.Trials[1].Phases[2].Name != "soak" || result.Trials[1].Phases[2].Stability != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("expected one final stable soak after four repeated phases (durations %v): outcome=%s err=%v", phases, result.Outcome, err)
	}
}

func TestCampaignSkipsSoakWhenAnyRepeatedTrialFails(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if calls == 3 { // the first recovery phase follows one passing observation
			return report.Result{Outcome: "FAIL", Config: report.RunConfig{Users: 1, Duration: v.Duration.String()}, Target: "http://127.0.0.1:8080"}
		}
		return stableResult(v)
	})
	if err != nil || result.Outcome != "FAIL" || len(result.Trials) != 2 || calls != 3 || len(result.Trials[1].Phases) != 1 || result.Trials[1].Phases[0].Name != "observation" {
		t.Fatalf("soak should not run after an earlier required phase fails: outcome=%s trials=%d calls=%d err=%v", result.Outcome, len(result.Trials), calls, err)
	}
}

func TestCampaignSoakDriftPreventsPassing(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		run := stableResult(v)
		if calls == 5 { // final soak follows two passing observation/recovery pairs
			run.Windows[3].Metrics.P95MS = 140
			run.Windows[3].Journeys.P95MS = 140
		}
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(result.Trials) != 2 || result.Trials[1].Phases[2].Name != "soak" || result.Trials[1].Phases[2].Stability != "NON_STATIONARY" {
		t.Fatalf("soak latency drift did not prevent a passing outcome: outcome=%s trials=%+v err=%v", result.Outcome, result.Trials, err)
	}
}

func TestCampaignSoakMustFitBoundedBudget(t *testing.T) {
	for _, soak := range []time.Duration{time.Second, 4 * time.Minute} {
		if err := Validate(Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: soak, Repeats: 2}); err == nil {
			t.Fatalf("unsafe soak duration accepted: %s", soak)
		}
	}
}

func TestCampaignSpikeRequiresRecoveryPhase(t *testing.T) {
	options := Options{Observation: 10 * time.Second, SpikeUsers: 2, SpikeDuration: 10 * time.Second, Repeats: 2}
	if err := Validate(options); err == nil {
		t.Fatal("campaign spike without a recovery phase was accepted")
	}
}

func TestLongRunRequiresStagingDeclarationExplicitApprovalAndBoundedBudget(t *testing.T) {
	options := Options{Observation: 30 * time.Minute, Recovery: 10 * time.Second, Repeats: 2,
		Budget: 70 * time.Minute, LongRun: true, StagingConfirmed: true}
	base := scenario(t)
	base.Scenario.Environment = "staging"
	if err := ValidateForScenario(base, options); err != nil {
		t.Fatalf("valid explicit staging run rejected: %v", err)
	}
	if err := ValidateForScenario(base, Options{Observation: options.Observation, Recovery: options.Recovery, Repeats: 2}); err == nil {
		t.Fatal("30m observation bypassed the default campaign bound")
	}
	for name, candidate := range map[string]Options{
		"approval missing":        {Observation: options.Observation, Recovery: options.Recovery, Repeats: 2, Budget: options.Budget, LongRun: true},
		"explicit budget missing": {Observation: options.Observation, Recovery: options.Recovery, Repeats: 2, LongRun: true, StagingConfirmed: true},
		"budget too large":        {Observation: options.Observation, Recovery: options.Recovery, Repeats: 2, Budget: LongRunWallTimeLimit + time.Minute, LongRun: true, StagingConfirmed: true},
	} {
		if err := Validate(candidate); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	base.Scenario.Environment = "production"
	if err := ValidateForScenario(base, options); err == nil {
		t.Fatal("long run accepted a non-staging environment declaration")
	}
	base.Scenario.Environment = "staging"
	base.Window = time.Second
	if err := ValidateForScenario(base, options); err == nil {
		t.Fatal("long-run phase accepted a report with unbounded window count")
	}
}

func TestCampaignPersistsGeneratorGuardrailsAndAbortReason(t *testing.T) {
	policy := generatorhealth.Guardrails{MaxProcessRSSBytes: 1, ConsecutiveSamples: 2}
	base := scenario(t)
	result, err := WithRunner(context.Background(), base, Options{
		Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2, Guardrails: policy,
	}, func(_ context.Context, v config.Validated) report.Result {
		if v.GeneratorGuardrails != policy {
			t.Fatalf("guardrails were not attached to phase execution: %+v", v.GeneratorGuardrails)
		}
		return report.Result{Outcome: "ABORTED", Generator: &generatorhealth.Summary{
			Guardrails: &policy, GuardrailTrigger: &generatorhealth.GuardrailTrigger{
				Resource: "generator_process_rss", ObservedRSSBytes: 2, LimitRSSBytes: 1,
				ConsecutiveSamples: 2, SampleNumber: 3, ElapsedMS: 250,
			},
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != CurrentSchemaVersion || result.Outcome != "ABORTED" || result.GeneratorGuardrails == nil || *result.GeneratorGuardrails != policy ||
		len(result.Trials) != 1 || !strings.Contains(result.Trials[0].AbortReason, "RSS") {
		t.Fatalf("campaign lost resource abort evidence: %+v", result)
	}
}

func TestCampaignFailsObservationAndStops(t *testing.T) {
	calls := 0
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		calls++
		if v.Duration == 10*time.Second && calls == 1 {
			return report.Result{Outcome: "FAIL"}
		}
		return report.Result{Outcome: "PASS"}
	})
	if err != nil || result.Outcome != "FAIL" || len(result.Trials) != 1 || calls != 1 {
		t.Fatalf("failed observation was not terminal: %+v err=%v calls=%d", result, err, calls)
	}
}

func TestCampaignRunsBoundedSpikeBeforeRecovery(t *testing.T) {
	var users []int
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, SpikeUsers: 3, SpikeDuration: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		users = append(users, v.Scenario.Users)
		return stableResult(v)
	})
	if err != nil || result.Outcome != "PASS" || len(users) != 6 || users[1] != 3 || users[4] != 3 {
		t.Fatalf("spike was not isolated: %+v err=%v users=%v", result, err, users)
	}
}

func TestDefaultCampaignBudgetIncludesBoundedDrain(t *testing.T) {
	options := Options{Warmup: 10 * time.Second, Observation: 20 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	if got := PlannedBudget(options); got != 4*time.Minute+20*time.Second {
		t.Fatalf("default wall-time cap omitted phase drains: %s", got)
	}
	result, err := WithRunner(context.Background(), scenario(t), options, func(_ context.Context, v config.Validated) report.Result {
		return stableResult(v)
	})
	if err != nil || result.Budget != "4m20s" || result.SchemaVersion != CurrentSchemaVersion || result.StabilityPolicy == nil || *result.StabilityPolicy != DefaultStabilityPolicy() {
		t.Fatalf("effective cap or schema missing: %+v %v", result, err)
	}
	options.Observation = 7 * time.Minute
	if err := Validate(options); err == nil {
		t.Fatal("default cap above 15m accepted")
	}
}

func TestStabilityPolicySchemaCompatibility(t *testing.T) {
	current := Result{SchemaVersion: CurrentSchemaVersion, StabilityPolicy: ptrStabilityPolicy(DefaultStabilityPolicy())}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.StabilityPolicy == nil || *decoded.StabilityPolicy != DefaultStabilityPolicy() {
		t.Fatalf("schema-v5 stability policy failed JSON round trip: %s", encoded)
	}

	historical := Result{SchemaVersion: 4}
	encoded, err = json.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"schema_version":4,"scenario":"","base_users":0,"target":"","started_at":"","warmup":"","observation":"","recovery":"","repeats":0,"trials":null,"outcome":"","notes":null}` {
		t.Fatalf("historical campaign unexpectedly gained a stability policy: %s", encoded)
	}
	if got := historical.EffectiveStabilityPolicy(); got != DefaultStabilityPolicy() {
		t.Fatalf("historical campaign lost default screen: %+v", got)
	}
}

func ptrStabilityPolicy(policy StabilityPolicy) *StabilityPolicy { return &policy }

func TestSoakBudgetIncludesOneFinalPhaseDrain(t *testing.T) {
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Soak: 30 * time.Second, Repeats: 2}
	if got, want := PlannedBudget(options), 3*time.Minute+40*time.Second; got != want {
		t.Fatalf("final soak drain missing from wall-time cap: got %s want %s", got, want)
	}
	options.Budget = 2*time.Minute + 30*time.Second
	if err := Validate(options); err == nil {
		t.Fatal("explicit budget without final soak drain was accepted")
	}
}

func stableResult(v config.Validated) report.Result {
	run := report.Result{Outcome: "PASS", ElapsedSeconds: v.Duration.Seconds(), ObservationSeconds: v.Duration.Seconds(), Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100}, Journeys: report.MetricSet{Total: 80, Success: 80, P95MS: 100}, Config: report.RunConfig{Users: v.Scenario.Users, Duration: v.Duration.String(), WindowDuration: v.Window.String()}, Target: v.Base.Scheme + "://" + v.Base.Host}
	for i := 0; i < 4; i++ {
		run.Windows = append(run.Windows, report.WindowResult{StartSeconds: float64(i) * v.Duration.Seconds() / 4, EndSeconds: float64(i+1) * v.Duration.Seconds() / 4, Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}, Journeys: report.MetricSet{Total: 20, Success: 20, P95MS: 100}})
	}
	return run
}

func TestCampaignRejectsDriftingObservation(t *testing.T) {
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}, func(_ context.Context, v config.Validated) report.Result {
		run := stableResult(v)
		run.Windows[3].Metrics.P95MS = 140
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || result.Trials[0].Phases[0].Stability != "NON_STATIONARY" {
		t.Fatalf("latency drift was not reported: %+v %v", result, err)
	}
}

func TestStabilityScreenDetectsThroughputLossAndMissingCoverage(t *testing.T) {
	run := stableResult(scenario(t))
	if got := AssessStability(run); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("stable fixture: %s", got)
	}
	run.Windows[3].Metrics = report.MetricSet{Total: 20, Success: 20, P95MS: 100}
	run.Windows[0].Metrics = report.MetricSet{Total: 40, Success: 40, P95MS: 100}
	if got := AssessStability(run); got != "NON_STATIONARY" {
		t.Fatalf("throughput loss: %s", got)
	}
	run.Windows[3].StartSeconds += 0.5
	if got := AssessStability(run); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("gapped windows: %s", got)
	}
}

func TestStabilityPolicyConfiguresLatencyAndWindowEvidence(t *testing.T) {
	run := stableResult(scenario(t))
	run.Windows[3].Metrics.P95MS = 115
	if got := AssessStability(run); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("default policy unexpectedly changed: %s", got)
	}
	policy := DefaultStabilityPolicy()
	policy.MaxP95IncreasePercent = 10
	if got := AssessStabilityWithPolicy(run, policy); got != "NON_STATIONARY" {
		t.Fatalf("configured tighter latency threshold was ignored: %s", got)
	}
	policy.MaxP95IncreaseMS = 20
	if got := AssessStabilityWithPolicy(run, policy); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("configured absolute latency floor was ignored: %s", got)
	}
	policy = DefaultStabilityPolicy()
	policy.MinimumWindows = 5
	if got := AssessStabilityWithPolicy(run, policy); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("configured minimum window count was ignored: %s", got)
	}
	rateRun := stableResult(scenario(t))
	rateRun.Windows[0].Metrics.Total, rateRun.Windows[0].Metrics.Success = 40, 40
	for i := 1; i < len(rateRun.Windows); i++ {
		rateRun.Windows[i].Metrics.Total, rateRun.Windows[i].Metrics.Success = 35, 35
	}
	if got := AssessStability(rateRun); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("default throughput threshold unexpectedly changed: %s", got)
	}
	policy = DefaultStabilityPolicy()
	policy.MaxThroughputDropPercent = 10
	if got := AssessStabilityWithPolicy(rateRun, policy); got != "NON_STATIONARY" {
		t.Fatalf("configured throughput threshold was ignored: %s", got)
	}
}

func TestCampaignPersistsConfiguredStabilityAndStopsOnItsThreshold(t *testing.T) {
	policy := DefaultStabilityPolicy()
	policy.MaxP95IncreasePercent = 10
	result, err := WithRunner(context.Background(), scenario(t), Options{Observation: 10 * time.Second, Repeats: 2, Stability: policy}, func(_ context.Context, v config.Validated) report.Result {
		run := stableResult(v)
		run.Windows[3].Metrics.P95MS = 115
		return run
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || result.StabilityPolicy == nil || *result.StabilityPolicy != policy || len(result.Trials) != 1 || result.Trials[0].Phases[0].Stability != "NON_STATIONARY" {
		t.Fatalf("campaign did not apply and persist configured drift limits: %+v %v", result, err)
	}
}

func TestStabilityPolicyValidationIsBounded(t *testing.T) {
	base := DefaultStabilityPolicy()
	for name, mutate := range map[string]func(*StabilityPolicy){
		"too few windows":      func(p *StabilityPolicy) { p.MinimumWindows = 2 },
		"too many windows":     func(p *StabilityPolicy) { p.MinimumWindows = 21 },
		"too few samples":      func(p *StabilityPolicy) { p.MinimumSamplesPerWindow = 19 },
		"negative p95 floor":   func(p *StabilityPolicy) { p.MaxP95IncreaseMS = -1 },
		"non-finite threshold": func(p *StabilityPolicy) { p.MaxP95IncreasePercent = math.Inf(1) },
		"zero throughput drop": func(p *StabilityPolicy) { p.MaxThroughputDropPercent = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			policy := base
			mutate(&policy)
			if err := ValidateStabilityPolicy(policy); err == nil {
				t.Fatalf("invalid stability policy accepted: %+v", policy)
			}
		})
	}
}

func TestValidateForScenarioRequiresEnoughDeclaredWindows(t *testing.T) {
	base := scenario(t)
	options := Options{Observation: 10 * time.Second, Repeats: 2}
	if err := ValidateForScenario(base, options); err != nil {
		t.Fatalf("default generated window coverage rejected: %v", err)
	}
	options.Stability = DefaultStabilityPolicy()
	options.Stability.MinimumWindows = 5
	if err := ValidateForScenario(base, options); err == nil {
		t.Fatal("unachievable minimum window count accepted before traffic")
	}
}

func TestNativeEngineCampaignRecordsStableLoopbackWindows(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(20 * time.Millisecond)
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	v, err := config.Parse(config.Scenario{
		Name: "bounded local campaign", BaseURL: server.URL, Users: 1, Duration: "10s", MaxRPS: 100,
		P95LimitMS: 1000, MaxErrorRate: 0.1, MinSamplesPerStep: 20,
		Steps: []config.Step{{Name: "read", Path: "/", ExpectedStatus: []int{204}}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), v, Options{Observation: 10 * time.Second, Repeats: 2})
	if err != nil || result.Outcome != "PASS" || len(result.Trials) != 2 || received.Load() < 200 {
		t.Fatalf("bounded local campaign failed: outcome=%s trials=%d received=%d err=%v", result.Outcome, len(result.Trials), received.Load(), err)
	}
	for _, trial := range result.Trials {
		if len(trial.Phases) != 1 || trial.Phases[0].Stability != "NO_LARGE_DRIFT_DETECTED" || trial.Phases[0].EvidenceState != "PASS_OBSERVED" || len(trial.Phases[0].Run.Windows) != 4 {
			t.Fatalf("missing real engine window evidence: %+v", trial.Phases)
		}
	}
	run := result.Trials[0].Phases[0].Run
	run.Generator.MeanProcessCPUUtilizationRatio = report.GeneratorUtilizationGuard
	if state, reason := ClassifyEvidence(run); state != "INVALID_DELIVERY" || reason == "" {
		t.Fatalf("generator-bound native phase was accepted: %s %q", state, reason)
	}
	run.Outcome = "FAIL"
	if state, reason := ClassifyEvidence(run); state != "INVALID_DELIVERY" || reason == "" {
		t.Fatalf("generator-bound native failure was reported as an application result: %s %q", state, reason)
	}
	run.Outcome = "PASS"
	run.Generator.MeanProcessCPUUtilizationRatio = 0.1
	run.Generator.PeakProcessCPUUtilizationRatio = 1.1
	if state, reason := ClassifyEvidence(run); state != "PASS_OBSERVED" {
		t.Fatalf("transient process CPU peak invalidated native evidence: %s %q", state, reason)
	}
	run.Metrics.Total = int(report.GeneratorUtilizationGuard * float64(run.Config.MaxRPS) * run.WindowSeconds())
	if state, reason := ClassifyEvidence(run); state != "INVALID_DELIVERY" || reason == "" {
		t.Fatalf("client-rate-bound native phase was accepted: %s %q", state, reason)
	}
}

func TestCampaignSeparatesInvalidDeliveryFromObservedFailure(t *testing.T) {
	options := Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2}
	result, err := WithRunner(context.Background(), scenario(t), options, func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "FAIL", ArrivalsDropped: 1}
	})
	if err != nil || result.Outcome != "INCONCLUSIVE" || len(result.Trials) != 1 || result.Trials[0].Phases[0].EvidenceState != "INVALID_DELIVERY" {
		t.Fatalf("lost arrival was interpreted as target failure: %+v %v", result, err)
	}
	if state, _ := ClassifyEvidence(report.Result{Outcome: "FAIL"}); state != "FAIL_OBSERVED" {
		t.Fatalf("ordinary observed failure classified as %s", state)
	}
}

func TestCampaignRecordsInterruptionReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := WithRunner(ctx, scenario(t), Options{Observation: 10 * time.Second, Repeats: 2}, func(context.Context, config.Validated) report.Result {
		t.Fatal("cancelled campaign started a phase")
		return report.Result{}
	})
	if err != nil || result.Outcome != "ABORTED" || len(result.Trials) != 1 || result.Trials[0].AbortReason != "The campaign was interrupted by its caller." {
		t.Fatalf("interruption reason missing: %+v %v", result, err)
	}
}
