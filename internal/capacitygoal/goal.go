// Package capacitygoal compares a business target against observed explorer
// trials. It does NOT certify sustainable server or human-user capacity.
package capacitygoal

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// Goal expresses the demand to test, measured in synthetic actors executing
// the same defined closed-loop business journey (not registered/logged-in people).
type Goal struct {
	PeakUsers      int
	GrowthPercent  float64
	ReservePercent float64
}

// Assessment deliberately separates planned demand from observations.
type Assessment struct {
	TargetUsers              int
	PeakUsers                int
	GrowthPercent            float64
	ReservePercent           float64
	State                    string
	ObservedPassingUsers     *int
	FirstObservedFailingUsers *int
	MatchedProbeUsers        *int
	MatchedTrials            int
	Findings                 []string
	Next                     []string
}

func (g Goal) required() (int,error) {
	if g.PeakUsers < 1 || g.PeakUsers > 1000000 {
		return 0, errors.New("peak-users must be between 1 and 1000000")
	}
	for _, p := range []float64{g.GrowthPercent,g.ReservePercent} {
		if math.IsNaN(p) || math.IsInf(p,0) || p < 0 || p > 1000 {
			return 0, errors.New("growth-percent and reserve-percent must be finite values from 0 to 1000")
		}
	}
	// Percentages represent successive multipliers: planned peak after growth,
	// then a reserve on that projected peak. This is NOT measured capacity.
	value:=math.Ceil(float64(g.PeakUsers)*(1+g.GrowthPercent/100)*(1+g.ReservePercent/100))
	if math.IsInf(value,0)||value>math.MaxInt {
		return 0, errors.New("calculated target overflows integer bounds")
	}
	return int(value),nil
}

func EvaluatedUsers(g Goal) (int,error) {return g.required()}

// Evaluate is deliberately conservative: a historical synthetic passing level
// does not establish long-term sustainability or equivalence to human users.
func Evaluate(ex explorer.Result, g Goal) (Assessment,error) {
	target,err:=g.required()
	if err!=nil{return Assessment{},err}
	if ex.SchemaVersion != 1 || ex.Scenario=="" || ex.MaxUsers<ex.MinUsers || ex.MinUsers<1 || ex.MaxUsers<1 ||
		ex.Repeats<2 || len(ex.Probes)==0 {
		return Assessment{}, errors.New("invalid or empty capacity exploration: expected repeated schema-v1 probes")
	}
	stage,err:=time.ParseDuration(ex.StageDuration)
	if err!=nil || stage<=0 {return Assessment{},errors.New("missing or invalid stage duration")}
	a:=Assessment{
		PeakUsers:g.PeakUsers,GrowthPercent:g.GrowthPercent,ReservePercent:g.ReservePercent,
		TargetUsers:target,State:"INSUFFICIENT_EVIDENCE",
		ObservedPassingUsers:ex.HighestPassingUsers,
		FirstObservedFailingUsers:ex.FirstFailingUsers,
		Findings:[]string{
			"Goal represents synthetic closed-loop workload-equivalent active actors, not distinct humans or logged-in sessions.",
			"Capacity exploration currently uses short stages; passing trials do not establish sustained production capacity.",
			"No infrastructure upgrade is justified by these HTTP observations alone.",
		},
		Next:[]string{"Test the exact target workload with representative, independently verified sessions and safety limits.",
			"Repeat a sustained campaign with generator, application, database and host telemetry before confirming capacity.",
		},
	}
	if target>ex.MaxUsers {
		a.State="OUTSIDE_TESTED_CEILING"
		a.Findings=append(a.Findings,fmt.Sprintf("Required load %d exceeds the exploration ceiling of %d VUs. No conclusion about meeting that demand is possible.",target,ex.MaxUsers))
		return a,nil
	}
	var matched *explorer.Probe
	for idx:=range ex.Probes {
		p:=&ex.Probes[idx]
		if p.Users<=0 || p.Users>ex.MaxUsers {return Assessment{},errors.New("capacity report contains a probe outside its declared bounds")}
		if p.Users==target {
			if matched!=nil {return Assessment{},errors.New("duplicate target probe is ambiguous")}
			matched=p
		}
	}
	if matched==nil {
		a.State="TARGET_NOT_DIRECTLY_TESTED"
		a.Findings=append(a.Findings,fmt.Sprintf("No recorded trial exercised the exact required %d VUs. A nearby passing or failing level is not a direct validation.",target))
		return a,nil
	}
	a.MatchedProbeUsers=&matched.Users
	a.MatchedTrials=len(matched.Runs)
	if matched.Status=="ABORTED" || matched.Status=="INCONCLUSIVE" {
		a.Findings=append(a.Findings,"Target-stage results were interrupted or declared inconclusive.")
		return a,nil
	}
	if len(matched.Runs)==0 {
		a.Findings=append(a.Findings,"Target-stage result contains no run observations.")
		return a,nil
	}
	if matched.Status=="FAIL" {
		for _,r:=range matched.Runs {
			if r.Config.Users!=target || r.Config.Mode!="closed" || r.Outcome!="FAIL" ||
				!adequateGenerator(r) {
				a.Findings=append(a.Findings,"Target-stage failure cannot be attributed to the application from this report (workload or generator invalid).")
				return a,nil
			}
		}
		a.State="SLO_MISSED_AT_TESTED_TARGET"
		a.Findings=append(a.Findings,fmt.Sprintf("At %d synthetic VUs, a recorded trial missed configured objectives. This does not identify a hardware bottleneck.",target))
		a.Next=append([]string{"Investigate failed steps, error reasons, and resource telemetry before changing infrastructure."},a.Next...)
		return a,nil
	}
	if matched.Status!="PASS" || len(matched.Runs)<ex.Repeats {
		a.Findings=append(a.Findings,"The target load was not confirmed by the required number of passing trials.")
		return a,nil
	}
	for _,r:=range matched.Runs {
		if r.Outcome!="PASS" || r.Config.Users!=target || r.Config.Mode!="closed" || !adequateGenerator(r) {
			a.Findings=append(a.Findings,"One or more target trials has missing/invalid generator data or a client-side load ceiling; this is not verified workload delivery.")
			return a,nil
		}
	}
	a.State="SHORT_TRIAL_TARGET_PASSED"
	a.Findings=append(a.Findings,fmt.Sprintf("%d short synthetic trials at the exact %d-VU target passed their configured criteria. Sustained capacity and the real-user workload equivalence remain unverified.",len(matched.Runs),target))
	if !matched.Runs[0].Config.SeparateSessions {
		a.Findings=append(a.Findings,"Independent authenticated identities were not verified; VUs must not be advertised as distinct people.")
	}
	return a,nil
}

func adequateGenerator(r report.Result) bool {
	if r.Metrics.Total<=0 || r.Generator==nil || r.Generator.Samples<2 ||
		r.ArrivalsDropped>0 || r.ArrivalsDroppedLate>0 || r.Metrics.Cancelled>0 ||
		r.Config.MaxRPS<=0 || r.ElapsedSeconds<=0 ||
		r.Generator.GoCPUValid && r.Generator.GoCPUUtilizationRatio>=0.90 {
		return false
	}
	// Client-side limit binding can hide any potential server capacity.
	return float64(r.Metrics.Total)/r.ElapsedSeconds < 0.90*float64(r.Config.MaxRPS)
}
