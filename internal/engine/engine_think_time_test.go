package engine

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestSampledThinkTimeIsBoundedAndRepeatable(t *testing.T) {
	profile := config.PreparedProfile{ThinkTimeMin: 2 * time.Millisecond, ThinkTimeMax: 20 * time.Millisecond}
	first := make([]time.Duration, 100)
	changed := false
	for i := range first {
		first[i] = sampledThinkTime(42, 3, uint64(i+1), 0, profile)
		if first[i] < profile.ThinkTimeMin || first[i] > profile.ThinkTimeMax {
			t.Fatalf("sample %d outside declared bounds: %s", i, first[i])
		}
		if other := sampledThinkTime(43, 3, uint64(i+1), 0, profile); other != first[i] {
			changed = true
		}
		if repeat := sampledThinkTime(42, 3, uint64(i+1), 0, profile); repeat != first[i] {
			t.Fatalf("sample %d changed across replay: %s versus %s", i, first[i], repeat)
		}
	}
	if !changed {
		t.Fatal("changing seed never changed a timing decision")
	}
}

func TestDiscreteThinkTimeSamplesAreRepeatableAndWeighted(t *testing.T) {
	profile := config.PreparedProfile{ThinkTimeSamples: []time.Duration{0, 0, 0, 20 * time.Millisecond}}
	count := 0
	for i := 0; i < 1000; i++ {
		got := sampledThinkTime(42, 3, uint64(i+1), 0, profile)
		if got != 0 && got != 20*time.Millisecond {
			t.Fatalf("sample outside declared set: %s", got)
		}
		if got != sampledThinkTime(42, 3, uint64(i+1), 0, profile) {
			t.Fatal("same seeded coordinate changed")
		}
		if got == 20*time.Millisecond {
			count++
		}
	}
	if count < 180 || count > 320 {
		t.Fatalf("repeated entries did not produce an approximately 3:1 selection: %d", count)
	}
}

func TestUniformThinkTimeRunsThroughTwoStepJourney(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := config.Scenario{
		Name: "synthetic timing", BaseURL: server.URL, Users: 1, Duration: "1s", MaxRPS: 20,
		P95LimitMS: 2000, MaxErrorRate: 0, WorkloadSeed: 42,
		ThinkTimeRange: &config.ThinkTimeRange{Min: "10ms", Max: "20ms"},
		Steps:          []config.Step{{Name: "first", Path: "/first"}, {Name: "second", Path: "/second"}},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), v)
	if result.Metrics.Total < 2 || received.Load() != int64(result.Metrics.Total) {
		t.Fatalf("client/server request ledgers differ: client=%d server=%d", result.Metrics.Total, received.Load())
	}
	if result.Config.WorkloadSeed != 42 || result.Config.ThinkTimeRange == nil || result.Config.ThinkTimeRange.Min != "10ms" {
		t.Fatalf("timing provenance missing from report: %+v", result.Config)
	}
	// The final interval may be partially observed if the bounded drain ends.
	minimumCompleted := float64(result.ThinkTimeObservations-1) * 10
	if result.ThinkTimeObservations == 0 || result.ObservedThinkTimeMS < minimumCompleted {
		t.Fatalf("bounded between-step timing not observed: count=%d total=%.3f", result.ThinkTimeObservations, result.ObservedThinkTimeMS)
	}
	if result.Journeys.Total == 0 || result.Quality == nil || result.Quality.State == report.QualityInvalid {
		t.Fatalf("journey or quality accounting failed: journeys=%+v quality=%+v", result.Journeys, result.Quality)
	}
}

func TestThreeProfilesShareRateBudgetWithTimingOverrides(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := config.Scenario{
		Name: "three synthetic journeys", BaseURL: server.URL, Users: 3, Duration: "1s", MaxRPS: 12,
		P95LimitMS: 2000, MaxErrorRate: 0, WorkloadSeed: 42,
		ThinkTimeRange: &config.ThinkTimeRange{Min: "5ms", Max: "15ms"},
		Profiles: []config.Profile{
			{Name: "browse", Weight: 1, Steps: []config.Step{{Name: "browse-list", Path: "/browse"}, {Name: "browse-detail", Path: "/browse/detail"}}},
			{Name: "search", Weight: 1, ThinkTimeSamples: []string{"10ms", "10ms", "20ms"}, Steps: []config.Step{{Name: "search-form", Path: "/search"}, {Name: "search-results", Path: "/search/results"}}},
			{Name: "review", Weight: 1, ThinkTime: "10ms", Steps: []config.Step{{Name: "review-list", Path: "/review"}, {Name: "review-detail", Path: "/review/detail"}}},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), v)
	// Already-started journeys may consume tokens during bounded drain.
	// The shared ticker must bound all profiles over the full elapsed period.
	limit := int(math.Ceil(result.ElapsedSeconds*float64(s.MaxRPS))) + 1
	if result.Metrics.Total != int(received.Load()) || result.Metrics.Total > limit {
		t.Fatalf("shared 12 RPS budget or server ledger violated: client=%d server=%d", result.Metrics.Total, received.Load())
	}
	if len(result.Profiles) != 3 || len(result.Config.Profiles) != 3 {
		t.Fatalf("three profile ledgers missing: %+v", result.Profiles)
	}
	for _, profile := range result.Profiles {
		if profile.AssignedVUs != 1 || profile.Journeys.Total == 0 {
			t.Fatalf("profile %q not exercised by its assigned actor: %+v", profile.Name, profile)
		}
	}
	if result.Config.Profiles[0].ThinkTimeRange == nil || len(result.Config.Profiles[1].ThinkTimeSamples) != 3 || result.Config.Profiles[1].ThinkTime != "" || result.Config.Profiles[2].ThinkTimeRange != nil || result.Config.Profiles[2].ThinkTime != "10ms" {
		t.Fatalf("profile timing overrides not persisted: %+v", result.Config.Profiles)
	}
	if result.Quality == nil || result.Quality.State == report.QualityInvalid {
		t.Fatalf("profile and request ledgers did not reconcile: %+v", result.Quality)
	}
}
