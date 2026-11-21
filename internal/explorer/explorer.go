// Package explorer probes bounded virtual-user levels using Damocline's native engine.
// It reports the highest repeatedly passing *tested* level, not a universal server limit.
package explorer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// ValidateArtifactJSON rejects omitted persisted exploration fields before
// decoding can turn them into Go zero values. Explicit zeros and empty arrays
// remain valid evidence where the schema permits them.
func ValidateArtifactJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("exploration must be a JSON object")
	}
	version, ok := fields["schema_version"]
	var schema int
	if !ok || json.Unmarshal(version, &schema) != nil || schema < 1 || schema > CurrentSchemaVersion {
		return errors.New("exploration has a missing or unsupported schema_version")
	}
	required := []string{"scenario", "target", "started_at", "stage_duration", "repeats", "min_users", "max_users", "outcome", "probes", "notes"}
	if schema >= EnvelopeSchemaVersion {
		required = append(required, "load_envelope")
	} else if _, exists := fields["load_envelope"]; exists {
		return errors.New("exploration load_envelope is unsupported by this schema")
	}
	for _, name := range required {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("exploration is missing required field %q; absence cannot be interpreted as zero", name)
		}
	}
	var probes []json.RawMessage
	if err := json.Unmarshal(fields["probes"], &probes); err != nil || probes == nil || !bytes.HasPrefix(bytes.TrimSpace(fields["probes"]), []byte("[")) {
		return errors.New("exploration probes must be a JSON array")
	}
	for i, raw := range probes {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
			return fmt.Errorf("exploration probe %d must be a JSON object", i+1)
		}
		for _, name := range []string{"users", "status", "runs"} {
			value, exists := probe[name]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("exploration probe %d is missing required field %q", i+1, name)
			}
		}
		var runs []json.RawMessage
		if err := json.Unmarshal(probe["runs"], &runs); err != nil || runs == nil || !bytes.HasPrefix(bytes.TrimSpace(probe["runs"]), []byte("[")) {
			return fmt.Errorf("exploration probe %d runs must be a JSON array", i+1)
		}
		for j, run := range runs {
			var reportFields map[string]json.RawMessage
			if err := json.Unmarshal(run, &reportFields); err != nil || reportFields == nil {
				return fmt.Errorf("exploration probe %d run %d must be a JSON object", i+1, j+1)
			}
			if value, exists := reportFields["schema_version"]; !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("exploration probe %d run %d is missing required field %q", i+1, j+1, "schema_version")
			}
		}
	}
	return nil
}

// CurrentSchemaVersion is the newest exploration schema readers accept.
// Schema 3 records the load envelope and is written only by extended-envelope
// explorations (ADR 0157); standard explorations keep writing schema 2.
const CurrentSchemaVersion = 3

// EnvelopeSchemaVersion is the first exploration schema with load_envelope.
const EnvelopeSchemaVersion = 3

const standardSchemaVersion = 2

type Options struct {
	MinUsers      int           `json:"min_users"`
	MaxUsers      int           `json:"max_users"`
	TargetUsers   int           `json:"target_users,omitempty"`
	Repeats       int           `json:"repeats"`
	StageDuration time.Duration `json:"-"`
	Budget        time.Duration `json:"-"`
	// UserCeiling is the inclusive VU ceiling; zero means the standard 50
	// VUs. Only the CLI extended-envelope gate raises it (ADR 0157).
	UserCeiling int `json:"-"`
	// Extended and TargetClass record the validated extended envelope.
	Extended    bool   `json:"-"`
	TargetClass string `json:"-"`
	// LevelCovered, when set, is consulted before every bracket refinement
	// level. A level it rejects is never probed (ADR 0157).
	LevelCovered func(users int) bool `json:"-"`
}
type Probe struct {
	Users  int             `json:"users"`
	Status string          `json:"status"`
	Reason string          `json:"reason,omitempty"`
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
	Reason              string   `json:"reason,omitempty"`
	Probes              []Probe  `json:"probes"`
	Notes               []string `json:"notes"`
	// LoadEnvelope records the ceilings that applied (schema 3+).
	LoadEnvelope *report.Envelope `json:"load_envelope,omitempty"`
}

// ValidateEnvelope checks that saved evidence stays inside the envelope it
// declares and that every embedded run agrees with it.
func ValidateEnvelope(r Result) error {
	if r.SchemaVersion < EnvelopeSchemaVersion {
		if r.LoadEnvelope != nil {
			return errors.New("load envelope is unsupported by this exploration schema")
		}
		if r.MaxUsers > report.StandardMaxUsers {
			return errors.New("exploration exceeds the standard 50 VU ceiling without a recorded extended envelope")
		}
		return nil
	}
	if !report.ValidAggregateEnvelope(r.LoadEnvelope, r.MaxUsers) {
		return errors.New("exploration load envelope is missing, inconsistent, or exceeded")
	}
	for _, p := range r.Probes {
		for _, run := range p.Runs {
			if !report.RunMatchesAggregateEnvelope(r.LoadEnvelope, run) {
				return fmt.Errorf("probe %d VUs contains a run whose load envelope differs from the exploration", p.Users)
			}
		}
	}
	return nil
}

