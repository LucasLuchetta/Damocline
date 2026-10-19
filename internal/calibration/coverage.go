package calibration

import (
	"fmt"
	"net"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

type Dimension struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type Coverage struct {
	Status     string      `json:"status"`
	Dimensions []Dimension `json:"dimensions"`
}

// RateAssessment records whether a calibrated generator profile covers one
// exact planned arrival rate. It describes generator-side fixture evidence,
// not target capacity or sustained headroom.
type RateAssessment struct {
	ArrivalRate int         `json:"arrival_rate"`
	Status      string      `json:"status"`
	Reason      string      `json:"reason"`
	Dimensions  []Dimension `json:"dimensions"`
}

// ArrivalEnvelope summarizes exact calibrated coverage for a bounded set of
// planned arrival rates. A READY result is not a capacity or duration claim.
type ArrivalEnvelope struct {
	Status string           `json:"status"`
	Rates  []RateAssessment `json:"rates"`
}

// ClosedLoopAssessment records the exact VU levels and generator controls a
// closed-loop campaign would run. It does not qualify target capacity.
type ClosedLoopAssessment struct {
	Users      int         `json:"users"`
	Status     string      `json:"status"`
	Reason     string      `json:"reason"`
	Dimensions []Dimension `json:"dimensions"`
}

type ClosedLoopEnvelope struct {
	Status string                 `json:"status"`
	Levels []ClosedLoopAssessment `json:"levels"`
}

// AssessClosedLoopEnvelope checks each distinct planned VU level against exact
// closed-loop profiles. Arrival profiles never stand in for virtual users.
func AssessClosedLoopEnvelope(s config.Scenario, users []int, rec Record, local Runtime, now time.Time) ClosedLoopEnvelope {
	result := ClosedLoopEnvelope{Status: "BLOCKED", Levels: []ClosedLoopAssessment{}}
	seen := map[int]bool{}
	validRecord := ValidateRecord(rec) == nil && rec.SchemaVersion >= 9 && rec.Status == "REFERENCE_SMOKE_PASSED"
	for _, count := range users {
		assessment := ClosedLoopAssessment{Users: count, Status: "BLOCKED"}
		switch {
		case count < 1 || count > 10000:
			assessment.Reason = "Planned virtual-user levels must be within the bounded 1-10000 range."
			assessment.Dimensions = []Dimension{{Name: "users", Status: "OUTSIDE_REFERENCE", Reason: assessment.Reason}}
		case seen[count]:
			assessment.Reason = "Planned virtual-user levels must be unique."
			assessment.Dimensions = []Dimension{{Name: "users", Status: "INVALID", Reason: assessment.Reason}}
		default:
			seen[count] = true
			assessment.Dimensions = assessClosedLoopLevel(s, count, rec, local, now, validRecord)
			blockers := make([]Dimension, 0)
			for _, dimension := range assessment.Dimensions {
				if dimension.Status != "MATCHED" {
					blockers = append(blockers, dimension)
				}
			}
			if len(blockers) == 0 {
				assessment.Status = "READY"
				assessment.Reason = "An exact local closed-loop profile covers this VU level, declared one-step workload, runtime controls, and generator identity fields. It does not establish target identity, longer campaign phase headroom, or application capacity."
			} else {
				assessment.Reason = "The closed-loop calibration artifact does not cover every required generator dimension for this VU level."
			}
		}
		result.Levels = append(result.Levels, assessment)
	}
	if len(result.Levels) == 0 {
		return result
	}
	result.Status = "READY"
	for _, level := range result.Levels {
		if level.Status != "READY" {
			result.Status = "BLOCKED"
			break
		}
	}
	return result
}

func assessClosedLoopLevel(s config.Scenario, users int, rec Record, local Runtime, now time.Time, validRecord bool) []Dimension {
	if !validRecord {
		return []Dimension{{Name: "calibration", Status: "UNVERIFIED", Reason: "A validated passing schema-v9 closed-loop calibration record is unavailable."}}
	}
	var dimensions []Dimension
	add := func(name, status, reason string) {
		dimensions = append(dimensions, Dimension{Name: name, Status: status, Reason: reason})
	}
	finished, err := time.Parse(time.RFC3339Nano, rec.FinishedAt)
	if err != nil || now.Before(finished.Add(-5*time.Minute)) || now.Sub(finished) > 24*time.Hour {
		add("calibration_age", "OUTSIDE_REFERENCE", "Calibration is future-dated or older than the 24-hour review threshold.")
	} else {
		add("calibration_age", "MATCHED", "Calibration is within the 24-hour review threshold.")
	}
	g := rec.Generator
	if g.OS != local.OS || g.Arch != local.Arch || g.GoVersion != local.GoVersion || g.LogicalCPUs != local.LogicalCPUs || g.GOMAXPROCS != local.GOMAXPROCS || g.CPUCapacityCores != local.CPUCapacityCores || g.CPUCapacitySource != local.CPUCapacitySource || g.CPUCapacityKnown != local.CPUCapacityKnown {
		add("generator_runtime", "OUTSIDE_REFERENCE", "Current generator OS, architecture, Go version, runner or CPU configuration differs from the calibration.")
	} else if !g.CPUCapacityKnown || !local.CPUCapacityKnown {
		add("generator_runtime", "UNVERIFIED", "CPU quota or effective capacity could not be verified for both calibration and current generator.")
	} else if g.RunnerVersion == "source-development" || local.RunnerVersion == "source-development" {
		add("generator_runtime", "UNVERIFIED", "Runtime metadata matches but the exact runner source revision is unavailable.")
	} else if g.RunnerVersion != local.RunnerVersion {
		add("generator_runtime", "OUTSIDE_REFERENCE", "Current generator runner revision differs from the calibration.")
	} else {
		add("generator_runtime", "MATCHED", "Runtime metadata matches; physical host identity is not attested.")
	}
	mode := s.Mode
	if mode == "" {
		mode = "closed"
	}
	if mode != "closed" || s.ArrivalRate != 0 {
		add("workload_model", "OUTSIDE_REFERENCE", "The calibration covers closed-loop virtual users, not independent arrival-mode starts.")
	} else {
		add("workload_model", "MATCHED", "The scenario uses closed-loop virtual users.")
	}
	connection := s.ConnectionPolicy
	if connection == "" {
		connection = "reuse"
	}
	timeout := s.Timeout
	if timeout == "" {
		timeout = "8s"
	}
	ramp := s.RampUp
	if ramp == "" {
		ramp = "0s"
	}
	think := s.ThinkTime
	if think == "" {
		think = "0s"
	}
	if s.MaxConnectionsPerHost != 0 || ramp != "0s" || think != "0s" || s.ThinkTimeRange != nil || len(s.ThinkTimeSamples) > 0 {
		add("timing_controls", "OUTSIDE_REFERENCE", "Closed-loop profiles cover zero ramp-up, fixed zero think time, and the default connection pool only.")
	} else {
		add("timing_controls", "MATCHED", "Ramp-up, think-time and connection-cap controls match the closed-loop fixture.")
	}
	shape := AssessDeclaredWorkloadShape(s, rec)
	add(shape.Name, shape.Status, shape.Reason)
	profileFound := false
	for _, profile := range rec.Fixture.WorkloadProfiles {
		if profile.Mode == "closed" && profile.TransportScheme == "http" && profile.Users == users && profile.MaxRPS == 100 && s.MaxRPS == profile.MaxRPS && profile.Timeout == timeout && profile.ConnectionPolicy == connection && profile.Duration == "2s" && profile.RampUp == "0s" && profile.ThinkTime == "0s" && profile.Fingerprint == WorkloadProfileFingerprint(profile) {
			profileFound = true
			break
		}
	}
	if profileFound {
		add("exact_profile", "MATCHED", "The exact VU level, request ceiling, timeout, duration and connection policy were measured in closed-loop mode.")
	} else {
		add("exact_profile", "OUTSIDE_REFERENCE", "No closed-loop profile matches this exact VU level and control tuple; VU levels are not interpolated.")
	}
	return dimensions
}

type Runtime struct {
	OS                string
	Arch              string
	GoVersion         string
	LogicalCPUs       int
	GOMAXPROCS        int
	CPUCapacityCores  float64
	CPUCapacitySource string
	CPUCapacityKnown  bool
	RunnerVersion     string
}

// AssessScenario compares only declarative inputs with the fixed reference
// fixture. It never treats scenario declarations as proof of target behavior.
func AssessScenario(s config.Scenario, rec Record, local Runtime, now time.Time) Coverage {
	c := Coverage{Status: "INCONCLUSIVE"}
	add := func(name, status, reason string) {
		c.Dimensions = append(c.Dimensions, Dimension{name, status, reason})
	}
	if err := ValidateRecord(rec); err != nil || rec.Status != "REFERENCE_SMOKE_PASSED" {
		add("calibration", "UNVERIFIED", "A validated passing calibration record is unavailable.")
		return c
	}
	workloadDimension := AssessDeclaredWorkloadShape(s, rec)
	add(workloadDimension.Name, workloadDimension.Status, workloadDimension.Reason)
	finished, err := time.Parse(time.RFC3339Nano, rec.FinishedAt)
	if err != nil || now.Before(finished.Add(-5*time.Minute)) || now.Sub(finished) > 24*time.Hour {
		add("calibration_age", "OUTSIDE_REFERENCE", "Calibration is future-dated or older than the 24-hour review threshold.")
	} else {
		add("calibration_age", "MATCHED", "Calibration is within the 24-hour review threshold.")
	}
	g := rec.Generator
	if g.OS != local.OS || g.Arch != local.Arch || g.GoVersion != local.GoVersion ||
		g.LogicalCPUs != local.LogicalCPUs || g.GOMAXPROCS != local.GOMAXPROCS ||
		(rec.SchemaVersion >= 7 && (g.CPUCapacityCores != local.CPUCapacityCores || g.CPUCapacitySource != local.CPUCapacitySource || g.CPUCapacityKnown != local.CPUCapacityKnown)) {
		add("generator_runtime", "OUTSIDE_REFERENCE", "Current generator OS, architecture, Go version or CPU configuration differs from the calibration.")
	} else if rec.SchemaVersion < 7 {
		add("generator_runtime", "UNVERIFIED", "Legacy calibration does not record effective CPU capacity or its cgroup evidence source.")
	} else if !g.CPUCapacityKnown || !local.CPUCapacityKnown {
		add("generator_runtime", "UNVERIFIED", "CPU quota or effective capacity could not be verified for both calibration and current generator.")
	} else if g.RunnerVersion == "source-development" || local.RunnerVersion == "source-development" {
		add("generator_runtime", "UNVERIFIED", "Runtime metadata matches but the exact runner source revision is unavailable.")
	} else if g.RunnerVersion != local.RunnerVersion {
		add("generator_runtime", "OUTSIDE_REFERENCE", "Current generator runner revision differs from the calibration.")
	} else {
		add("generator_runtime", "MATCHED", "Runtime metadata matches; physical host identity is not attested.")
	}
	mode := s.Mode
	if mode == "" {
		mode = "closed"
	}
	if mode == "arrival" {
		add("workload_model", "MATCHED", "The scenario uses independent journey arrivals.")
	} else {
		add("workload_model", "OUTSIDE_REFERENCE", "The fixture tested independent journey arrivals only.")
	}
	connectionPolicy := s.ConnectionPolicy
	if connectionPolicy == "" {
		connectionPolicy = "reuse"
	}
	if exactProfileControlsMatch(rec.Fixture.WorkloadProfiles, s, connectionPolicy) || exactReferenceSoakControlsMatch(s, connectionPolicy) {
		add("connection_policy", "MATCHED", "The declared connection policy and exact control tuple match a measured profile; live connection behavior remains fixture-scoped.")
	} else {
		add("connection_policy", "OUTSIDE_REFERENCE", "No measured profile matches the declared connection policy and exact control tuple.")
	}
	rateMatched := false
	for _, rate := range rec.Fixture.HealthyRates {
		if rate == s.ArrivalRate {
			rateMatched = true
			break
		}
	}
	if mode != "arrival" || !rateMatched {
		add("arrival_rate", "OUTSIDE_REFERENCE", "Only exact recorded arrival rates are eligible; rates are not interpolated.")
	} else {
		add("arrival_rate", "MATCHED", "The configured arrival rate was tested exactly.")
	}
	if s.Users != rec.Fixture.StageUsers {
		add("worker_budget", "OUTSIDE_REFERENCE", "The configured arrival-worker budget differs from the fixture.")
	} else {
		add("worker_budget", "MATCHED", "The configured arrival-worker budget matches the fixture.")
	}
	stepCount := len(s.Steps)
	if len(s.Profiles) > 0 {
		stepCount = 0
		for _, profile := range s.Profiles {
			if len(profile.Steps) > stepCount {
				stepCount = len(profile.Steps)
			}
		}
	}
	requestCeilingValid := s.MaxRPS >= 1 && s.MaxRPS <= rec.Fixture.MaximumRPS &&
		s.ArrivalRate >= 1 && int64(s.ArrivalRate)*int64(stepCount) <= int64(s.MaxRPS)
	requestCeilingTested := rec.SchemaVersion < 3
	if rec.SchemaVersion >= 3 {
		requestCeilingTested = false
		for _, stage := range rec.HealthyStages {
			if stage.ArrivalRate == s.ArrivalRate && stage.MaxRPS == s.MaxRPS {
				requestCeilingTested = true
				break
			}
		}
		for _, profile := range rec.Fixture.WorkloadProfiles {
			if profile.ArrivalRate == s.ArrivalRate && profile.MaxRPS == s.MaxRPS {
				requestCeilingTested = true
				break
			}
		}
	}
	if !requestCeilingValid || !requestCeilingTested {
		add("request_rate_ceiling", "OUTSIDE_REFERENCE", "The declared request ceiling is invalid or its exact arrival-rate and max_rps combination was not tested.")
	} else {
		add("request_rate_ceiling", "MATCHED", "The declared request ceiling is within the fixture bound; delivered rate still requires observation.")
	}
	simpleRequestMix := len(s.Profiles) == 0 && len(s.Steps) == 1 &&
		len(s.Steps[0].Headers) == 0 && len(s.Steps[0].ExpectedStatus) == 0 &&
		s.Steps[0].MustContain == "" && len(s.Steps[0].Extract) == 0 &&
		(s.Steps[0].BodyEnv == "" || strings.EqualFold(s.Steps[0].Method, "POST")) &&
		s.PerVUCookieEnvPrefix == "" && s.SyntheticDataPrefixEnv == ""
	if simpleRequestMix {
		add("request_mix", "MATCHED", "The declared journey contains one simple request step; exact request equivalence is assessed separately.")
	} else {
		add("request_mix", "OUTSIDE_REFERENCE", "Multiple steps, profiles, custom headers, assertions, or dynamic dependencies are outside the fixed reference.")
	}
	duration, durationErr := time.ParseDuration(s.Duration)
	durationMatched := durationErr == nil && (duration == 30*time.Second && s.ArrivalRate == 10)
	if exactProfileControlsMatch(rec.Fixture.WorkloadProfiles, s, connectionPolicy) {
		durationMatched = true
	}
	if durationMatched {
		add("duration", "MATCHED", "The configured duration matches a measured stage or soak.")
	} else {
		add("duration", "OUTSIDE_REFERENCE", "The requested duration is not part of an exact measured profile or the controlled 30-second soak.")
	}
	timeout := s.Timeout
	if timeout == "" {
		timeout = "8s"
	}
	if timeout == "1s" && (s.RampUp == "" || s.RampUp == "0s") && (s.ThinkTime == "" || s.ThinkTime == "0s") {
		add("timing", "MATCHED", "Timeout, ramp-up and think-time settings match the fixture.")
	} else {
		add("timing", "OUTSIDE_REFERENCE", "Timeout, ramp-up or think time differs from the fixture.")
	}
	u, parseErr := url.Parse(s.BaseURL)
	if parseErr == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).Equal(net.ParseIP("127.0.0.1")) {
		add("declared_transport", "MATCHED", "The URL declares unencrypted IPv4 loopback; server identity is unverified.")
	} else {
		add("declared_transport", "OUTSIDE_REFERENCE", "Only unencrypted IPv4 loopback was tested.")
	}
	add("response_shape", "UNVERIFIED", "Calibrated response sizes are recorded for local profiles, but the scenario does not prove the target's actual response-size or timing distribution.")
	add("fixture_identity", "UNVERIFIED", "No authenticated signal proves the configured endpoint is the measured fixture server.")
	return c
}

