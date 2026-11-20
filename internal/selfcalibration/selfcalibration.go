// Package selfcalibration replays one declared scenario shape against a
// bounded loopback mirror on the generator host and records whether the
// generator delivered it (ADR 0154). It never contacts the scenario target.
package selfcalibration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// MinLevelDuration is a variable only so deterministic tests can shorten it.
var MinLevelDuration = 5 * time.Second

const (
	DefaultLevelDuration = 20 * time.Second
	MaxLevelDuration     = 2 * time.Minute
	// levelDrainAllowance bounds the per-level drain and setup time that the
	// wall-time plan reserves beyond each level's declared duration.
	levelDrainAllowance = 35 * time.Second
	syntheticEnvPrefix  = "DAMOCLINE_SELFCAL_"
)

// DefaultGuardrails are the generator headroom limits applied while
// calibrating when the operator does not choose stricter ones.
var DefaultGuardrails = generatorhealth.Guardrails{MaxProcessCPUPercent: 80, MaxProcessRSSBytes: 1 << 30, ConsecutiveSamples: 3}

// Runner executes one validated workload. Tests inject a deterministic one.
type Runner func(context.Context, config.Validated) report.Result

// Options controls one bounded self-calibration invocation.
type Options struct {
	// Levels are closed-loop VU levels, or open-arrival journey-start rates.
	Levels        []int
	LevelDuration time.Duration
	// Declared mirror model, used when FromReport is nil.
	ResponseBytes int
	ServerDelay   time.Duration
	// FromReport supplies per-step mirror sizes and median latency from a
	// previously recorded report of the real target. The target is never
	// contacted.
	FromReport       *report.Result
	FromReportSHA256 string
	Guardrails       generatorhealth.Guardrails
	Runner           Runner
	// ExtendedEnvelope parses the loopback mirror workload under the opt-in
	// extended envelope (ADR 0155) so levels above the standard ceilings can
	// be calibrated. Only the 127.0.0.1 mirror receives that load.
	ExtendedEnvelope bool
	Now              func() time.Time
}

// stepRef locates one prepared step and the original declaration it mirrors.
type stepRef struct {
	profile, index int
	original       config.Step
}

type plan struct {
	original     config.Scenario
	mirrorScen   config.Scenario
	refs         []stepRef
	routes       []calibration.MirrorRoute
	prefixes     []string
	notes        []string
	synthEnv     map[string]string
	bodyLengths  map[string]int
	routeForStep []int
}

func originalProfiles(s config.Scenario) []config.Profile {
	switch {
	case s.ScenarioVersion == config.CurrentScenarioVersion && len(s.Workflows) > 0:
		return s.Workflows
	case len(s.Profiles) > 0:
		return s.Profiles
	default:
		return []config.Profile{{Name: "default", Steps: s.Steps}}
	}
}

func setProfiles(s *config.Scenario, profiles []config.Profile) {
	switch {
	case s.ScenarioVersion == config.CurrentScenarioVersion && len(s.Workflows) > 0:
		s.Workflows = profiles
	case len(s.Profiles) > 0:
		s.Profiles = profiles
	default:
		s.Steps = profiles[0].Steps
	}
}

