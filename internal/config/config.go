// Package config loads and validates bounded, reproducible workload scenarios.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Scenario struct {
	Name                   string    `json:"name"`
	WorkloadRevision       string    `json:"workload_revision,omitempty"`
	ApplicationRevision    string    `json:"application_revision,omitempty"`
	Environment            string    `json:"environment,omitempty"`
	ProfileMixBasis        string    `json:"profile_mix_basis,omitempty"`
	ProfileMixReference    string    `json:"profile_mix_reference,omitempty"`
	BaseURL                string    `json:"base_url"`
	Users                  int       `json:"users"`
	Duration               string    `json:"duration"`
	RampUp                 string    `json:"ramp_up"`
	ThinkTime              string    `json:"think_time"`
	Timeout                string    `json:"request_timeout"`
	ConnectionPolicy       string    `json:"connection_policy,omitempty"`
	MaxRPS                 int       `json:"max_rps"`
	P95LimitMS             float64   `json:"p95_limit_ms"`
	MaxErrorRate           float64   `json:"max_error_rate"`
	Mode                   string    `json:"mode,omitempty"`
	ArrivalRate            int       `json:"arrival_rate,omitempty"`
	PerVUCookieEnvPrefix   string    `json:"per_vu_cookie_env_prefix,omitempty"`
	SyntheticDataPrefixEnv string    `json:"synthetic_data_prefix_env,omitempty"`
	MinSamplesPerStep      int       `json:"min_samples_per_step,omitempty"`
	WindowDuration         string    `json:"window_duration,omitempty"`
	Steps                  []Step    `json:"steps,omitempty"`
	Profiles               []Profile `json:"profiles,omitempty"`
}

// Profile is a weighted, named workflow. Its weight expresses the intended
// share of journey starts, not a guarantee of completed journeys.
type Profile struct {
	Name      string `json:"name"`
	Weight    int    `json:"weight"`
	ThinkTime string `json:"think_time,omitempty"`
	Steps     []Step `json:"steps"`
}

type Step struct {
	Name           string             `json:"name"`
	Method         string             `json:"method"`
	Path           string             `json:"path"`
	Headers        map[string]string  `json:"headers,omitempty"`
	ExpectedStatus []int              `json:"expected_status,omitempty"`
	MustContain    string             `json:"must_contain,omitempty"`
	BodyEnv        string             `json:"body_env,omitempty"`
	Extract        map[string]Extract `json:"extract,omitempty"`
}

// Extract declares a value that a step may capture for later steps in the same
// journey. Exactly one selector is required for each variable.
type Extract struct {
	JSONPointer string `json:"json_pointer,omitempty"`
	Header      string `json:"header,omitempty"`
}

type Options struct {
	Authorized        bool
	AllowWrites       bool
	AllowInsecureHTTP bool
}

type Validated struct {
	Scenario            Scenario
	WorkloadFingerprint string
	Base                *url.URL
	Duration            time.Duration
	RampUp              time.Duration
	ThinkTime           time.Duration
	Timeout             time.Duration
	Window              time.Duration
	Steps               []PreparedStep
	Profiles            []PreparedProfile
	Cookies             []string
	SyntheticDataPrefix string
}

// PreparedProfile contains the safe, resolved workflow selected by the
// scheduler. Steps remain flattened in Validated.Steps for aggregate metrics.
type PreparedProfile struct {
	Name      string
	Weight    int
	ThinkTime time.Duration
	Steps     []PreparedStep
}

type PreparedStep struct {
	Step
	Profile         string
	URL             string
	ResolvedHeaders map[string]string
	Body            []byte
	PathTemplate    string
}

var envPattern = regexp.MustCompile(`^\$\{([A-Z_][A-Z0-9_]*)\}$`)
var variablePattern = regexp.MustCompile(`\{\{([a-z][a-z0-9_]{0,63})\}\}`)
var variableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

const maxScenarioBytes = 1 << 20