// AssessArrivalEnvelope checks that each planned rate has an exact healthy
// workload-matrix profile for the declared request shape and current generator
// runtime. The short fixture duration is used only to match profile controls;
// this does not establish sustained headroom for the campaign's longer phases.
func AssessArrivalEnvelope(s config.Scenario, rates []int, rec Record, local Runtime, now time.Time) ArrivalEnvelope {
	result := ArrivalEnvelope{Status: "BLOCKED", Rates: []RateAssessment{}}
	if len(rates) == 0 {
		return result
	}
	if err := ValidateRecord(rec); err != nil || rec.SchemaVersion < 6 || rec.Status != "REFERENCE_SMOKE_PASSED" {
		for _, rate := range rates {
			result.Rates = append(result.Rates, RateAssessment{ArrivalRate: rate, Status: "BLOCKED",
				Reason:     "A current passing workload-matrix calibration is unavailable.",
				Dimensions: []Dimension{{Name: "calibration", Status: "UNVERIFIED", Reason: "A current passing workload-matrix calibration is unavailable."}}})
		}
		return result
	}
	ordered := append([]int(nil), rates...)
	sort.Ints(ordered)
	for i, rate := range ordered {
		assessment := RateAssessment{ArrivalRate: rate, Status: "BLOCKED"}
		if rate < 1 || rate > 100 || i > 0 && ordered[i-1] == rate {
			assessment.Reason = "Planned arrival rates must be unique values within the bounded 1-100/s range."
			assessment.Dimensions = []Dimension{{Name: "arrival_rate", Status: "OUTSIDE_REFERENCE", Reason: assessment.Reason}}
			result.Rates = append(result.Rates, assessment)
			continue
		}
		candidate := s
		candidate.Mode = "arrival"
		candidate.ArrivalRate = rate
		profileDuration, profileFound := exactArrivalProfileDuration(candidate, rate, rec)
		if profileFound {
			candidate.Duration = profileDuration
		}
		coverage := AssessScenario(candidate, rec, local, now.UTC())
		assessment.Dimensions = coverage.Dimensions
		if !profileFound {
			assessment.Dimensions = append(assessment.Dimensions, Dimension{Name: "exact_profile", Status: "OUTSIDE_REFERENCE", Reason: "No exact healthy matrix profile matches this rate, request ceiling, worker limit, timeout, and connection policy."})
		}
		blockers := requiredArrivalEnvelopeDimensions(assessment.Dimensions)
		if len(blockers) == 0 && profileFound {
			assessment.Status = "READY"
			assessment.Reason = "An exact local fixture profile covers this scheduled arrival rate and declared request shape; campaign duration, target response behavior, and target identity remain unverified."
		} else {
			assessment.Reason = fmt.Sprintf("The local preflight does not cover all generator dimensions for %d scheduled starts/s.", rate)
		}
		result.Rates = append(result.Rates, assessment)
	}
	for _, assessment := range result.Rates {
		if assessment.Status != "READY" {
			return result
		}
	}
	result.Status = "READY"
	return result
}

