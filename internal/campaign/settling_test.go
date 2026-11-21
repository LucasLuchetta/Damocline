package campaign_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func settlingRun(p95s ...float64) report.Result {
	run := report.Result{ObservationSeconds: float64(2 * len(p95s))}
	for i, p95 := range p95s {
		m := report.MetricSet{Total: 50, Success: 50, P95MS: p95}
		run.Windows = append(run.Windows, report.WindowResult{StartSeconds: float64(2 * i), EndSeconds: float64(2*i + 2), Metrics: m, Journeys: m,
			Steps: []report.WindowMetric{{Name: "page", Metrics: m}}})
	}
	return run
}

func TestAssessWarmupSettlingRequiresASettledTail(t *testing.T) {
	policy := campaign.WarmupSettlingPolicy{WindowSeconds: 2, TolerancePercent: 25, ToleranceMS: 5, ConsecutiveStable: 2, MinimumSamples: 20}
	got := campaign.AssessWarmupSettling(settlingRun(400, 390, 100, 98, 101), policy)
	if got.State != campaign.SettlingStabilized || got.StabilizedAfterSeconds == nil || *got.StabilizedAfterSeconds != 4 {
		t.Fatalf("transient followed by a settled tail: %+v", got)
	}
	// Two stable comparisons early do not excuse a change at the end.
	if got := campaign.AssessWarmupSettling(settlingRun(100, 100, 100, 300), policy); got.State != campaign.SettlingNonStationary {
		t.Fatalf("late change must be NON_STATIONARY: %+v", got)
	}
	if got := campaign.AssessWarmupSettling(settlingRun(100, 100), policy); got.State != campaign.SettlingInsufficient {
		t.Fatalf("two windows cannot show two comparisons: %+v", got)
	}
	sparse := settlingRun(100, 100, 100)
	sparse.Windows[2].Metrics.Total = 5
	if got := campaign.AssessWarmupSettling(sparse, policy); got.State != campaign.SettlingNonStationary {
		t.Fatalf("an under-sampled window is not comparable: %+v", got)
	}
	// A single step that keeps moving blocks the verdict even when the
	// aggregate is flat.
	step := settlingRun(100, 100, 100)
	step.Windows[2].Steps[0].Metrics.P95MS = 200
	if got := campaign.AssessWarmupSettling(step, policy); got.State != campaign.SettlingNonStationary {
		t.Fatalf("moving step must block settling: %+v", got)
	}
	if err := campaign.ValidateWarmupSettlingPolicy(policy, 4*time.Second); err == nil {
		t.Fatal("a warm-up shorter than three settling windows must be rejected")
	}
}

// transientServer is slow for the first slowFor after its first request and
// fast afterwards; growing never settles and slows down linearly.
func transientServer(slowFor, slow time.Duration, growing bool) *httptest.Server {
	var once sync.Once
	var start time.Time
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { start = time.Now() })
		elapsed := time.Since(start)
		switch {
		case growing:
			time.Sleep(40*time.Millisecond + elapsed/50)
		case elapsed < slowFor:
			time.Sleep(slow)
		default:
			time.Sleep(40 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func settlingScenario(t *testing.T, target string) config.Validated {
	t.Helper()
	v, err := config.Parse(config.Scenario{
		Name: "settling", BaseURL: target, Users: 3, Duration: "10s", Timeout: "2s", MaxRPS: 100,
		P95LimitMS: 120, MaxErrorRate: 0.1, WindowDuration: "2.5s",
		Steps: []config.Step{{Name: "request", Method: http.MethodGet, Path: "/"}},
	}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func settlingStability() campaign.StabilityPolicy {
	s := campaign.DefaultStabilityPolicy()
	s.MaxP95IncreasePercent, s.MaxP95IncreaseMS = 100, 100
	return s
}

func TestStartupTransientFailsShortWarmupButSettlesWithWindowedWarmup(t *testing.T) {
	if testing.Short() {
		t.Skip("real-engine campaign")
	}
	t.Run("fixed short warm-up judges the transient", func(t *testing.T) {
		t.Parallel()
		server := transientServer(5*time.Second, 200*time.Millisecond, false)
		defer server.Close()
		result, err := campaign.WithRunner(context.Background(), settlingScenario(t, server.URL), campaign.Options{
			Warmup: 2 * time.Second, Observation: 10 * time.Second, Recovery: 5 * time.Second, Repeats: 2, Stability: settlingStability(),
		}, engine.Run)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome == "PASS" {
			t.Fatalf("observation that contains the start-up transient must not pass: %+v", result.Trials)
		}
	})
	t.Run("settled warm-up", func(t *testing.T) {
		t.Parallel()
		server := transientServer(3*time.Second, 200*time.Millisecond, false)
		defer server.Close()
		policy := campaign.WarmupSettlingPolicy{WindowSeconds: 2, TolerancePercent: 50, ToleranceMS: 20, ConsecutiveStable: 2, MinimumSamples: 20}
		result, err := campaign.WithRunner(context.Background(), settlingScenario(t, server.URL), campaign.Options{
			Warmup: 10 * time.Second, WarmupSettling: policy, Observation: 10 * time.Second, Recovery: 5 * time.Second, Repeats: 2, Stability: settlingStability(),
		}, engine.Run)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != "PASS" || result.WarmupSettling == nil || *result.WarmupSettling != policy {
			for _, ph := range result.Trials[0].Phases {
				t.Logf("%s %s %s %s %s", ph.Name, ph.EvidenceState, ph.Reason, ph.Stability, ph.Run.Outcome)
			}
			t.Fatalf("settled campaign did not pass: %s", result.Outcome)
		}
		first := result.Trials[0].Phases[0]
		if first.Name != "warmup" || first.Settling == nil || first.Settling.State != campaign.SettlingStabilized || *first.Settling.StabilizedAfterSeconds < 2 {
			t.Fatalf("first warm-up must record the transient and a later settle point: %+v", first.Settling)
		}
		for _, trial := range result.Trials {
			for _, phase := range trial.Phases {
				if phase.Name == "observation" && (phase.Run.Outcome != "PASS" || len(phase.Run.Windows) < 3) {
					t.Fatalf("observation windows must still be judged: %+v", phase.Run.Windows)
				}
			}
		}
	})
	t.Run("never settles", func(t *testing.T) {
		t.Parallel()
		server := transientServer(0, 0, true)
		defer server.Close()
		policy := campaign.WarmupSettlingPolicy{WindowSeconds: 2, TolerancePercent: 10, ToleranceMS: 5, ConsecutiveStable: 2, MinimumSamples: 20}
		phases := 0
		result, err := campaign.WithRunner(context.Background(), settlingScenario(t, server.URL), campaign.Options{
			Warmup: 8 * time.Second, WarmupSettling: policy, Observation: 10 * time.Second, Recovery: 5 * time.Second, Repeats: 2, Stability: settlingStability(),
		}, func(ctx context.Context, v config.Validated) report.Result {
			phases++
			return engine.Run(ctx, v)
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != "INCONCLUSIVE" || phases != 1 || len(result.Trials) != 1 {
			t.Fatalf("a never-settling warm-up must end INCONCLUSIVE before judged phases: %s phases=%d", result.Outcome, phases)
		}
		s := result.Trials[0].Phases[0].Settling
		if s == nil || s.State != campaign.SettlingNonStationary || !strings.Contains(strings.Join(result.Notes, " "), "NON_STATIONARY") {
			t.Fatalf("missing NON_STATIONARY settling evidence: %+v %v", s, result.Notes)
		}
	})
}