func Load(path string) (Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("open scenario: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxScenarioBytes+1))
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	if len(data) > maxScenarioBytes {
		return Scenario{}, fmt.Errorf("scenario exceeds the %d-byte input limit", maxScenarioBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Scenario{}, errors.New("scenario must contain exactly one JSON object")
	}
	return s, nil
}

func Parse(s Scenario, o Options) (Validated, error) {
	var v Validated
	if !o.Authorized {
		return v, errors.New("explicit --authorized confirmation is required before generating traffic")
	}
	if strings.TrimSpace(s.Name) == "" {
		return v, errors.New("name is required")
	}
	for field, value := range map[string]string{"workload_revision": s.WorkloadRevision, "application_revision": s.ApplicationRevision, "environment": s.Environment} {
		if value != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`).MatchString(value) {
			return v, fmt.Errorf("%s must be a safe 1-120 character label", field)
		}
	}
	if s.ProfileMixBasis == "" {
		s.ProfileMixBasis = "assumption"
	}
	if s.ProfileMixBasis != "assumption" && s.ProfileMixBasis != "sanitized_analytics" && s.ProfileMixBasis != "business_estimate" {
		return v, errors.New("profile_mix_basis must be assumption, sanitized_analytics, or business_estimate")
	}
	if s.ProfileMixReference != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`).MatchString(s.ProfileMixReference) {
		return v, errors.New("profile_mix_reference must be a safe 1-80 character label")
	}
	if s.ProfileMixBasis == "assumption" && s.ProfileMixReference != "" {
		return v, errors.New("profile_mix_reference requires a non-assumption profile_mix_basis")
	}
	if s.ProfileMixBasis != "assumption" && s.ProfileMixReference == "" {
		return v, errors.New("profile_mix_reference is required for a non-assumption profile_mix_basis")
	}
	if s.Users < 1 || s.Users > 50 {
		return v, errors.New("users must be between 1 and 50 for this initial release")
	}
	if s.MaxRPS < 1 || s.MaxRPS > 100 {
		return v, errors.New("max_rps must be between 1 and 100; use conservative limits on production")
	}
	profiles, err := workloadProfiles(s)
	if err != nil {
		return v, err
	}
	maxSteps := 0
	totalSteps := 0
	for _, profile := range profiles {
		if len(profile.Steps) > maxSteps {
			maxSteps = len(profile.Steps)
		}
		totalSteps += len(profile.Steps)
	}
	if s.Mode == "" {
		s.Mode = "closed"
	}
	if s.Mode != "closed" && s.Mode != "arrival" {
		return v, errors.New("mode must be closed or arrival")
	}
	if s.ConnectionPolicy == "" {
		s.ConnectionPolicy = "reuse"
	}
	if s.ConnectionPolicy != "reuse" && s.ConnectionPolicy != "new_per_request" {
		return v, errors.New("connection_policy must be reuse or new_per_request")
	}
	if s.Mode == "arrival" {
		if s.ArrivalRate < 1 || s.ArrivalRate > 100 {
			return v, errors.New("arrival_rate must be between 1 and 100 iterations/second")
		}
		if s.ArrivalRate*maxSteps > s.MaxRPS {
			return v, errors.New("arrival_rate multiplied by the longest profile exceeds max_rps; increase max_rps or reduce arrival_rate")
		}
		if s.RampUp != "" && s.RampUp != "0s" {
			return v, errors.New("ramp_up is not supported with arrival mode")
		}
	} else if s.ArrivalRate != 0 {
		return v, errors.New("arrival_rate requires mode=arrival")
	}
	if s.Mode == "closed" && len(s.Profiles) > 0 && s.Users < len(s.Profiles) {
		return v, errors.New("users must be at least the number of profiles so every profile is represented")
	}
	if s.MinSamplesPerStep == 0 {
		s.MinSamplesPerStep = 1
	}
	if s.MinSamplesPerStep < 1 || s.MinSamplesPerStep > 10000 {
		return v, errors.New("min_samples_per_step must be 1..10000")
	}
	if s.P95LimitMS <= 0 || s.P95LimitMS > 120000 {
		return v, errors.New("p95_limit_ms must be in (0,120000]")
	}
	if s.MaxErrorRate < 0 || s.MaxErrorRate > 1 {
		return v, errors.New("max_error_rate must be between 0 and 1")
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return v, errors.New("base_url must be an HTTP(S) origin without credentials, paths, or query")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return v, errors.New("base_url must use HTTP or HTTPS")
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return v, errors.New("base_url hostname is required")
	}
	if u.Scheme == "http" && !isLoopback(hostname) && !o.AllowInsecureHTTP {
		return v, errors.New("plaintext HTTP is blocked for non-loopback targets; explicitly pass --allow-insecure-http only when appropriate")
	}
	if strings.ContainsAny(u.Host, " \t\r\n") {
		return v, errors.New("invalid host")
	}
	duration, err := requiredDuration("duration", s.Duration, time.Second, 3*time.Minute)
	if err != nil {
		return v, err
	}
	ramp, err := optionalDuration("ramp_up", s.RampUp, 0, duration)
	if err != nil {
		return v, err
	}
	think, err := optionalDuration("think_time", s.ThinkTime, 0, 30*time.Second)
	if err != nil {
		return v, err
	}
	window, err := optionalDuration("window_duration", s.WindowDuration, 0, time.Minute)
	if err != nil {
		return v, err
	}
	if window > 0 && (window < time.Second || window > duration) {
		return v, errors.New("window_duration must be at least 1s and no longer than duration")
	}
	timeout, err := optionalDuration("request_timeout", s.Timeout, 8*time.Second, 30*time.Second)
	if err != nil {
		return v, err
	}
	seen := map[string]bool{}
	prepared := make([]PreparedStep, 0, totalSteps)
	preparedProfiles := make([]PreparedProfile, 0, len(profiles))
	if s.PerVUCookieEnvPrefix != "" {
		for _, profile := range profiles {
			if containsCookieHeader(profile.Steps) {
				return v, errors.New("cannot combine per_vu_cookie_env_prefix with a shared Cookie header")
			}
		}
	}
	dataPrefix := ""
	if s.SyntheticDataPrefixEnv != "" {
		if !regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`).MatchString(s.SyntheticDataPrefixEnv) {
			return v, errors.New("synthetic_data_prefix_env must be an environment variable name")
		}
		var ok bool
		dataPrefix, ok = os.LookupEnv(s.SyntheticDataPrefixEnv)
		if !ok || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(dataPrefix) {
			return v, fmt.Errorf("synthetic data prefix environment variable %s must contain 1-64 letters, digits, underscores, or hyphens", s.SyntheticDataPrefixEnv)
		}
	}
	for _, profile := range profiles {
		profileThink, err := optionalDuration("profile think_time", profile.ThinkTime, think, 30*time.Second)
		if err != nil {
			return v, err
		}
		profileSteps := make([]PreparedStep, 0, len(profile.Steps))
		available := map[string]bool{}
		if dataPrefix != "" {
			available["synthetic_id"] = true
		}
		for _, step := range profile.Steps {
			if step.Name == "" || len(step.Name) > 80 || seen[step.Name] {
				return v, fmt.Errorf("step names must be non-empty, unique, and at most 80 characters: %q", step.Name)
			}
			seen[step.Name] = true
			method := strings.ToUpper(step.Method)
			if method == "" {
				method = "GET"
			}
			if method != "GET" && method != "HEAD" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
				return v, fmt.Errorf("unsupported method in %q", step.Name)
			}
			if method != "GET" && method != "HEAD" && !o.AllowWrites {
				return v, fmt.Errorf("step %q uses %s: --allow-writes is required", step.Name, method)
			}
			if step.BodyEnv != "" && method != "POST" && method != "PUT" && method != "PATCH" {
				return v, fmt.Errorf("body_env requires POST, PUT, or PATCH in %q", step.Name)
			}
			if step.BodyEnv != "" && !regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`).MatchString(step.BodyEnv) {
				return v, fmt.Errorf("invalid body_env in %q", step.Name)
			}
			if step.Path == "" || !strings.HasPrefix(step.Path, "/") || strings.HasPrefix(step.Path, "//") || strings.ContainsAny(step.Path, "\r\n\\") {
				return v, fmt.Errorf("step %q path must be a relative-to-origin absolute path", step.Name)
			}
			if err := validatePathTemplate(step.Path, available); err != nil {
				return v, fmt.Errorf("step %q: %w", step.Name, err)
			}
			p, err := url.Parse(step.Path)
			if err != nil || p.Host != "" || p.Scheme != "" || p.Fragment != "" || strings.Contains(strings.ToLower(p.EscapedPath()), "%2e") || strings.Contains(step.Path, "..") {
				return v, fmt.Errorf("unsafe path in step %q", step.Name)
			}
			if len(step.ExpectedStatus) > 30 {
				return v, fmt.Errorf("too many expected_status values in %q", step.Name)
			}
			for _, code := range step.ExpectedStatus {
				if code < 100 || code > 599 {
					return v, fmt.Errorf("invalid expected_status in %q", step.Name)
				}
			}
			if len(step.MustContain) > 256 {
				return v, fmt.Errorf("must_contain is limited to 256 bytes in %q", step.Name)
			}
			head := make(map[string]string, len(step.Headers))
			for key, value := range step.Headers {
				canonical := strings.ToLower(strings.TrimSpace(key))
				if canonical == "" || strings.ContainsAny(key+value, "\r\n") || canonical == "host" || canonical == "content-length" {
					return v, fmt.Errorf("invalid/forbidden header in %q", step.Name)
				}
				if len(value) > 8192 {
					return v, fmt.Errorf("header value too large in %q", step.Name)
				}
				if err := validateTemplate(value, available); err != nil {
					return v, fmt.Errorf("header %q in step %q: %w", key, step.Name, err)
				}
				found := envPattern.FindStringSubmatch(value)
				if found != nil {
					secret, ok := os.LookupEnv(found[1])
					if !ok || secret == "" {
						return v, fmt.Errorf("missing required environment variable %s", found[1])
					}
					if strings.ContainsAny(secret, "\r\n") {
						return v, fmt.Errorf("environment variable %s contains an invalid header character", found[1])
					}
					head[key] = secret
				} else {
					isSensitive := canonical == "authorization" || canonical == "cookie" || canonical == "proxy-authorization" || strings.Contains(canonical, "api-key")
					if isSensitive && !strings.Contains(value, "{{") {
						return v, fmt.Errorf("sensitive header %q must reference an environment variable like ${DAMOCLINE_COOKIE}", key)
					}
					head[key] = value
				}
			}
			var body []byte
			if step.BodyEnv != "" {
				secret, ok := os.LookupEnv(step.BodyEnv)
				if !ok {
					return v, fmt.Errorf("missing body environment variable %s", step.BodyEnv)
				}
				if len(secret) > 65536 {
					return v, fmt.Errorf("body_env %s exceeds 64 KiB", step.BodyEnv)
				}
				body = []byte(secret)
				if err := validateTemplate(secret, available); err != nil {
					return v, fmt.Errorf("body_env in step %q: %w", step.Name, err)
				}
			}
			if err := validateExtracts(step.Extract, available); err != nil {
				return v, fmt.Errorf("step %q: %w", step.Name, err)
			}
			step.Method = method
			preparedStep := PreparedStep{Step: step, Profile: profile.Name, URL: u.ResolveReference(p).String(), ResolvedHeaders: head, Body: body, PathTemplate: step.Path}
			prepared = append(prepared, preparedStep)
			profileSteps = append(profileSteps, preparedStep)
		}
		preparedProfiles = append(preparedProfiles, PreparedProfile{Name: profile.Name, Weight: profile.Weight, ThinkTime: profileThink, Steps: profileSteps})
	}
	cookies, err := ResolveSessions(s.PerVUCookieEnvPrefix, s.Users)
	if err != nil {
		return v, err
	}
	v = Validated{Scenario: s, Base: u, Duration: duration, RampUp: ramp, ThinkTime: think, Timeout: timeout, Window: window, Steps: prepared, Profiles: preparedProfiles, Cookies: cookies, SyntheticDataPrefix: dataPrefix}
	v.WorkloadFingerprint = WorkloadFingerprint(v)
	return v, nil
}

