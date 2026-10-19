// Package arrivalexplorer probes bounded open-loop journey-start rates.
// Its rates are offered journey starts per second, never VUs or HTTP requests/s.
package arrivalexplorer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const generatorUtilizationGuard = 0.90

type Options struct {
	MinJourneysPerSecond    int
	MaxJourneysPerSecond    int
	TargetJourneysPerSecond int
	Repeats                 int
	StageDuration           time.Duration
	Budget                  time.Duration
}

type Probe struct {
	JourneysPerSecond int             `json:"scheduled_journey_starts_per_second"`
	Status            string          `json:"status"`
	Reason            string          `json:"reason,omitempty"`
	Runs              []report.Result `json:"runs"`
}

type Result struct {
	SchemaVersion                   int      `json:"schema_version"`
	Scenario                        string   `json:"scenario"`
	Target                          string   `json:"target"`
	StartedAt                       string   `json:"started_at"`
	StageDuration                   string   `json:"stage_duration"`
	Repeats                         int      `json:"repeats"`
	WorkerLimit                     int      `json:"journey_worker_limit"`
	MinJourneysPerSecond            int      `json:"min_scheduled_journey_starts_per_second"`
	MaxJourneysPerSecond            int      `json:"max_scheduled_journey_starts_per_second"`
	TargetJourneysPerSecond         int      `json:"exact_target_scheduled_starts_per_second,omitempty"`
	TargetTested                    bool     `json:"exact_target_tested,omitempty"`
	HighestPassingJourneysPerSecond *int     `json:"highest_repeatedly_passing_tested_starts_per_second,omitempty"`
	FirstFailingJourneysPerSecond   *int     `json:"first_observed_failing_tested_starts_per_second,omitempty"`
	Outcome                         string   `json:"outcome"`
	Reason                          string   `json:"reason,omitempty"`
	Probes                          []Probe  `json:"probes"`
	Notes                           []string `json:"notes"`
}

type Runner func(context.Context, config.Validated) report.Result

func Validate(o Options, s config.Scenario) error {
	if s.Mode != "arrival" {
		return fmt.Errorf("arrival exploration requires mode=arrival; closed-loop VUs use damocline explore")
	}
	if o.MinJourneysPerSecond < 1 || o.MaxJourneysPerSecond > 100 || o.MinJourneysPerSecond > o.MaxJourneysPerSecond {
		return fmt.Errorf("journey-start rate bounds must be 1..100 starts/s with min <= max")
	}
	if o.TargetJourneysPerSecond < 0 {
		return fmt.Errorf("target journey-start rate cannot be negative")
	}
	if o.Repeats < 2 || o.Repeats > 4 {
		return fmt.Errorf("repeats must be between 2 and 4")
	}
	if o.StageDuration < 10*time.Second || o.StageDuration > 3*time.Minute {
		return fmt.Errorf("stage-duration must be between 10s and 3m")
	}
	if o.Budget != 0 && (o.Budget < o.StageDuration+30*time.Second || o.Budget > 15*time.Minute) {
		return fmt.Errorf("budget must cover one stage plus the engine's maximum bounded drain allowance and be at most 15m")
	}
	maxSteps := 0
	if len(s.Profiles) > 0 {
		for _, profile := range s.Profiles {
			if len(profile.Steps) > maxSteps {
				maxSteps = len(profile.Steps)
			}
		}
	} else {
		maxSteps = len(s.Steps)
	}
	if maxSteps == 0 || o.MaxJourneysPerSecond*maxSteps > s.MaxRPS {
		return fmt.Errorf("maximum scheduled starts/s multiplied by the longest journey exceeds max_rps")
	}
	return nil
}

func Run(ctx context.Context, base config.Validated, opt Options) (Result, error) {
	return WithRunner(ctx, base, opt, engine.Run)
}

