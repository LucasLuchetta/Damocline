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
	steps := s.Steps
	singleGET := len(s.Profiles) == 0 && len(steps) == 1
	if singleGET {
		step := steps[0]
		method := strings.ToUpper(step.Method)
		if method == "" {
			method = "GET"
		}
		singleGET = method == "GET" && len(step.Headers) == 0 && step.BodyEnv == "" && len(step.Extract) == 0 &&
			len(step.ExpectedStatus) == 0 && step.MustContain == "" && s.PerVUCookieEnvPrefix == "" && s.SyntheticDataPrefixEnv == ""
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
	if s.MaxRPS < 1 || s.MaxRPS > rec.Fixture.MaximumRPS || s.ArrivalRate < 1 || int64(s.ArrivalRate)*int64(stepCount) > int64(s.MaxRPS) {
		add("request_rate_ceiling", "OUTSIDE_REFERENCE", "The declared request ceiling is invalid or outside the tested fixture bound.")
	} else {
		add("request_rate_ceiling", "MATCHED", "The declared request ceiling is within the fixture bound; delivered rate still requires observation.")
	}
	if singleGET {
		add("request_mix", "MATCHED", "The declared request is one unauthenticated GET step.")
	} else {
		add("request_mix", "OUTSIDE_REFERENCE", "Multiple steps, profiles, writes, authentication, assertions or dynamic values are outside the fixture.")
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
	add("response_shape", "UNVERIFIED", "Scenario declarations do not prove actual response size or timing distribution.")
	add("fixture_identity", "UNVERIFIED", "No authenticated signal proves the configured endpoint is the measured fixture server.")
	return c
}

func CurrentRuntime() Runtime {
	return Runtime{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), RunnerVersion: SourceRunnerVersion()}
}