func workloadProfiles(s Scenario) ([]Profile, error) {
	if len(s.Profiles) == 0 {
		if len(s.Steps) < 1 || len(s.Steps) > 20 {
			return nil, errors.New("steps must contain 1 to 20 entries when profiles are absent")
		}
		return []Profile{{Name: "default", Weight: 1, Steps: s.Steps}}, nil
	}
	if len(s.Steps) > 0 {
		return nil, errors.New("steps and profiles cannot be combined")
	}
	if len(s.Profiles) < 2 || len(s.Profiles) > 10 {
		return nil, errors.New("profiles must contain 2 to 10 entries")
	}
	seen := map[string]bool{}
	totalWeight := 0
	totalSteps := 0
	profiles := make([]Profile, 0, len(s.Profiles))
	for _, profile := range s.Profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" || len(name) > 80 || seen[name] {
			return nil, fmt.Errorf("profile names must be non-empty, unique, and at most 80 characters: %q", profile.Name)
		}
		if profile.Weight < 1 || profile.Weight > 1000 {
			return nil, fmt.Errorf("profile %q weight must be between 1 and 1000", name)
		}
		if len(profile.Steps) < 1 || len(profile.Steps) > 20 {
			return nil, fmt.Errorf("profile %q steps must contain 1 to 20 entries", name)
		}
		profile.Name = name
		seen[name] = true
		totalWeight += profile.Weight
		totalSteps += len(profile.Steps)
		profiles = append(profiles, profile)
	}
	if totalWeight > 10000 {
		return nil, errors.New("profile weights must total at most 10000")
	}
	if totalSteps > 20 {
		return nil, errors.New("profiles may contain at most 20 total steps")
	}
	return profiles, nil
}