// WithRunner keeps search behavior testable without making network requests.
func WithRunner(parent context.Context, base config.Validated, opt Options, runner Runner) (Result, error) {
	if err := Validate(opt, base.Scenario); err != nil {
		return Result{}, err
	}
	if opt.Budget == 0 {
		opt.Budget = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, opt.Budget)
	defer cancel()
	started := time.Now()
	result := Result{
		SchemaVersion: 1, Scenario: base.Scenario.Name,
		Target:    base.Base.Scheme + "://" + base.Base.Host,
		StartedAt: started.UTC().Format(time.RFC3339), StageDuration: opt.StageDuration.String(),
		Repeats: opt.Repeats, WorkerLimit: base.Scenario.Users,
		MinJourneysPerSecond: opt.MinJourneysPerSecond, MaxJourneysPerSecond: opt.MaxJourneysPerSecond,
		TargetJourneysPerSecond: opt.TargetJourneysPerSecond,
		Outcome:                 "INCONCLUSIVE", Probes: []Probe{}, Notes: []string{
			"Probe levels are scheduled open-loop journey starts per second; they are not virtual users, admitted journeys, successful journeys, or HTTP requests per second.",
			"A bracket describes only the tested workload, worker limit, request-rate ceiling, deployment, duration, and configured SLOs. It is not a universal capacity limit or a sustainable-capacity certification.",
			"Dropped/cancelled starts, generator pressure, or a binding client request-rate ceiling make a probe inconclusive rather than an application failure.",
		},
	}

	probe := func(rate int) string {
		p := Probe{JourneysPerSecond: rate, Status: "PASS_OBSERVED", Runs: []report.Result{}}
		for repeat := 0; repeat < opt.Repeats; repeat++ {
			if ctx.Err() != nil {
				p.Status = "ABORTED"
				p.Reason = abortReason(ctx.Err())
				break
			}
			v := base
			v.Scenario.Mode = "arrival"
			v.Scenario.ArrivalRate = rate
			v.Scenario.Duration = opt.StageDuration.String()
			v.Scenario.RampUp = "0s"
			v.Duration = opt.StageDuration
			v.RampUp = 0
			run := runner(ctx, v)
			p.Runs = append(p.Runs, run)
			status, reason := classify(run, v)
			if status != "PASS_OBSERVED" {
				p.Status = status
				p.Reason = reason
				break
			}
		}
		result.Probes = append(result.Probes, p)
		if rate == opt.TargetJourneysPerSecond && opt.TargetJourneysPerSecond > 0 {
			result.TargetTested = true
		}
		return p.Status
	}

	low, high := 0, 0
	candidate := opt.MinJourneysPerSecond
	for {
		status := probe(candidate)
		if status == "ABORTED" || status == "INVALID_DELIVERY" || status == "INSUFFICIENT_EVIDENCE" {
			result.Outcome = status
			result.Reason = result.Probes[len(result.Probes)-1].Reason
			return result, nil
		}
		if status == "FAIL_OBSERVED" {
			high = candidate
			if low == 0 {
				result.FirstFailingJourneysPerSecond = &high
				result.Outcome = "NO_PASSING_RATE"
				return result, nil
			}
			break
		}
		low = candidate
		if candidate == opt.MaxJourneysPerSecond {
			result.HighestPassingJourneysPerSecond = &low
			result.Outcome = "PASSED_TO_CONFIGURED_CEILING"
			return result, nil
		}
		next := min(candidate*2, opt.MaxJourneysPerSecond)
		if opt.TargetJourneysPerSecond > candidate && opt.TargetJourneysPerSecond < next && opt.TargetJourneysPerSecond <= opt.MaxJourneysPerSecond {
			next = opt.TargetJourneysPerSecond
		}
		candidate = next
	}

	for high-low > 1 {
		if ctx.Err() != nil {
			result.Outcome = "ABORTED"
			result.Reason = abortReason(ctx.Err())
			return result, nil
		}
		mid := low + (high-low)/2
		status := probe(mid)
		if status == "ABORTED" || status == "INVALID_DELIVERY" || status == "INSUFFICIENT_EVIDENCE" {
			result.Outcome = status
			result.Reason = result.Probes[len(result.Probes)-1].Reason
			return result, nil
		}
		if status == "PASS_OBSERVED" {
			low = mid
		} else {
			high = mid
		}
	}
	result.HighestPassingJourneysPerSecond = &low
	result.FirstFailingJourneysPerSecond = &high
	result.Outcome = "BRACKETED"
	return result, nil
}

func classify(r report.Result, scenario config.Validated) (string, string) {
	if r.Outcome == "ABORTED" {
		return "ABORTED", "The native run was interrupted or exhausted its wall-time budget."
	}
	if r.Quality != nil && r.Quality.State == report.QualityInvalid || r.SchemaVersion >= 16 && report.AssessQuality(r).State == report.QualityInvalid {
		return "INVALID_DELIVERY", "Report accounting or generator evidence is invalid."
	}
	if r.ArrivalsDropped > 0 || r.ArrivalsDroppedLate > 0 || r.ArrivalsDroppedBusy > 0 ||
		r.ArrivalsCancelledBeforeAdmission != nil && *r.ArrivalsCancelledBeforeAdmission > 0 ||
		r.Metrics.Cancelled > 0 || r.Journeys.Cancelled > 0 {
		return "INVALID_DELIVERY", "Scheduled starts or configured journeys were dropped or cancelled; offered workload was not fully delivered."
	}
	if r.Metrics.Total <= 0 || r.Config.MaxRPS <= 0 || r.WindowSeconds() <= 0 || r.Generator == nil ||
		r.Generator.Samples < 2 || !r.Generator.GoCPUValid || r.Generator.ProcessSamples < 2 ||
		!r.Generator.ProcessCPUValid || r.Generator.PeakProcessRSSBytes == 0 {
		return "INSUFFICIENT_EVIDENCE", "The probe lacks request, duration, or generator process-health evidence."
	}
	if r.Generator.GoCPUUtilizationRatio >= generatorUtilizationGuard ||
		r.Generator.MeanProcessCPUUtilizationRatio >= generatorUtilizationGuard {
		return "INVALID_DELIVERY", "Generator CPU reached the conservative utilization guardrail."
	}
	if float64(r.Metrics.Total)/r.WindowSeconds() >= generatorUtilizationGuard*float64(r.Config.MaxRPS) {
		return "INVALID_DELIVERY", "Observed HTTP request rate reached the configured client rate-ceiling guardrail."
	}
	for _, warning := range r.Warnings {
		if strings.Contains(warning, "may share one login identity") && hasExplicitSharedCookie(scenario) {
			return "INSUFFICIENT_EVIDENCE", "The configured workers may share one authenticated identity; independent session coverage is not established."
		}
	}
	if reason := sampleCoverageIssue(r, scenario); reason != "" {
		return "INSUFFICIENT_EVIDENCE", reason
	}
	state, reason := campaign.ClassifyEvidence(r)
	return state, reason
}