func requiredArrivalEnvelopeDimensions(dimensions []Dimension) []Dimension {
	required := map[string]bool{
		"calibration_age": true, "generator_runtime": true, "workload_model": true,
		"connection_policy": true, "arrival_rate": true, "worker_budget": true,
		"request_rate_ceiling": true, "request_mix": true, "timing": true,
		"declared_workload_shape": true,
	}
	seen := make(map[string]int, len(required))
	blockers := make([]Dimension, 0)
	for _, dimension := range dimensions {
		if required[dimension.Name] {
			seen[dimension.Name]++
			if dimension.Status != "MATCHED" {
				blockers = append(blockers, dimension)
			}
		}
	}
	for name := range required {
		if seen[name] == 0 {
			blockers = append(blockers, Dimension{Name: name, Status: "UNVERIFIED", Reason: "Required generator preflight evidence is missing."})
		} else if seen[name] > 1 {
			blockers = append(blockers, Dimension{Name: name, Status: "INVALID", Reason: "Required generator preflight evidence is duplicated."})
		}
	}
	return blockers
}

func exactArrivalProfileDuration(s config.Scenario, rate int, rec Record) (string, bool) {
	if rec.SchemaVersion < 6 || ValidateRecord(rec) != nil || rec.Status != "REFERENCE_SMOKE_PASSED" ||
		AssessDeclaredWorkloadShape(s, rec).Status != "MATCHED" {
		return "", false
	}
	connectionPolicy := s.ConnectionPolicy
	if connectionPolicy == "" {
		connectionPolicy = "reuse"
	}
	timeout := s.Timeout
	if timeout == "" {
		timeout = "8s"
	}
	for _, profile := range rec.Fixture.WorkloadProfiles {
		if profile.ArrivalRate == rate && profile.MaxRPS == s.MaxRPS && profile.Users == s.Users &&
			profile.Timeout == timeout && profile.ConnectionPolicy == connectionPolicy &&
			profile.Mode == "arrival" && profile.TransportScheme == "http" {
			return profile.Duration, true
		}
	}
	return "", false
}