func routePrefix(basePath, path string) string {
	if i := strings.Index(path, "{{"); i >= 0 {
		path = path[:i]
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	joined := strings.TrimSuffix(basePath, "/") + "/" + strings.TrimPrefix(path, "/")
	if joined == "" {
		joined = "/"
	}
	return joined
}

// buildPlan derives the loopback mirror copy of a scenario. Environment-backed
// secrets are never read except for their byte length; the mirror receives
// synthetic values of the same length.
func buildPlan(s config.Scenario, opt Options) (*plan, error) {
	base, err := url.Parse(s.BaseURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("scenario base_url is invalid")
	}
	p := &plan{original: s, synthEnv: map[string]string{}, bodyLengths: map[string]int{}}
	envCounter := 0
	synthetic := func(realName string, fallback int) string {
		envCounter++
		name := fmt.Sprintf("%sV%d", syntheticEnvPrefix, envCounter)
		length := fallback
		if value, ok := os.LookupEnv(realName); ok && value != "" {
			length = len(value)
		}
		p.synthEnv[name] = strings.Repeat("s", length)
		return name
	}
	mirror := s
	mirror.BaseURL = "http://127.0.0.1"
	mirror.PerVUCookieEnvPrefix = ""
	mirror.ProfileCookieEnvPrefixes = nil
	mirror.BaselinePath = ""
	mirror.NetworkBaselineSkipReason = ""
	if s.SyntheticDataPrefixEnv != "" {
		envCounter++
		name := fmt.Sprintf("%sPREFIX%d", syntheticEnvPrefix, envCounter)
		p.synthEnv[name] = "dcal-synthetic"
		mirror.SyntheticDataPrefixEnv = name
	}
	profiles := originalProfiles(s)
	mirrored := make([]config.Profile, len(profiles))
	routeIndex := map[string]int{}
	for pi, profile := range profiles {
		mirrored[pi] = profile
		mirrored[pi].Steps = make([]config.Step, len(profile.Steps))
		for si, step := range profile.Steps {
			if len(step.Extract) > 0 {
				return nil, fmt.Errorf("step %q extracts response values; extraction cannot be replayed against a synthetic mirror", step.Name)
			}
			if len(step.JSONAssertions) > 0 {
				return nil, fmt.Errorf("step %q declares JSON assertions; they cannot be replayed against a synthetic mirror", step.Name)
			}
			copyStep := step
			copyStep.Headers = make(map[string]string, len(step.Headers))
			for key, value := range step.Headers {
				if match := envRef(value); match != "" {
					copyStep.Headers[key] = "${" + synthetic(match, 24) + "}"
				} else {
					copyStep.Headers[key] = value
				}
			}
			if step.BodyEnv != "" {
				value, ok := os.LookupEnv(step.BodyEnv)
				if !ok {
					return nil, fmt.Errorf("body_env %s must be set so its byte length can be replayed; its value is never sent", step.BodyEnv)
				}
				name := synthetic(step.BodyEnv, len(value))
				p.synthEnv[name] = strings.Repeat("b", len(value))
				copyStep.BodyEnv = name
			}
			mirrored[pi].Steps[si] = copyStep
			p.refs = append(p.refs, stepRef{profile: pi, index: si, original: step})
			method := strings.ToUpper(step.Method)
			if method == "" {
				method = "GET"
			}
			prefix := routePrefix(base.Path, step.Path)
			key := method + " " + prefix
			index, exists := routeIndex[key]
			bytes, delay, err := mirrorShape(profile.Name, step, method, opt)
			if err != nil {
				return nil, err
			}
			status := 200
			if len(step.ExpectedStatus) > 0 {
				status = step.ExpectedStatus[0]
			}
			if status == 204 || status == 304 || status < 200 {
				bytes = 0
			}
			if bytes < len(step.MustContain) {
				bytes = len(step.MustContain)
			}
			if !exists {
				index = len(p.routes)
				routeIndex[key] = index
				p.routes = append(p.routes, calibration.MirrorRoute{Method: method, PathPrefix: prefix, Status: status, ResponseBytes: bytes, DelayMS: delay})
				p.prefixes = append(p.prefixes, step.MustContain)
			} else if p.routes[index].Status != status || p.routes[index].ResponseBytes != bytes || p.routes[index].DelayMS != delay || p.prefixes[index] != step.MustContain {
				return nil, fmt.Errorf("steps sharing %s %s declare different mirror responses; give them distinct paths", method, prefix)
			}
			p.routeForStep = append(p.routeForStep, index)
		}
	}
	if len(p.routes) > 64 {
		return nil, errors.New("scenario has more than 64 distinct mirror routes")
	}
	setProfiles(&mirror, mirrored)
	mirror.BaseURL = "http://127.0.0.1" + strings.TrimSuffix(base.Path, "/")
	p.mirrorScen = mirror
	if envCounter > 0 {
		p.notes = append(p.notes, "environment-backed values were replaced with synthetic values of equal byte length")
	}
	p.notes = append(p.notes,
		"the mirror runs in the generator process on loopback; its CPU cost is counted against generator headroom",
		"response content is synthetic padding; only status, size, must_contain text and delay are mirrored")
	return p, nil
}

func envRef(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return value[2 : len(value)-1]
	}
	return ""
}

