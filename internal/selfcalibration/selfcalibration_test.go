package selfcalibration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func authenticatedScenario() config.Scenario {
	steps := make([]config.Step, 0, 5)
	for _, name := range []string{"home", "list", "detail", "search", "profile"} {
		steps = append(steps, config.Step{Name: name, Method: "GET", Path: "/" + name, MustContain: "ok-" + name})
	}
	return config.Scenario{Name: "Synthetic authenticated workload", BaseURL: "https://app.example.com", Users: 4, Duration: "10s",
		Timeout: "2s", MaxRPS: 100, ThinkTime: "100ms", P95LimitMS: 500, PerVUCookieEnvPrefix: "SELFCAL_TEST_COOKIE_", Steps: steps}
}

func shortOptions(levels ...int) Options {
	return Options{Levels: levels, LevelDuration: 1500 * time.Millisecond, ResponseBytes: 26 << 10, ServerDelay: 15 * time.Millisecond}
}

func init() { MinLevelDuration = time.Second }

func TestSelfCalibrationReplaysScenarioWithoutTargetCredentials(t *testing.T) {
	record, err := Run(context.Background(), authenticatedScenario(), shortOptions(2, 4))
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != calibration.ScenarioCalibrationPassed || len(record.Levels) != 2 {
		t.Fatalf("unexpected record: %+v", record)
	}
	for _, level := range record.Levels {
		if level.ServerReceived != level.ClientRequests || level.ServerCookieRequests != level.ServerReceived || level.ClientResponseBodyBytes == 0 {
			t.Fatalf("mirror ledger does not reconcile: %+v", level)
		}
	}
	if !record.Controls.PerVUCookieSessions || record.Controls.Steps != 5 || len(record.Mirror.Routes) != 5 || record.Mirror.Routes[0].ResponseBytes != 26<<10 {
		t.Fatalf("workload shape was not mirrored: %+v %+v", record.Controls, record.Mirror)
	}
	if _, err := calibration.ParseScenarioRecord(mustJSON(t, record), true); err != nil {
		t.Fatalf("artifact does not round-trip: %v", err)
	}
}

// The recorded per-level fingerprint must equal the fingerprint the real
// scenario records when its own sessions are present.
func TestSelfCalibrationFingerprintMatchesRealScenario(t *testing.T) {
	s := authenticatedScenario()
	for i := 1; i <= 4; i++ {
		t.Setenv("SELFCAL_TEST_COOKIE_"+string(rune('0'+i)), "session=value-"+string(rune('0'+i)))
	}
	real, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Run(context.Background(), s, shortOptions(4))
	if err != nil {
		t.Fatal(err)
	}
	if record.Levels[0].WorkloadFingerprint != real.WorkloadFingerprint {
		t.Fatalf("fingerprint mismatch: %s != %s", record.Levels[0].WorkloadFingerprint, real.WorkloadFingerprint)
	}
}

func TestSelfCalibrationUsesRecordedReportShapes(t *testing.T) {
	s := authenticatedScenario()
	s.PerVUCookieEnvPrefix = ""
	real, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	recorded := report.Result{Config: engine.RunConfigFor(real)}
	for i, step := range s.Steps {
		m := report.NewMetricSet()
		m.Total, m.Success, m.P50MS = 10, 10, float64(5+i)
		m.ResponseBody.CompleteObservations, m.ResponseBody.TotalBytes = 10, int64(10*(1000+i))
		recorded.Steps = append(recorded.Steps, report.StepResult{Profile: "default", Name: step.Name, Method: "GET", Path: step.Path, Metrics: *m})
	}
	opt := Options{Levels: []int{4}, LevelDuration: 1500 * time.Millisecond, FromReport: &recorded, FromReportSHA256: "sha256:" + strings.Repeat("a", 64)}
	record, err := Run(context.Background(), s, opt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Mirror.Source != "recorded_report" || record.Mirror.Routes[2].ResponseBytes != 1002 || record.Mirror.Routes[2].DelayMS != 7 {
		t.Fatalf("recorded shapes not applied: %+v", record.Mirror)
	}
	other := recorded
	other.Config.WorkloadFingerprint = "sha256:" + strings.Repeat("b", 64)
	opt.FromReport = &other
	if _, err := Run(context.Background(), s, opt); err == nil {
		t.Fatal("a report from a different workload must be rejected")
	}
}

func TestSelfCalibrationFailsUnderForcedGeneratorPressure(t *testing.T) {
	opt := shortOptions(2, 4)
	// A 1-byte RSS ceiling is always exceeded: the engine's own guardrail
	// stops the level and the headroom check must fail.
	opt.Guardrails = generatorhealth.Guardrails{MaxProcessRSSBytes: 1, ConsecutiveSamples: 2}
	record, err := Run(context.Background(), authenticatedScenario(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != calibration.ScenarioCalibrationFailed || len(record.Levels) != 1 || !contains(record.Levels[0].FailedChecks, "generator_headroom") {
		t.Fatalf("overloaded generator passed calibration: %+v", record.Levels)
	}
	if _, err := calibration.ParseScenarioRecord(mustJSON(t, record), true); err == nil {
		t.Fatal("a failed calibration must not be accepted as passing evidence")
	}
}

func TestSelfCalibrationFailsOnSchedulerPressure(t *testing.T) {
	s := authenticatedScenario()
	s.Mode, s.ArrivalRate, s.PerVUCookieEnvPrefix = "arrival", 2, ""
	opt := shortOptions(2)
	opt.Runner = func(ctx context.Context, v config.Validated) report.Result {
		r := engine.Run(ctx, v)
		r.SchedulerLagP95MS, r.SchedulerLagMaxMS = 180, 400 // injected scheduler stall
		return r
	}
	record, err := Run(context.Background(), s, opt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != calibration.ScenarioCalibrationFailed || !contains(record.Levels[0].FailedChecks, "scheduler_lag_bounded") {
		t.Fatalf("scheduler pressure was not detected: %+v", record.Levels)
	}
}

func TestSelfCalibrationRejectsUnreplayableAndUnboundedPlans(t *testing.T) {
	s := authenticatedScenario()
	s.Steps[1].Extract = map[string]config.Extract{"id": {Header: "X-Id"}}
	if _, err := Run(context.Background(), s, shortOptions(2)); err == nil {
		t.Fatal("extraction must be rejected")
	}
	opt := shortOptions(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
	opt.LevelDuration = 2 * time.Minute
	if _, err := Run(context.Background(), authenticatedScenario(), opt); err == nil {
		t.Fatal("wall-time cap must be enforced")
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
