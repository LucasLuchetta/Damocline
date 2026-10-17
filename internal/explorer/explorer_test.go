package explorer

import (
	"context"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"testing"
	"time"
)

func TestBracketAndRefine(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "test", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 2, MaxUsers: 30, Repeats: 2, StageDuration: 10 * time.Second}, func(_ context.Context, v config.Validated) report.Result {
		counts[v.Scenario.Users]++
		outcome := "PASS"
		if v.Scenario.Users > 13 {
			outcome = "FAIL"
		}
		return report.Result{Outcome: outcome}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.HighestPassingUsers == nil || *r.HighestPassingUsers != 13 || r.FirstFailingUsers == nil || *r.FirstFailingUsers != 14 {
		t.Fatalf("bad bracket: %+v", r)
	}
	if r.Outcome != "BRACKETED" {
		t.Fatalf("bad outcome: %s", r.Outcome)
	}
	if counts[13] != 2 {
		t.Fatalf("passing level should be confirmed twice: %d", counts[13])
	}
}
func TestNoPassingLevel(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := WithRunner(context.Background(), v, Options{MinUsers: 1, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result { return report.Result{Outcome: "FAIL"} })
	if err != nil || r.Outcome != "NO_PASSING_LEVEL" || r.HighestPassingUsers != nil {
		t.Fatalf("unexpected: %+v, %v", r, err)
	}
}
func TestAbort(t *testing.T) {
	v, err := config.Parse(config.Scenario{Name: "x", BaseURL: "https://example.com", Users: 1, Duration: "10s", MaxRPS: 100, P95LimitMS: 1000, Steps: []config.Step{{Name: "x", Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := WithRunner(ctx, v, Options{MinUsers: 1, MaxUsers: 16, Repeats: 2, StageDuration: 10 * time.Second}, func(context.Context, config.Validated) report.Result {
		t.Fatal("should not run")
		return report.Result{}
	})
	if err != nil || r.Outcome != "ABORTED" {
		t.Fatalf("unexpected: %+v %v", r, err)
	}
}

func TestInconclusiveGeneratorCannotBecomeApplicationFailure(t *testing.T) {
    v, err := config.Parse(config.Scenario{
        Name: "local scenario", BaseURL:"http://127.0.0.1:18080",
        Users:1, Duration:"10s", MaxRPS:20, P95LimitMS:1000,
        Steps:[]config.Step{{Name:"local",Path:"/"}},
    },config.Options{Authorized:true})
    if err!=nil{t.Fatal(err)}
    r,err:=WithRunner(context.Background(),v,Options{
        MinUsers:1,MaxUsers:4,Repeats:2,StageDuration:10*time.Second,
    },func(context.Context,config.Validated)report.Result{
        return report.Result{Outcome:"INCONCLUSIVE",ArrivalsDroppedLate:1}
    })
    if err!=nil || r.Outcome!="INCONCLUSIVE" || r.HighestPassingUsers!=nil || r.FirstFailingUsers!=nil {
        t.Fatalf("generator limitations must not be branded as server capacity failure: %+v err=%v",r,err)
    }
}