func mirrorShape(profile string, step config.Step, method string, opt Options) (int, float64, error) {
	if opt.FromReport == nil {
		return opt.ResponseBytes, float64(opt.ServerDelay) / float64(time.Millisecond), nil
	}
	for _, recorded := range opt.FromReport.Steps {
		recordedProfile := recorded.Profile
		if recordedProfile == "" {
			recordedProfile = "default"
		}
		if recordedProfile != profile || recorded.Name != step.Name || !strings.EqualFold(recorded.Method, method) {
			continue
		}
		body := recorded.Metrics.ResponseBody
		if body.CompleteObservations < 1 || body.TotalBytes < 0 {
			return 0, 0, fmt.Errorf("recorded report has no complete response-body observations for step %q", step.Name)
		}
		bytes := int(math.Round(float64(body.TotalBytes) / float64(body.CompleteObservations)))
		delay := recorded.Metrics.P50MS
		if bytes > calibration.MaxMirrorResponseBytes || !(delay >= 0) || delay > calibration.MaxMirrorDelayMS {
			return 0, 0, fmt.Errorf("recorded step %q response size or latency exceeds the mirror bounds", step.Name)
		}
		return bytes, math.Round(delay*1000) / 1000, nil
	}
	return 0, 0, fmt.Errorf("recorded report does not contain step %q", step.Name)
}

