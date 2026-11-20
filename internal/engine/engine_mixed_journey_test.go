package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// TestMixedMethodJourneyCancelledMidJourneyConservesCounters runs a
// three-step GET/POST/GET journey with think time against a loopback ledger
// and cancels the run while the third step of the third journey is in
// flight. Every request the server receives must appear exactly once in the
// client's per-step completed or cancelled counters, completed journeys must
// equal completed final steps, and the partial journey must be counted as
// cancelled rather than entering completed journey percentiles.
func TestMixedMethodJourneyCancelledMidJourneyConservesCounters(t *testing.T) {
	const completedJourneys = 2
	var mu sync.Mutex
	received := map[string]int{}
	methods := map[string]string{}
	finalStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received[r.URL.Path]++
		methods[r.URL.Path] = r.Method
		finals := received["/confirm"]
		mu.Unlock()
		if r.URL.Path == "/confirm" && finals > completedJourneys {
			finalStarted <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	s := config.Scenario{
		Name: "mixed journey abort", BaseURL: server.URL, Users: 1, Duration: "1s",
		MaxRPS: 100, P95LimitMS: 1000, MaxErrorRate: 0, ThinkTime: "15ms",
		Steps: []config.Step{
			{Name: "browse", Method: http.MethodGet, Path: "/browse"},
			{Name: "submit", Method: http.MethodPost, Path: "/submit"},
			{Name: "confirm", Method: http.MethodGet, Path: "/confirm"},
		},
	}
	v, err := config.Parse(s, config.Options{Authorized: true, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	v.Duration = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-finalStarted:
		case <-time.After(4 * time.Second):
		}
		cancel()
	}()
	r := Run(ctx, v)

	mu.Lock()
	ledger := map[string]int{}
	for path, count := range received {
		ledger[path] = count
	}
	gotMethods := map[string]string{}
	for path, method := range methods {
		gotMethods[path] = method
	}
	mu.Unlock()

	if r.Outcome != "ABORTED" || len(r.Steps) != 3 {
		t.Fatalf("mid-journey cancellation was not reported as an aborted three-step run: outcome=%s steps=%d", r.Outcome, len(r.Steps))
	}
	if gotMethods["/browse"] != http.MethodGet || gotMethods["/submit"] != http.MethodPost || gotMethods["/confirm"] != http.MethodGet {
		t.Fatalf("mixed request methods were not delivered: %+v", gotMethods)
	}
	wantServer := map[string]int{"/browse": completedJourneys + 1, "/submit": completedJourneys + 1, "/confirm": completedJourneys + 1}
	for path, want := range wantServer {
		if ledger[path] != want {
			t.Fatalf("server ledger %s = %d, want %d (ledger=%+v)", path, ledger[path], want, ledger)
		}
	}
	paths := map[string]string{"browse": "/browse", "submit": "/submit", "confirm": "/confirm"}
	for _, step := range r.Steps {
		wantCancelled := 0
		if step.Name == "confirm" {
			wantCancelled = 1
		}
		m := step.Metrics
		if m.Success != wantServer[paths[step.Name]]-wantCancelled || m.Failures != 0 || m.Cancelled != wantCancelled ||
			m.Total+m.Cancelled != ledger[paths[step.Name]] {
			t.Fatalf("step %s counters do not conserve the server ledger: %+v ledger=%+v", step.Name, m, ledger)
		}
	}
	requests := ledger["/browse"] + ledger["/submit"] + ledger["/confirm"]
	if r.Metrics.Total+r.Metrics.Cancelled != requests || r.Metrics.Cancelled != 1 || r.Metrics.Success != requests-1 {
		t.Fatalf("aggregate counters do not conserve %d server requests: %+v", requests, r.Metrics)
	}
	if r.Journeys.Total != completedJourneys || r.Journeys.Success != completedJourneys || r.Journeys.Cancelled != 1 ||
		r.Journeys.Total != r.Steps[2].Metrics.Success {
		t.Fatalf("journey ledger does not match completed final steps: %+v", r.Journeys)
	}
	if r.IterationsStarted != completedJourneys+1 || r.IterationsCompleted != completedJourneys {
		t.Fatalf("iteration counters do not conserve journeys: started=%d completed=%d", r.IterationsStarted, r.IterationsCompleted)
	}
	// Think time runs between steps: two pauses per completed journey and the
	// two pauses that preceded the cancelled final step.
	if r.ThinkTimeObservations != 2*(completedJourneys+1) || r.ObservedThinkTimeMS < float64(r.ThinkTimeObservations)*10 {
		t.Fatalf("think-time intervals were not counted exactly: observations=%d total=%.1fms", r.ThinkTimeObservations, r.ObservedThinkTimeMS)
	}
	if r.Journeys.MeanMS < 2*10 {
		t.Fatalf("completed journey elapsed time excludes observed think time: %+v", r.Journeys)
	}
	if r.Quality == nil || r.Quality.State == report.QualityInvalid {
		t.Fatalf("conserved mid-journey cancellation must remain arithmetically valid: %+v", r.Quality)
	}
}