func sampleCoverageIssue(r report.Result, scenario config.Validated) string {
	minimum := r.Config.MinSamplesPerStep
	if minimum < 1 {
		return "The run does not declare a valid minimum sample requirement."
	}
	if r.Metrics.Total < minimum || r.Metrics.Success == 0 || r.Journeys.Total == 0 {
		return "The probe lacks minimum request or completed-journey samples."
	}
	expectedSteps := make(map[string]bool)
	expectedWindowSteps := make(map[string]bool)
	expectedProfiles := make(map[string]bool, len(scenario.Profiles))
	if len(scenario.Scenario.Profiles) > 0 {
		for _, profile := range scenario.Profiles {
			expectedProfiles[profile.Name] = false
			for _, step := range profile.Steps {
				expectedSteps[profile.Name+"\x00"+step.Name] = false
				expectedWindowSteps[step.Name] = false
			}
		}
	} else {
		for _, step := range scenario.Steps {
			expectedSteps["\x00"+step.Name] = false
			expectedWindowSteps[step.Name] = false
		}
	}
	for _, step := range r.Steps {
		key := step.Profile + "\x00" + step.Name
		if _, ok := expectedSteps[key]; !ok || expectedSteps[key] {
			return "The probe has duplicate or unexpected named-step evidence."
		}
		expectedSteps[key] = true
		if step.Metrics.Total < minimum || step.Metrics.Success == 0 {
			return "A workflow step lacks the configured minimum successful samples."
		}
	}
	for _, observed := range expectedSteps {
		if !observed {
			return "The probe is missing named-step evidence for the configured workflow."
		}
	}
	for _, profile := range r.Profiles {
		observed, ok := expectedProfiles[profile.Name]
		if !ok || observed {
			return "The probe has duplicate or unexpected workflow-profile evidence."
		}
		expectedProfiles[profile.Name] = true
		if profile.Journeys.Total < minimum || profile.Journeys.Success == 0 {
			return "A workflow profile lacks the configured minimum successful journey samples."
		}
	}
	for _, observed := range expectedProfiles {
		if !observed {
			return "The probe is missing workflow-profile evidence."
		}
	}
	for _, window := range r.Windows {
		if window.Metrics.Total < minimum || window.Journeys.Total == 0 {
			return "An observation window lacks the configured minimum request or journey samples."
		}
		windowSteps := make(map[string]bool, len(scenario.Steps))
		for _, step := range window.Steps {
			if _, ok := expectedWindowSteps[step.Name]; !ok || windowSteps[step.Name] {
				return "An observation window has duplicate or unexpected named-step evidence."
			}
			windowSteps[step.Name] = true
			if step.Metrics.Total < minimum || step.Metrics.Success == 0 {
				return "A workflow step observation window lacks the configured minimum successful samples."
			}
		}
		if len(windowSteps) != len(expectedSteps) {
			return "An observation window is missing named-step evidence."
		}
		windowProfiles := make(map[string]bool, len(scenario.Profiles))
		for _, profile := range window.Profiles {
			if _, ok := expectedProfiles[profile.Name]; !ok || windowProfiles[profile.Name] {
				return "An observation window has duplicate or unexpected workflow-profile evidence."
			}
			windowProfiles[profile.Name] = true
			if profile.Metrics.Total < minimum || profile.Metrics.Success == 0 {
				return "A workflow profile observation window lacks the configured minimum successful journeys."
			}
		}
		if len(windowProfiles) != len(expectedProfiles) {
			return "An observation window is missing workflow-profile evidence."
		}
	}
	return ""
}

func hasExplicitSharedCookie(scenario config.Validated) bool {
	if len(scenario.Cookies) > 0 {
		return false
	}
	for _, step := range scenario.Steps {
		for name := range step.ResolvedHeaders {
			if strings.EqualFold(name, "Cookie") {
				return true
			}
		}
	}
	return false
}

func abortReason(err error) string {
	if err == context.DeadlineExceeded {
		return "Arrival-rate exploration wall-time budget expired before the probe completed."
	}
	return "Arrival-rate exploration was interrupted by its caller."
}