func userCeiling(o Options) int {
	if o.UserCeiling == 0 {
		return report.StandardMaxUsers
	}
	return o.UserCeiling
}

type Runner func(context.Context, config.Validated) report.Result

func Validate(o Options) error {
	ceiling := userCeiling(o)
	if ceiling != report.StandardMaxUsers && (ceiling != report.ExtendedMaxUsers || !o.Extended) {
		return fmt.Errorf("only the validated extended envelope may raise the exploration ceiling")
	}
	if o.MinUsers < 1 || o.MaxUsers > ceiling || o.MinUsers > o.MaxUsers {
		return fmt.Errorf("min-users must be at least 1 and max-users at most %d, with min <= max", ceiling)
	}

	if o.TargetUsers < 0 {
		return fmt.Errorf("target users cannot be negative")
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
	if opt.Extended && opt.LevelCovered == nil {
		return Result{}, fmt.Errorf("extended exploration requires a calibration coverage check for refinement levels")
	}
	if opt.Extended != (base.Envelope.Mode == config.EnvelopeExtended) || opt.Extended && (opt.TargetClass != base.Envelope.TargetClass || base.Envelope.CalibrationCoverage != "READY") {
		return Result{}, fmt.Errorf("exploration envelope options do not match the validated scenario envelope")
	}
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
	result := Result{SchemaVersion: standardSchemaVersion, Scenario: base.Scenario.Name, Target: base.Base.Scheme + "://" + base.Base.Host, StartedAt: started.UTC().Format(time.RFC3339), StageDuration: opt.StageDuration.String(), Repeats: opt.Repeats, MinUsers: opt.MinUsers, MaxUsers: opt.MaxUsers, Outcome: "INCONCLUSIVE", Notes: []string{
		"The highest repeatedly passing tested level is not a universal server capacity. It applies only to this HTTP workload, traffic cap, deployment, and service-level objectives.",
		"The generator reports Go runtime heap, goroutines, GC, and partial Go CPU activity per trial; full process metrics, server CPU/RAM, databases, and root causes remain unverified.",
	}}
	if opt.Extended {
		result.SchemaVersion = EnvelopeSchemaVersion
		result.LoadEnvelope = report.NewAggregateEnvelope(true, opt.TargetClass, opt.MaxUsers)
		result.Notes = append(result.Notes, report.AggregateEnvelopeNote(result.LoadEnvelope))
	}
	probe := func(users int) string {
		p := Probe{Users: users, Status: "PASS"}
		for k := 0; k < opt.Repeats; k++ {
			if ctx.Err() != nil {
				p.Status = "ABORTED"
				p.Reason = abortReason(ctx.Err())
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
				if opt.Extended {
					v.WorkloadFingerprint = config.WorkloadFingerprint(v)
				}
			}
			r := runner(ctx, v)
			reason := deliveryIssue(&r)
			p.Runs = append(p.Runs, r)
			// A generator guardrail stop describes the load generator, never
			// the target, so the probe cannot pass or fail (ADR 0157).
			if r.Envelope != nil && r.Envelope.StopReason == report.EnvelopeStopGeneratorLimited {
				p.Status = "INCONCLUSIVE"
				p.Reason = GeneratorLimitedReason
				break
			}
			if r.Outcome == "ABORTED" {
				p.Status = "ABORTED"
				p.Reason = abortReason(ctx.Err())
				if ctx.Err() == nil {
					p.Reason = "The native run reported ABORTED before the stage completed."
				}
				break
			}
			if reason != "" {
				p.Status = "INCONCLUSIVE"
				p.Reason = reason
				break
			}
			// A failed, unstable, or objective-dominating network-path
			// baseline (ADR 0153) prevents reporting an application limit.
			if r.Outcome != "PASS" {
				if issue := report.NetworkBaselineIssue(r); issue != "" {
					p.Status = "INCONCLUSIVE"
					p.Reason = issue
					break
				}
			}
			if r.Outcome == "FAIL" && (r.SchemaVersion >= 16 || r.Generator != nil) {
				if reason := report.GeneratorEvidenceIssue(r); reason != "" {
					p.Status = "INCONCLUSIVE"
					p.Reason = reason
					break
				}
			}
			if r.Outcome != "PASS" {
				p.Status = "FAIL"
				if r.Outcome == "INCONCLUSIVE" {
					p.Status = r.Outcome
					p.Reason = "The stage did not produce decisive evidence."
				} else {
					p.Reason = "The configured application objective failed."
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
				p.Reason = "The configured request-rate ceiling may be limiting the workload."
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
			stopUnresolved(&result, status, candidate, low)
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
			// A business capacity target must be exercised at its exact
			// workload level instead of interpolating from nearby probes.
			// Preserve the normal bounded exploration when the target is
			// outside the configured range or cannot be reached safely.
			if opt.TargetUsers > candidate && opt.TargetUsers < next &&
				opt.TargetUsers <= opt.MaxUsers {
				next = opt.TargetUsers
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
			result.Reason = abortReason(ctx.Err())
			return result, nil
		}
		mid, ok := refinementLevel(low, high, opt.LevelCovered)
		if !ok {
			result.Outcome = "INCONCLUSIVE"
			result.Reason = fmt.Sprintf("%s Bracket %d-%d VUs (highest passing tested %d, first failing tested %d) was not refined further.", UncalibratedRefinementReason, low, high, low, high)
			return result, nil
		}
		status := probe(mid)
		if status == "ABORTED" || status == "INCONCLUSIVE" {
			stopUnresolved(&result, status, mid, low)
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

// stopUnresolved ends an exploration whose probe at users was ABORTED or
// INCONCLUSIVE. Levels that already passed every repeat stay recorded as the
// highest repeatedly passing level, and a note states that the stopped level
// and everything above it are unresolved and why.
func stopUnresolved(result *Result, status string, users, low int) {
	result.Outcome = status
	result.Reason = result.Probes[len(result.Probes)-1].Reason
	if low < 1 {
		return
	}
	passing := low
	result.HighestPassingUsers = &passing
	result.Notes = append(result.Notes, UnresolvedNote(users, result.Reason))
}

// UnresolvedNote states that a stopped probe level and the levels above it
// were not resolved by exploration.
func UnresolvedNote(users int, reason string) string {
	return fmt.Sprintf("Levels from %d VUs upward are unresolved: the %d VU probe ended without a target verdict (%s)", users, users, strings.TrimSpace(reason))
}

// GeneratorLimitedReason explains a probe stopped by a generator guardrail.
const GeneratorLimitedReason = "GENERATOR_LIMITED: a generator CPU or RSS guardrail stopped the trial; this describes the load generator, not the target, and no application limit is concluded."

// UncalibratedRefinementReason prefixes an extended exploration whose
// remaining bracket has no calibrated level.
const UncalibratedRefinementReason = "Bracket refinement stopped: no level strictly inside the failing bracket has READY extended-envelope calibration coverage, so no uncalibrated level was probed."

// refinementLevel picks the calibrated level strictly inside (low, high)
// closest to the midpoint, preferring the lower one on ties. Without a
// coverage check every level is eligible and the midpoint is returned.
func refinementLevel(low, high int, covered func(int) bool) (int, bool) {
	mid := low + (high-low)/2
	if covered == nil {
		return mid, true
	}
	for d := 0; mid-d > low || mid+d < high; d++ {
		if mid-d > low && covered(mid-d) {
			return mid - d, true
		}
		if mid+d < high && covered(mid+d) {
			return mid + d, true
		}
	}
	return 0, false
}

func abortReason(err error) string {
	if err == context.DeadlineExceeded {
		return "Exploration wall-time budget expired before the probe completed."
	}
	return "Exploration was interrupted by its caller."
}

func deliveryIssue(run *report.Result) string {
	reportedInvalid := run.Quality != nil && run.Quality.State == report.QualityInvalid
	if run.SchemaVersion >= 16 && (!reportedInvalid || len(run.Quality.Findings) == 0) {
		if assessment := report.AssessQuality(*run); assessment.State == report.QualityInvalid {
			run.Quality = assessment
			return "Current-schema report fails recomputed delivery or generator-accounting checks."
		}
	}
	if reportedInvalid {
		return "Generator or report accounting is invalid; this probe cannot be treated as an application failure or pass."
	}
	if run.ArrivalsDropped > 0 || run.ArrivalsDroppedLate > 0 || run.ArrivalsDroppedBusy > 0 ||
		run.ArrivalsCancelledBeforeAdmission != nil && *run.ArrivalsCancelledBeforeAdmission > 0 ||
		run.Metrics.Cancelled > 0 || run.Journeys.Cancelled > 0 {
		return "Planned arrivals or configured journeys were dropped or cancelled; delivered workload is incomplete."
	}
	return ""
}

// PlannedLevels returns the geometric VU ladder the explorer probes before
// any bracket refinement, including an in-range exact target. Refinement
// levels inside a failing bracket are data-dependent and not included.
func PlannedLevels(opt Options) []int {
	levels := []int{}
	candidate := opt.MinUsers
	for candidate > 0 {
		levels = append(levels, candidate)
		if candidate >= opt.MaxUsers {
			break
		}
		next := candidate * 2
		if next > opt.MaxUsers {
			next = opt.MaxUsers
		}
		if opt.TargetUsers > candidate && opt.TargetUsers < next && opt.TargetUsers <= opt.MaxUsers {
			next = opt.TargetUsers
		}
		candidate = next
	}
	return levels
}