func containsCookieHeader(steps []Step) bool {
	for _, step := range steps {
		for key := range step.Headers {
			if strings.EqualFold(key, "Cookie") {
				return true
			}
		}
	}
	return false
}

func validatePathTemplate(path string, available map[string]bool) error {
	pathOnly, query, hasQuery := strings.Cut(path, "?")
	if hasQuery && strings.Contains(query, "{{") {
		return errors.New("variables are only supported as complete path segments, headers, or body_env content")
	}
	for _, segment := range strings.Split(pathOnly, "/") {
		if !strings.Contains(segment, "{{") {
			continue
		}
		matches := variablePattern.FindStringSubmatch(segment)
		if len(matches) != 2 || matches[0] != segment || !available[matches[1]] {
			return fmt.Errorf("path variable %q must be extracted by an earlier step and occupy a complete path segment", segment)
		}
	}
	return nil
}

func validateTemplate(value string, available map[string]bool) error {
	if !strings.Contains(value, "{{") {
		return nil
	}
	matches := variablePattern.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return errors.New("invalid variable template")
	}
	for _, match := range matches {
		if !available[match[1]] {
			return fmt.Errorf("variable %q is not extracted by an earlier step", match[1])
		}
	}
	remaining := variablePattern.ReplaceAllString(value, "")
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return errors.New("invalid variable template")
	}
	return nil
}

