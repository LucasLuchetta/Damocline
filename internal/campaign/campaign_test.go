package campaign

import (
	"context"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
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
		return report.Result{Outcome: "PASS"}
	})
	if err != nil || result.Outcome != "PASS" || len(result.Trials) != 2 || len(durations) != 6 {
		t.Fatalf("unexpected campaign: %+v err=%v durations=%v", result, err, durations)
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
