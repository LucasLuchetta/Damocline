package explorer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// An INCONCLUSIVE refinement probe after repeatedly passing levels keeps the
// highest repeatedly passing level and states which levels stayed unresolved.
func TestInconclusiveRefinementKeepsHighestRepeatedlyPassingLevel(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 2, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(_ context.Context, v config.Validated) report.Result {
		switch users := v.Scenario.Users; {
		case users == 16:
			return report.Result{Outcome: "FAIL"}
		case users > 8:
			return report.Result{Outcome: "FAIL", Config: report.RunConfig{P95LimitMS: 1000}, NetworkBaseline: &report.NetworkBaseline{
				Status: report.NetworkBaselineMeasured, Planned: 20, Completed: 19, Failures: 1, P50MS: 5, P95MS: 9, MaxMS: 10, MaxToP50Ratio: 2}}
		}
		return report.Result{Outcome: "PASS"}
	})
	if err != nil || r.Outcome != "INCONCLUSIVE" || r.HighestPassingUsers == nil || *r.HighestPassingUsers != 8 || r.FirstFailingUsers != nil {
		t.Fatalf("unexpected exploration: %+v err=%v", r, err)
	}
	note := r.Notes[len(r.Notes)-1]
	if !strings.Contains(note, "Levels from 12 VUs upward are unresolved") || !strings.Contains(note, "incomplete") {
		t.Fatalf("unresolved levels were not stated: %q", note)
	}
}

func TestInconclusiveFirstProbeHasNoPassingLevel(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://app.example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 4, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result {
		return report.Result{Outcome: "INCONCLUSIVE", ArrivalsDroppedLate: 1}
	})
	if r.Outcome != "INCONCLUSIVE" || r.HighestPassingUsers != nil {
		t.Fatalf("unexpected exploration: %+v", r)
	}
}