func validateExtracts(extracts map[string]Extract, available map[string]bool) error {
	for name, extract := range extracts {
		if !variableNamePattern.MatchString(name) {
			return fmt.Errorf("invalid extracted variable name %q", name)
		}
		if available[name] {
			return fmt.Errorf("variable %q is extracted more than once in one journey", name)
		}
		hasPointer := extract.JSONPointer != ""
		hasHeader := extract.Header != ""
		if hasPointer == hasHeader {
			return fmt.Errorf("extract %q must define exactly one of json_pointer or header", name)
		}
		if hasPointer && (!strings.HasPrefix(extract.JSONPointer, "/") || len(extract.JSONPointer) > 512) {
			return fmt.Errorf("extract %q has an invalid json_pointer", name)
		}
		if hasHeader && (len(extract.Header) > 128 || strings.ContainsAny(extract.Header, "\r\n")) {
			return fmt.Errorf("extract %q has an invalid header", name)
		}
		available[name] = true
	}
	return nil
}

func isLoopback(h string) bool {
	return h == "localhost" || h == "localhost.localdomain" || (net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback())
}
func requiredDuration(name, value string, min, max time.Duration) (time.Duration, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < min || d > max {
		return 0, fmt.Errorf("%s must be between %s and %s", name, min, max)
	}
	return d, nil
}
func optionalDuration(name, value string, def, max time.Duration) (time.Duration, error) {
	if value == "" {
		return def, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 || d > max {
		return 0, fmt.Errorf("%s must be between 0 and %s", name, max)
	}
	return d, nil
}