// AssessDeclaredWorkloadShape compares the sanitized declared request/session
// shape with the exact workload exercised by healthy preflight stages. It does
// not establish endpoint identity or application behavior.
func AssessDeclaredWorkloadShape(s config.Scenario, rec Record) Dimension {
	if err := ValidateRecord(rec); err != nil || rec.SchemaVersion < 2 {
		return Dimension{
			Name:   "declared_workload_shape",
			Status: "UNVERIFIED",
			Reason: "The calibration record has no versioned workload-shape fingerprint.",
		}
	}
	if rec.SchemaVersion >= 3 {
		return assessMatrixWorkloadShape(s, rec)
	}
	if s.ProfileMixBasis != "" && s.ProfileMixBasis != "assumption" || s.ProfileMixReference != "" {
		return Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The fixed reference has one assumed journey and does not validate an externally sourced or weighted workflow mix.",
		}
	}
	if len(s.Profiles) != 0 || len(s.Steps) != 1 ||
		s.PerVUCookieEnvPrefix != "" || s.SyntheticDataPrefixEnv != "" {
		return Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The fixed reference covers one unauthenticated GET journey without profiles or synthetic session data.",
		}
	}
	step := s.Steps[0]
	method := strings.ToUpper(step.Method)
	if method == "" {
		method = "GET"
	}
	if method != "GET" || len(step.Headers) != 0 || len(step.ExpectedStatus) != 0 ||
		step.MustContain != "" || step.BodyEnv != "" || len(step.Extract) != 0 {
		return Dimension{
			Name:   "declared_workload_shape",
			Status: "OUTSIDE_REFERENCE",
			Reason: "The fixed reference covers one unauthenticated GET journey without assertions, request bodies, or dynamic dependencies.",
		}
	}
	// Normalize only campaign controls that do not participate in the workload
	// fingerprint. The declarative workload fields above remain untouched.
	// An explicit think time is part of the profile fingerprint and is retained.
	normalized := s
	if normalized.Name == "" {
		normalized.Name = "Synthetic workload comparison"
	}
	if normalized.BaseURL == "" {
		normalized.BaseURL = "http://127.0.0.1"
	}
	if normalized.Users == 0 {
		normalized.Users = 10
	}
	if normalized.Duration == "" {
		normalized.Duration = "5s"
	}
	if normalized.Timeout == "" {
		normalized.Timeout = "1s"
	}
	if normalized.MaxRPS == 0 {
		normalized.MaxRPS = 50
	}
	if normalized.Mode == "" {
		normalized.Mode = "arrival"
	}
	if normalized.ArrivalRate == 0 {
		normalized.ArrivalRate = 2
	}
	if normalized.P95LimitMS == 0 {
		normalized.P95LimitMS = 500
	}
	if normalized.MinSamplesPerStep == 0 {
		normalized.MinSamplesPerStep = 1
	}
	validated, err := config.Parse(normalized, config.Options{Authorized: true})
	if err != nil || validated.WorkloadFingerprint == "" {
		return Dimension{
			Name:   "declared_workload_shape",
			Status: "UNVERIFIED",
			Reason: "The declared workload shape could not be safely normalized for comparison.",
		}
	}
	return AssessWorkloadFingerprint(
		validated.WorkloadFingerprint,
		s.ProfileMixBasis,
		s.ProfileMixReference,
		rec,
	)
}

