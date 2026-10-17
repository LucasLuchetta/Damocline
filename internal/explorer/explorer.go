// Package explorer probes bounded virtual-user levels using Damocline's native engine.
// It reports the highest repeatedly passing *tested* level, not a universal server limit.
package explorer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

type Options struct {
	MinUsers      int           `json:"min_users"`
	MaxUsers      int           `json:"max_users"`
	Repeats       int           `json:"repeats"`
	StageDuration time.Duration `json:"-"`
	Budget        time.Duration `json:"-"`
}
type Probe struct {
	Users  int             `json:"users"`
	Status string          `json:"status"`
	Runs   []report.Result `json:"runs"`
}
type Result struct {
	SchemaVersion       int      `json:"schema_version"`
	Scenario            string   `json:"scenario"`
	Target              string   `json:"target"`
	StartedAt           string   `json:"started_at"`
	StageDuration       string   `json:"stage_duration"`
	Repeats             int      `json:"repeats"`
	MinUsers            int      `json:"min_users"`
	MaxUsers            int      `json:"max_users"`
	HighestPassingUsers *int     `json:"highest_passing_tested_users,omitempty"`
	FirstFailingUsers   *int     `json:"first_failing_tested_users,omitempty"`
	Outcome             string   `json:"outcome"`
	Probes              []Probe  `json:"probes"`
	Notes               []string `json:"notes"`
}

type Runner func(context.Context, config.Validated) report.Result

func Validate(o Options) error {
	if o.MinUsers < 1 || o.MaxUsers > 50 || o.MinUsers > o.MaxUsers {
		return fmt.Errorf("min-users must be at least 1 and max-users at most 50, with min <= max")
	}
	if o.Repeats < 2 || o.Repeats > 4 {
		return fmt.Errorf("repeats must be between 2 and 4 for reliable confirmation")
	}
	if o.Budget != 0 && (o.Budget < o.StageDuration || o.Budget > 15*time.Minute) {
		return fmt.Errorf("budget must cover at least one stage and be at most 15m")
	}
	if o.StageDuration < 10*time.Second || o.StageDuration > 3*time.Minute {
		return fmt.Errorf("stage-duration must be between 10s and 3m")
	}
	return nil
}

func Run(ctx context.Context, base config.Validated, opt Options) (Result, error) {
	return WithRunner(ctx, base, opt, engine.Run)
}

// WithRunner uses dependency injection to make search logic testable without network traffic.
func WithRunner(ctx context.Context, base config.Validated, opt Options, runner Runner) (Result, error) {
	if err := Validate(opt); err != nil {
		return Result{}, err
	}
	if opt.Budget == 0 {
		opt.Budget = 10 * time.Minute
	}
	ctx, stop := context.WithTimeout(ctx, opt.Budget)
	defer stop()
	if base.Scenario.Mode != "closed" {
		return Result{}, fmt.Errorf("explore requires closed-loop mode; arrival mode has a different capacity unit")
	}
	var cookies []string
	if base.Scenario.PerVUCookieEnvPrefix != "" {
		var err error
		cookies, err = config.ResolveSessions(base.Scenario.PerVUCookieEnvPrefix, opt.MaxUsers)
		if err != nil {
			return Result{}, err
		}
	}
	started := time.Now()
	result := Result{SchemaVersion: 1, Scenario: base.Scenario.Name, Target: base.Base.Scheme + "://" + base.Base.Host, StartedAt: started.UTC().Format(time.RFC3339), StageDuration: opt.StageDuration.String(), Repeats: opt.Repeats, MinUsers: opt.MinUsers, MaxUsers: opt.MaxUsers, Outcome: "INCONCLUSIVE", Notes: []string{
		"The highest repeatedly passing tested level is not a universal server capacity. It applies only to this HTTP workload, traffic cap, deployment, and service-level objectives.",
		"No server CPU, RAM, database, or generator resource telemetry is collected by this command; root cause and real-user capacity remain unverified.",
	}}
	probe := func(users int) string {
		p := Probe{Users: users, Status: "PASS"}
		for k := 0; k < opt.Repeats; k++ {
			if ctx.Err() != nil {
				p.Status = "ABORTED"
				break
			}
			v := base
			v.Scenario.Users = users
			v.Scenario.Duration = opt.StageDuration.String()
			v.Duration = opt.StageDuration
			if v.RampUp > v.Duration/3 {
				v.RampUp = v.Duration / 3
				v.Scenario.RampUp = v.RampUp.String()
			}
			if len(cookies) > 0 {
				v.Cookies = cookies[:users]
			}
			r := runner(ctx, v)
			p.Runs = append(p.Runs, r)
			if r.Outcome != "PASS" {
				p.Status = "FAIL"
				if r.Outcome == "ABORTED" || r.Outcome == "INCONCLUSIVE" {
					p.Status = r.Outcome
				}
				break
			}
			for _, w := range r.Warnings {
				if strings.Contains(w, "near max_rps") {
					p.Status = "INCONCLUSIVE"
					break
				}
			}
			if p.Status == "INCONCLUSIVE" {
				break
			}
		}
		result.Probes = append(result.Probes, p)
		return p.Status
	}
	low := 0
	high := 0
	candidate := opt.MinUsers
	for {
		status := probe(candidate)
		if status == "ABORTED" || status == "INCONCLUSIVE" {
			result.Outcome = status
			return result, nil
		}
		if status == "PASS" {
			low = candidate
			if candidate == opt.MaxUsers {
				result.HighestPassingUsers = &low
				result.Outcome = "PASSED_TO_CONFIGURED_CEILING"
				return result, nil
			}
			next := candidate * 2
			if next <= candidate {
				next = candidate + 1
			}
			if next > opt.MaxUsers {
				next = opt.MaxUsers
			}
			candidate = next
			continue
		}
		high = candidate
		if low == 0 {
			result.FirstFailingUsers = &high
			result.Outcome = "NO_PASSING_LEVEL"
			return result, nil
		}
		break
	}
	for high-low > 1 {
		if ctx.Err() != nil {
			result.Outcome = "ABORTED"
			return result, nil
		}
		mid := low + (high-low)/2
		status := probe(mid)
		if status == "ABORTED" || status == "INCONCLUSIVE" {
			result.Outcome = status
			return result, nil
		}
		if status == "PASS" {
			low = mid
		} else {
			high = mid
		}
	}
	result.HighestPassingUsers = &low
	result.FirstFailingUsers = &high
	result.Outcome = "BRACKETED"
	return result, nil
}
