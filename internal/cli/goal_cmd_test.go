package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestGoalCommandUsesSavedExploreWithoutGeneratingTraffic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "capacity.json")
	current := report.Result{
		Outcome:"PASS", ElapsedSeconds:30,
		Config:report.RunConfig{Users:30,Mode:"closed",MaxRPS:100,SeparateSessions:true},
		Metrics:report.MetricSet{Total:30,Success:30},
		Generator:&generatorhealth.Summary{Samples:4,GoCPUValid:true,GoCPUUtilizationRatio:0.22},
	}
	r := explorer.Result{SchemaVersion:1,Scenario:"synthetic app",StageDuration:"30s",
		MinUsers:2,MaxUsers:50,Repeats:2,
		Probes:[]explorer.Probe{{Users:30,Status:"PASS",Runs:[]report.Result{current,current}}},
	}
	if err := report.WriteAnyJSON(file,r);err!=nil{t.Fatal(err)}
	var out,errOut bytes.Buffer
	exit:=Run([]string{"goal","--file",file,"--peak-users","20","--growth-percent","25","--reserve-percent","20"},&out,&errOut)
	if exit!=0{t.Fatalf("exit %d: %s",exit,errOut.String())}
	for _,want:=range []string{"DEMAND TO VALIDATE: 30","SHORT_TRIAL_TARGET_PASSED","not a certified server limit","No load was generated","upgrade is NOT justified"}{
		if !strings.Contains(out.String(),want){t.Fatalf("expected %q: %s",want,out.String())}
	}
}
func TestGoalCommandRejectsInvalidJSONAndMissingFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if c:=Run([]string{"goal"},&out,&errOut);c!=2 {t.Fatalf("missing flags: exit %d",c)}
	out.Reset();errOut.Reset()
	p:=filepath.Join(t.TempDir(),"not-a-report.json")
	if err:=report.WriteAnyJSON(p,map[string]any{"hello":"world"});err!=nil{t.Fatal(err)}
	if c:=Run([]string{"goal","--file",p,"--peak-users","20"},&out,&errOut);c!=2 || !strings.Contains(errOut.String(),"invalid or empty capacity exploration") {
		t.Fatalf("invalid JSON schema not rejected: exit %d stderr %s",c,errOut.String())
	}
}