func assessMatrixWorkloadShape(s config.Scenario, rec Record) Dimension {
	dimension := Dimension{Name: "declared_workload_shape", Status: "OUTSIDE_REFERENCE"}
	if (s.ProfileMixBasis != "" && s.ProfileMixBasis != "assumption") || s.ProfileMixReference != "" {
		dimension.Reason = "The calibrated profiles cover one assumed request at a time and do not validate a sourced or weighted workflow mix."
		return dimension
	}
	if len(s.Profiles) != 0 || len(s.Steps) != 1 || s.PerVUCookieEnvPrefix != "" || s.SyntheticDataPrefixEnv != "" {
		dimension.Reason = "The calibrated matrix covers one request step without profiles, independent sessions, or synthetic journey data."
		return dimension
	}
	step := s.Steps[0]
	method := strings.ToUpper(step.Method)
	if method == "" {
		method = "GET"
	}
	if (method != "GET" && method != "POST") || len(step.Headers) != 0 || len(step.ExpectedStatus) != 0 ||
		step.MustContain != "" || len(step.Extract) != 0 {
		dimension.Reason = "The calibrated matrix covers unasserted GET and POST steps without custom headers or dynamic dependencies."
		return dimension
	}
	if s.Mode == "closed" {
		profiles := rec.Fixture.WorkloadProfiles
		for _, profile := range profiles {
			if profile.Mode != "closed" || profile.Method != method || profile.Path != step.Path || profile.ConnectionPolicy != func() string {
				if s.ConnectionPolicy == "" {
					return "reuse"
				}
				return s.ConnectionPolicy
			}() {
				continue
			}
			if profile.RequestBodyBytes == 0 && step.BodyEnv == "" && profile.Fingerprint == WorkloadProfileFingerprint(profile) {
				dimension.Status = "MATCHED"
				dimension.Reason = "The declared closed-loop request shape matches an exactly measured local VU profile; response behavior and target identity remain unverified."
				return dimension
			}
		}
		dimension.Reason = "No closed-loop profile exactly matches the declared request shape."
		return dimension
	}

	connectionPolicy := s.ConnectionPolicy
	if connectionPolicy == "" {
		connectionPolicy = "reuse"
	}
	transportScheme := "http"
	if s.BaseURL != "" {
		declaredURL, err := url.Parse(s.BaseURL)
		if err != nil || declaredURL.Scheme == "" {
			dimension.Status = "UNVERIFIED"
			dimension.Reason = "The declared transport scheme could not be safely read for profile comparison."
			return dimension
		}
		transportScheme = declaredURL.Scheme
	}
	step.Method = method
	arrivalRate := s.ArrivalRate
	if arrivalRate < 1 {
		arrivalRate = 2
	}
	normalized := config.Scenario{
		Name: "Synthetic workload profile comparison", BaseURL: "http://127.0.0.1",
		Users: s.Users, Duration: s.Duration, Mode: "arrival", ArrivalRate: arrivalRate,
		MaxRPS: s.MaxRPS, Timeout: s.Timeout, ConnectionPolicy: connectionPolicy,
		P95LimitMS: 500, MaxErrorRate: 1, Steps: []config.Step{step},
	}
	validated, err := config.Parse(normalized, config.Options{Authorized: true, AllowWrites: true})
	if err != nil || len(validated.Steps) != 1 {
		dimension.Status = "UNVERIFIED"
		dimension.Reason = "The declared request shape could not be safely normalized; request-body contents are never copied into calibration evidence."
		return dimension
	}
	profileShape := WorkloadProfile{
		Method: method, Path: validated.Steps[0].PathTemplate,
		RequestBodyBytes: len(validated.Steps[0].Body), ConnectionPolicy: connectionPolicy,
	}
	fingerprint := WorkloadProfileFingerprint(profileShape)
	for _, profile := range rec.Fixture.WorkloadProfiles {
		if profile.Fingerprint == fingerprint && profile.TransportScheme == transportScheme {
			dimension.Status = "MATCHED"
			dimension.Reason = "The declared method, path, request-body size, and connection policy match an exactly measured local profile; response behavior and target identity remain unverified."
			return dimension
		}
	}
	dimension.Reason = "No calibrated profile exactly matches the declared method, path, request-body size, and connection policy."
	return dimension
}

