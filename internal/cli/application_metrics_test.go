package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestApplicationMetricsRunnerAttachesTimelineWithoutChangingLoadCounters(t *testing.T) {
	var requests atomic.Int32
	scraped := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("queue_depth 5\n"))
		scraped <- struct{}{}
	}))
	defer server.Close()

	base := phaseRunner(func(context.Context, config.Validated) report.Result {
		select {
		case <-scraped:
			time.Sleep(20 * time.Millisecond)
		case <-time.After(2 * time.Second):
			t.Fatal("metrics scrape did not run concurrently with the phase")
		}
		return report.Result{Metrics: report.MetricSet{Total: 3, Success: 3}}
	})
	runner := withApplicationMetrics(server.URL+"/metrics", time.Second, appmetrics.NewBudget(5), base)
	result := runner(context.Background(), config.Validated{})
	if requests.Load() != 1 || result.Metrics.Total != 3 || result.ApplicationMetrics == nil {
		t.Fatalf("app-metrics scrape changed load accounting or was not attached: requests=%d result=%+v", requests.Load(), result)
	}
	if result.ApplicationMetrics.State() != "COLLECTED" || len(result.ApplicationMetrics.Samples) != 1 || result.ApplicationMetrics.Samples[0].Samples[0].Value != 5 {
		t.Fatalf("unexpected application-metrics timeline: %+v", result.ApplicationMetrics)
	}
}

func TestApplicationMetricsOptionsAndCampaignDryRunsAreBounded(t *testing.T) {
	if err := validateApplicationMetrics("http://127.0.0.1/metrics?token=secret", 10*time.Second); err == nil {
		t.Fatal("credential-like query parameter was accepted")
	}
	if err := validateApplicationMetrics("http://127.0.0.1/metrics", 500*time.Millisecond); err == nil {
		t.Fatal("sub-second metrics interval was accepted")
	}
	if err := validateApplicationMetrics("http://127.0.0.1/metrics", 10*time.Second); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("queue_depth 5\n"))
	}))
	defer server.Close()
	cases := []struct {
		name string
		args []string
	}{
		{name: "run", args: []string{"run", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--dry-run"}},
		{name: "closed campaign", args: []string{"campaign", "--config", filepath.Join("..", "..", "examples", "advanced-readonly.json"), "--warmup", "0s", "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run"}},
		{name: "arrival campaign", args: []string{"arrival-campaign", "--config", filepath.Join("..", "..", "examples", "arrival-rate-readonly.json"), "--observation", "10s", "--recovery", "10s", "--repeats", "2", "--dry-run"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append(tc.args, "--app-metrics-endpoint", server.URL+"/metrics", "--app-metrics-interval", "1s")
			if code := Run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("dry run rejected valid local endpoint: code=%d stderr=%q", code, stderr.String())
			}
			if requests.Load() != 0 {
				t.Fatalf("dry run scraped the application metrics endpoint %d times", requests.Load())
			}
		})
	}
}

func TestBothCampaignModesRetainMetricsPerPhase(t *testing.T) {
	scraped := make(chan struct{}, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("queue_depth 5\n"))
		scraped <- struct{}{}
	}))
	defer server.Close()

	closedScenario, err := config.Load(filepath.Join("..", "..", "examples", "advanced-readonly.json"))
	if err != nil {
		t.Fatal(err)
	}
	closedBase, err := config.Parse(closedScenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	arrivalScenario, err := config.Load(filepath.Join("..", "..", "examples", "arrival-rate-readonly.json"))
	if err != nil {
		t.Fatal(err)
	}
	arrivalBase, err := config.Parse(arrivalScenario, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}

	makeRunner := func() phaseRunner {
		base := phaseRunner(func(ctx context.Context, scenario config.Validated) report.Result {
			select {
			case <-scraped:
				time.Sleep(5 * time.Millisecond)
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("phase did not receive its concurrent metrics scrape")
			}
			return report.Result{Outcome: "INCONCLUSIVE", ElapsedSeconds: 10, ObservationSeconds: 10,
				Config:  report.RunConfig{Users: scenario.Scenario.Users, MaxRPS: scenario.Scenario.MaxRPS, Mode: scenario.Scenario.Mode, Duration: scenario.Scenario.Duration},
				Metrics: report.MetricSet{}, Journeys: report.MetricSet{}}
		})
		return withApplicationMetrics(server.URL+"/metrics", time.Second, appmetrics.NewBudget(16), base)
	}

	closed, err := campaign.WithRunner(context.Background(), closedBase, campaign.Options{
		Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
	}, campaign.Runner(makeRunner()))
	if err != nil {
		t.Fatalf("closed campaign failed: %v", err)
	}
	closedPhaseCount := 0
	for _, trial := range closed.Trials {
		closedPhaseCount += len(trial.Phases)
	}
	assertCampaignMetricTimelines(t, closedPhaseCount, func(i int) *appmetrics.Timeline {
		phaseIndex := 0
		for _, trial := range closed.Trials {
			for _, phase := range trial.Phases {
				if phaseIndex == i {
					return phase.Run.ApplicationMetrics
				}
				phaseIndex++
			}
		}
		return nil
	})

	arrival, err := arrivalcampaign.WithRunner(context.Background(), arrivalBase, arrivalcampaign.Options{
		Warmup: 0, Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2,
	}, arrivalcampaign.Runner(makeRunner()))
	if err != nil {
		t.Fatalf("arrival campaign failed: %v", err)
	}
	phaseCount := 0
	for _, trial := range arrival.Trials {
		phaseCount += len(trial.Phases)
	}
	assertCampaignMetricTimelines(t, phaseCount, func(i int) *appmetrics.Timeline {
		phaseIndex := 0
		for _, trial := range arrival.Trials {
			for _, phase := range trial.Phases {
				if phaseIndex == i {
					return phase.Run.ApplicationMetrics
				}
				phaseIndex++
			}
		}
		return nil
	})
}

func assertCampaignMetricTimelines(t *testing.T, phaseCount int, timelineAt func(int) *appmetrics.Timeline) {
	t.Helper()
	if phaseCount == 0 {
		t.Fatal("campaign did not execute a phase")
	}
	for i := 0; i < phaseCount; i++ {
		timeline := timelineAt(i)
		if timeline == nil || timeline.State() != "COLLECTED" || len(timeline.Samples) != 1 {
			t.Fatalf("phase %d did not retain one successful metric sample: %+v", i, timeline)
		}
		if err := appmetrics.ValidateTimeline(*timeline); err != nil {
			t.Fatalf("phase %d stored an invalid metrics timeline: %v", i, err)
		}
	}
}
