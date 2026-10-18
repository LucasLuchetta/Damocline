package calibration

import (
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
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

type Runtime struct {
	OS            string
	Arch          string
	GoVersion     string
	LogicalCPUs   int
	GOMAXPROCS    int
	RunnerVersion string
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
		g.LogicalCPUs != local.LogicalCPUs || g.GOMAXPROCS != local.GOMAXPROCS {
		add("generator_runtime", "OUTSIDE_REFERENCE", "Current generator OS, architecture, Go version or CPU configuration differs from the calibration.")
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
	if connectionPolicy == "reuse" {
		add("connection_policy", "MATCHED", "The reference uses reusable HTTP connections; this is a declared policy, not endpoint evidence.")
	} else {
		add("connection_policy", "OUTSIDE_REFERENCE", "The reference calibration covers reusable HTTP connections only.")
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
	durationMatched := durationErr == nil && (duration == 5*time.Second || (duration == 30*time.Second && s.ArrivalRate == 10))
	if durationMatched {
		add("duration", "MATCHED", "The configured duration matches a measured stage or soak.")
	} else {
		add("duration", "OUTSIDE_REFERENCE", "Only five-second stages and a 30-second soak at ten arrivals/s were measured.")
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
	normalized := config.Scenario{
		Name: "Synthetic workload profile comparison", BaseURL: "http://127.0.0.1",
		Users: 1, Duration: "5s", Mode: "arrival", ArrivalRate: 1,
		MaxRPS: 50, Timeout: "1s", ConnectionPolicy: connectionPolicy,
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
	return Runtime{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), RunnerVersion: SourceRunnerVersion()}
}