func exactProfileControlsMatch(profiles []WorkloadProfile, s config.Scenario, connectionPolicy string) bool {
	for _, profile := range profiles {
		if profile.ArrivalRate == s.ArrivalRate && profile.MaxRPS == s.MaxRPS && profile.Users == s.Users &&
			profile.Duration == s.Duration && profile.Timeout == s.Timeout && profile.ConnectionPolicy == connectionPolicy {
			return true
		}
	}
	return false
}

func exactReferenceSoakControlsMatch(s config.Scenario, connectionPolicy string) bool {
	return connectionPolicy == "reuse" && s.Mode == "arrival" && s.ArrivalRate == 10 && s.Users == 10 &&
		s.MaxRPS == 50 && s.Duration == "30s" && s.Timeout == "1s" &&
		(s.RampUp == "" || s.RampUp == "0s") && (s.ThinkTime == "" || s.ThinkTime == "0s")
}

// AssessWorkloadFingerprint is the report-side comparison used to recompute
// declared workload equivalence without retaining scenario content.
func AssessWorkloadFingerprint(fingerprint, mixBasis, mixReference string, rec Record) Dimension {
	dimension := Dimension{
		Name:   "declared_workload_shape",
		Status: "UNVERIFIED",
		Reason: "Run or calibration workload-shape fingerprint is unavailable.",
	}
	if ValidateRecord(rec) != nil || rec.SchemaVersion < 2 || !workloadFingerprintPattern.MatchString(rec.FixtureWorkloadFingerprint) {
		return dimension
	}
	if !workloadFingerprintPattern.MatchString(fingerprint) {
		return dimension
	}
	if (mixBasis != "" && mixBasis != "assumption") || mixReference != "" {
		dimension.Status = "OUTSIDE_REFERENCE"
		dimension.Reason = "The fixed reference has one assumed journey and does not validate an externally sourced or weighted workflow mix."
		return dimension
	}
	if fingerprint != rec.FixtureWorkloadFingerprint {
		dimension.Status = "OUTSIDE_REFERENCE"
		dimension.Reason = "The declared request, journey, session, or synthetic-data shape differs from the workload exercised by preflight."
		return dimension
	}
	dimension.Status = "MATCHED"
	dimension.Reason = "The declared request and session shape matches the fixed preflight workload; endpoint identity and application behavior remain unverified."
	return dimension
}

func CurrentRuntime() Runtime {
	capacity := generatorhealth.CurrentCPUCapacity()
	return Runtime{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), CPUCapacityCores: capacity.Cores, CPUCapacitySource: capacity.Source, CPUCapacityKnown: capacity.Known, RunnerVersion: SourceRunnerVersion()}
}