// Run executes every level in ascending order against a fresh loopback mirror
// and returns the artifact. A failing level stops the invocation; the
// artifact then records CHECKS_FAILED.
func Run(ctx context.Context, s config.Scenario, opt Options) (calibration.ScenarioRecord, error) {
	if opt.Runner == nil {
		opt.Runner = engine.Run
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.LevelDuration == 0 {
		opt.LevelDuration = DefaultLevelDuration
	}
	if opt.Guardrails == (generatorhealth.Guardrails{}) {
		opt.Guardrails = DefaultGuardrails
	}
	if s.Mode == "" {
		s.Mode = "closed"
	}
	if err := validateOptions(s, &opt); err != nil {
		return calibration.ScenarioRecord{}, err
	}
	p, err := buildPlan(s, opt)
	if err != nil {
		return calibration.ScenarioRecord{}, err
	}
	for name, value := range p.synthEnv {
		os.Setenv(name, value)
		defer os.Unsetenv(name)
	}
	mirror, err := StartMirror(p.routes, p.prefixes)
	if err != nil {
		return calibration.ScenarioRecord{}, fmt.Errorf("start loopback mirror: %w", err)
	}
	defer mirror.Close()
	p.mirrorScen.BaseURL = mirror.BaseURL() + strings.TrimPrefix(p.mirrorScen.BaseURL, "http://127.0.0.1")
	maxUsers := s.Users
	if s.Mode != "arrival" {
		maxUsers = opt.Levels[len(opt.Levels)-1]
	}
	mirrorScenario := p.mirrorScen
	mirrorScenario.Users = maxUsers
	if s.Mode == "arrival" {
		mirrorScenario.ArrivalRate = opt.Levels[0]
	}
	mirrorScenario.Duration = opt.LevelDuration.String()
	parseOptions := config.Options{Authorized: true, AllowWrites: true}
	if opt.ExtendedEnvelope {
		parseOptions.ExtendedEnvelope = &config.ExtendedEnvelopeRequest{Acknowledged: true, TargetClass: config.TargetClassLab}
	}
	baseValidated, err := config.Parse(mirrorScenario, parseOptions)
	if err != nil {
		return calibration.ScenarioRecord{}, fmt.Errorf("mirror scenario: %w", err)
	}
	if opt.FromReport != nil {
		users := opt.FromReport.Config.Users
		if users < 1 || users > maxUsers {
			users = maxUsers
		}
		fp, fpErr := p.fingerprint(baseValidated, users)
		if fpErr != nil || fp != opt.FromReport.Config.WorkloadFingerprint {
			return calibration.ScenarioRecord{}, errors.New("the recorded report was produced by a different workload fingerprint; mirror sizes cannot be reused")
		}
	}
	started := opt.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, calibration.ScenarioCalibrationMaxWallTime)
	defer cancel()
	record := calibration.ScenarioRecord{
		SchemaVersion: calibration.ScenarioCalibrationSchemaVersion, Kind: calibration.ScenarioCalibrationKind,
		StartedAt: started.Format(time.RFC3339Nano), NetworkMode: calibration.ScenarioCalibrationNetworkMode,
		Mirror:     calibration.MirrorModel{Source: "declared", Routes: p.routes, ReplayNotes: p.notes},
		Guardrails: opt.Guardrails, Interpretation: calibration.ScenarioInterpretation,
	}
	if opt.FromReport != nil {
		record.Mirror.Source = "recorded_report"
		record.Mirror.SourceSHA256 = opt.FromReportSHA256
	}
	status := calibration.ScenarioCalibrationPassed
	for _, level := range opt.Levels {
		v, levelErr := p.levelValidated(baseValidated, s.Mode, level, opt)
		if levelErr != nil {
			return calibration.ScenarioRecord{}, levelErr
		}
		if record.Controls.Mode == "" {
			record.Controls = controlsFor(v)
		}
		mirror.Reset()
		result := opt.Runner(ctx, v)
		ledger := mirror.Snapshot()
		if record.Generator.OS == "" {
			record.Generator = generatorFor(result)
		}
		entry := levelEvidence(v, result, ledger, p)
		entry.FailedChecks = calibration.EvaluateScenarioLevel(record.Controls, entry, opt.Guardrails)
		if ledger.Unmatched > 0 {
			entry.FailedChecks = append(entry.FailedChecks, "mirror_routes_matched")
		}
		entry.Status = calibration.ScenarioCalibrationPassed
		if len(entry.FailedChecks) > 0 {
			entry.Status = calibration.ScenarioCalibrationFailed
			status = calibration.ScenarioCalibrationFailed
		}
		record.Levels = append(record.Levels, entry)
		if status != calibration.ScenarioCalibrationPassed || ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil && status == calibration.ScenarioCalibrationPassed && len(record.Levels) < len(opt.Levels) {
		status = calibration.ScenarioCalibrationFailed
	}
	record.Status = status
	record.FinishedAt = opt.Now().UTC().Format(time.RFC3339Nano)
	if err := calibration.ValidateScenarioRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

// PlannedWallTime is the conservative wall time reserved for the levels.
func PlannedWallTime(levels int, duration time.Duration) time.Duration {
	return time.Duration(levels) * (duration + levelDrainAllowance)
}

func validateOptions(s config.Scenario, opt *Options) error {
	if len(opt.Levels) == 0 || len(opt.Levels) > calibration.ScenarioCalibrationMaxLevels {
		return fmt.Errorf("between 1 and %d calibration levels are required", calibration.ScenarioCalibrationMaxLevels)
	}
	levels := append([]int(nil), opt.Levels...)
	sort.Ints(levels)
	for i, level := range levels {
		if level < 1 || level > 1000 || (i > 0 && levels[i-1] == level) {
			return errors.New("calibration levels must be distinct integers from 1 to 1000")
		}
	}
	opt.Levels = levels
	if opt.LevelDuration < MinLevelDuration || opt.LevelDuration > MaxLevelDuration {
		return fmt.Errorf("level duration must be between %s and %s", MinLevelDuration, MaxLevelDuration)
	}
	if PlannedWallTime(len(levels), opt.LevelDuration) > calibration.ScenarioCalibrationMaxWallTime {
		return fmt.Errorf("planned levels exceed the %s self-calibration wall-time cap", calibration.ScenarioCalibrationMaxWallTime)
	}
	if !opt.Guardrails.Validate() || !opt.Guardrails.Enabled() {
		return errors.New("generator guardrails must be enabled and valid")
	}
	if opt.FromReport == nil {
		if opt.ResponseBytes < 0 || opt.ResponseBytes > calibration.MaxMirrorResponseBytes ||
			opt.ServerDelay < 0 || opt.ServerDelay > time.Duration(calibration.MaxMirrorDelayMS)*time.Millisecond {
			return errors.New("declared mirror response size or delay is out of bounds")
		}
	} else if opt.ResponseBytes != 0 || opt.ServerDelay != 0 {
		return errors.New("declared mirror values cannot be combined with a recorded report")
	}
	if s.Mode != "closed" && s.Mode != "arrival" && s.Mode != "" {
		return errors.New("unsupported scenario mode")
	}
	return nil
}

func syntheticCookies(prefix string, users int) []string {
	cookies := make([]string, users)
	for i := range cookies {
		cookies[i] = fmt.Sprintf("dcal_%s_session=%d", prefix, i+1)
	}
	return cookies
}

// fingerprint recomputes the workload fingerprint the real scenario records
// at the given VU level: original step declarations and session counts are
// restored; resolved synthetic values never enter the digest.
func (p *plan) fingerprint(v config.Validated, users int) (string, error) {
	v.Scenario.SyntheticDataPrefixEnv = p.original.SyntheticDataPrefixEnv
	profiles := append([]config.PreparedProfile(nil), v.Profiles...)
	if len(p.refs) != countSteps(profiles) {
		return "", errors.New("mirror and declared step layouts differ")
	}
	for i := range profiles {
		profiles[i].Steps = append([]config.PreparedStep(nil), profiles[i].Steps...)
	}
	for _, ref := range p.refs {
		profiles[ref.profile].Steps[ref.index].Step = ref.original
	}
	v.Profiles = profiles
	v.Cookies = nil
	if p.original.PerVUCookieEnvPrefix != "" {
		v.Cookies = syntheticCookies("vu", users)
	}
	for i := range v.Profiles {
		v.Profiles[i].Cookies = nil
		if _, ok := p.original.ProfileCookieEnvPrefixes[v.Profiles[i].Name]; ok {
			v.Profiles[i].Cookies = syntheticCookies(fmt.Sprintf("p%d", i), users)
		}
	}
	fp := config.WorkloadFingerprint(v)
	if fp == "" {
		return "", errors.New("workload fingerprint is unavailable")
	}
	return fp, nil
}

func countSteps(profiles []config.PreparedProfile) int {
	n := 0
	for _, profile := range profiles {
		n += len(profile.Steps)
	}
	return n
}

func (p *plan) levelValidated(base config.Validated, mode string, level int, opt Options) (config.Validated, error) {
	v := base
	users := base.Scenario.Users
	if mode == "arrival" {
		v.Scenario.ArrivalRate = level
	} else {
		users = level
		v.Scenario.Users = level
	}
	v.Duration = opt.LevelDuration
	v.Scenario.Duration = opt.LevelDuration.String()
	v.GeneratorGuardrails = opt.Guardrails
	v.NetworkBaseline = nil
	v.Profiles = append([]config.PreparedProfile(nil), base.Profiles...)
	v.ProfileCookies = nil
	if p.original.PerVUCookieEnvPrefix != "" {
		v.Cookies = syntheticCookies("vu", users)
	}
	for i := range v.Profiles {
		if _, ok := p.original.ProfileCookieEnvPrefixes[v.Profiles[i].Name]; ok {
			cookies := syntheticCookies(fmt.Sprintf("p%d", i), users)
			v.Profiles[i].Cookies = cookies
			if v.ProfileCookies == nil {
				v.ProfileCookies = map[string][]string{}
			}
			v.ProfileCookies[v.Profiles[i].Name] = cookies
		}
	}
	fp, err := p.fingerprint(v, users)
	if err != nil {
		return v, err
	}
	v.WorkloadFingerprint = fp
	return v, nil
}

func controlsFor(v config.Validated) calibration.ScenarioControls {
	cfg := engine.RunConfigFor(v)
	c := calibration.ScenarioControls{Mode: cfg.Mode, Steps: len(v.Steps), MaxRPS: cfg.MaxRPS, RequestTimeout: cfg.RequestTimeout,
		ConnectionPolicy: cfg.ConnectionPolicy, MaxConnectionsPerHost: cfg.MaxConnectionsPerHost, ThinkTime: cfg.ThinkTime,
		ThinkTimeSamples: cfg.ThinkTimeSamples, WorkloadSeed: cfg.WorkloadSeed, PerVUCookieSessions: cfg.SeparateSessions}
	if cfg.ThinkTimeRange != nil {
		c.ThinkTimeRangeMin, c.ThinkTimeRangeMax = cfg.ThinkTimeRange.Min, cfg.ThinkTimeRange.Max
	}
	if len(c.ThinkTimeSamples) == 0 {
		c.ThinkTimeSamples = nil
	}
	return c
}

func generatorFor(r report.Result) calibration.ScenarioGenerator {
	g := calibration.ScenarioGenerator{OS: r.GeneratorIdentity.OS, Architecture: r.GeneratorIdentity.Architecture, GoVersion: r.GeneratorIdentity.GoVersion,
		RunnerIdentityMethod: r.GeneratorIdentity.RunnerIdentityMethod, RunnerIdentity: r.GeneratorIdentity.RunnerIdentity}
	if r.Generator != nil {
		g.LogicalCPUs, g.GOMAXPROCS = r.Generator.LogicalCPUs, r.Generator.GOMAXPROCS
		g.CPUCapacityCores, g.CPUCapacitySource, g.CPUCapacityKnown = r.Generator.CPUCapacityCores, r.Generator.CPUCapacitySource, r.Generator.CPUCapacityKnown
	}
	return g
}

func levelEvidence(v config.Validated, r report.Result, ledger Ledger, p *plan) calibration.ScenarioLevel {
	l := calibration.ScenarioLevel{Users: v.Scenario.Users, WorkloadFingerprint: v.WorkloadFingerprint, Duration: v.Duration.String(), RampUp: v.Scenario.RampUp,
		Outcome: r.Outcome, ClientRequests: int64(r.Metrics.Total), ClientSuccess: int64(r.Metrics.Success), ClientFailures: int64(r.Metrics.Failures),
		ClientCancelled: int64(r.Metrics.Cancelled), ServerReceived: ledger.Received, ServerCompleted: ledger.Completed,
		ServerResponseBodyBytes: ledger.ResponseBodyBytes, ServerRequestBodyBytes: ledger.RequestBodyBytes, ServerCookieRequests: ledger.CookieRequests,
		IterationsStarted: r.IterationsStarted, IterationsCompleted: r.IterationsCompleted, ClientP50MS: r.Metrics.P50MS, ClientP95MS: r.Metrics.P95MS,
		ThinkTimeObservations: r.ThinkTimeObservations, FailedChecks: []string{}}
	if v.Scenario.Mode == "arrival" {
		l.ArrivalRate = v.Scenario.ArrivalRate
		l.ArrivalsScheduled, l.ArrivalsDropped = r.ArrivalsScheduled, r.ArrivalsDropped
		l.SchedulerLagP95MS, l.SchedulerLagMaxMS = r.SchedulerLagP95MS, r.SchedulerLagMaxMS
	}
	l.ClientResponseBodyBytes = int64(r.Metrics.ResponseBody.TotalBytes)
	if r.ThinkTimeObservations > 0 {
		l.MeanThinkTimeMS = round3(r.ObservedThinkTimeMS / float64(r.ThinkTimeObservations))
	}
	l.OverheadP50MS, l.OverheadP95MS = math.Inf(1), math.Inf(1)
	delays := map[string]float64{}
	for i, ref := range p.refs {
		profile := "default"
		if len(v.Profiles) > ref.profile {
			profile = v.Profiles[ref.profile].Name
		}
		delays[profile+"\x00"+ref.original.Name] = p.routes[p.routeForStep[i]].DelayMS
	}
	worst50, worst95, matched := 0.0, 0.0, 0
	for _, step := range r.Steps {
		profile := step.Profile
		if profile == "" {
			profile = "default"
		}
		delay, ok := delays[profile+"\x00"+step.Name]
		if !ok || step.Metrics.Total == 0 {
			continue
		}
		matched++
		worst50 = math.Max(worst50, step.Metrics.P50MS-delay)
		worst95 = math.Max(worst95, step.Metrics.P95MS-delay)
	}
	if matched > 0 {
		l.OverheadP50MS, l.OverheadP95MS = round3(worst50), round3(worst95)
	} else {
		l.OverheadP50MS, l.OverheadP95MS = calibration.MaxMirrorDelayMS, calibration.MaxMirrorDelayMS
	}
	l.DeliveryRatio = round3(deliveryRatio(v, r, p))
	if r.Generator != nil {
		l.PeakProcessCPUPercent = round3(r.Generator.PeakProcessCPUUtilizationRatio * 100)
		l.PeakProcessRSSBytes = r.Generator.PeakProcessRSSBytes
		l.GuardrailTriggered = r.Generator.GuardrailTrigger != nil
	}
	return l
}

// deliveryRatio compares achieved closed-loop request starts with the bound
// implied by users, mirror delay, think time between steps and max_rps.
func deliveryRatio(v config.Validated, r report.Result, p *plan) float64 {
	if v.Scenario.Mode == "arrival" {
		return 1
	}
	weightTotal, requestsPerJourney, cycleSeconds := 0.0, 0.0, 0.0
	cursor := 0
	for _, profile := range v.Profiles {
		weight := float64(profile.Weight)
		if weight <= 0 {
			weight = 1
		}
		cycle := 0.0
		for si := range profile.Steps {
			cycle += p.routes[p.routeForStep[cursor+si]].DelayMS / 1000
		}
		think := profile.ThinkTime
		if profile.ThinkTimeMax > 0 {
			think = (profile.ThinkTimeMin + profile.ThinkTimeMax) / 2
		}
		if len(profile.ThinkTimeSamples) > 0 {
			var total time.Duration
			for _, sample := range profile.ThinkTimeSamples {
				total += sample
			}
			think = total / time.Duration(len(profile.ThinkTimeSamples))
		}
		if len(profile.Steps) > 1 {
			cycle += float64(len(profile.Steps)-1) * think.Seconds()
		}
		cursor += len(profile.Steps)
		weightTotal += weight
		requestsPerJourney += weight * float64(len(profile.Steps))
		cycleSeconds += weight * cycle
	}
	if weightTotal == 0 {
		return 0
	}
	requestsPerJourney /= weightTotal
	cycleSeconds /= weightTotal
	active := v.Duration.Seconds() - v.RampUp.Seconds()/2
	if active <= 0 {
		return 0
	}
	perVU := math.Inf(1)
	if cycleSeconds > 0 {
		perVU = requestsPerJourney / cycleSeconds
	}
	bound := math.Min(float64(v.Scenario.MaxRPS), float64(v.Scenario.Users)*perVU) * active
	if bound <= 0 {
		return 0
	}
	return math.Min(float64(r.Metrics.Total)/bound, 10)
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// DigestBytes returns the content digest used to identify a recorded report.
func DigestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
