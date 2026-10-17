// Package config loads and validates bounded, reproducible workload scenarios.
package config

import (
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
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	Users                int     `json:"users"`
	Duration             string  `json:"duration"`
	RampUp               string  `json:"ramp_up"`
	ThinkTime            string  `json:"think_time"`
	Timeout              string  `json:"request_timeout"`
	MaxRPS               int     `json:"max_rps"`
	P95LimitMS           float64 `json:"p95_limit_ms"`
	MaxErrorRate         float64 `json:"max_error_rate"`
	Mode                 string  `json:"mode,omitempty"`
	ArrivalRate          int     `json:"arrival_rate,omitempty"`
	PerVUCookieEnvPrefix string  `json:"per_vu_cookie_env_prefix,omitempty"`
	MinSamplesPerStep    int     `json:"min_samples_per_step,omitempty"`
	Steps                []Step  `json:"steps"`
}

type Step struct {
	Name           string            `json:"name"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Headers        map[string]string `json:"headers,omitempty"`
	ExpectedStatus []int             `json:"expected_status,omitempty"`
	MustContain    string            `json:"must_contain,omitempty"`
	BodyEnv        string            `json:"body_env,omitempty"`
}

type Options struct {
	Authorized        bool
	AllowWrites       bool
	AllowInsecureHTTP bool
}

type Validated struct {
	Scenario  Scenario
	Base      *url.URL
	Duration  time.Duration
	RampUp    time.Duration
	ThinkTime time.Duration
	Timeout   time.Duration
	Steps     []PreparedStep
	Cookies   []string
}

type PreparedStep struct {
	Step
	URL             string
	ResolvedHeaders map[string]string
	Body            []byte
}

var envPattern = regexp.MustCompile(`^\$\{([A-Z_][A-Z0-9_]*)\}$`)

func Load(path string) (Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("open scenario: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
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
	if s.Users < 1 || s.Users > 50 {
		return v, errors.New("users must be between 1 and 50 for this initial release")
	}
	if s.MaxRPS < 1 || s.MaxRPS > 100 {
		return v, errors.New("max_rps must be between 1 and 100; use conservative limits on production")
	}
	if s.Mode == "" {
		s.Mode = "closed"
	}
	if s.Mode != "closed" && s.Mode != "arrival" {
		return v, errors.New("mode must be closed or arrival")
	}
	if s.Mode == "arrival" {
		if s.ArrivalRate < 1 || s.ArrivalRate > 20 {
			return v, errors.New("arrival_rate must be between 1 and 20 iterations/second")
		}
		if s.ArrivalRate*len(s.Steps) > s.MaxRPS {
			return v, errors.New("arrival_rate multiplied by steps exceeds max_rps; increase max_rps or reduce arrival_rate")
		}
		if s.RampUp != "" && s.RampUp != "0s" {
			return v, errors.New("ramp_up is not supported with arrival mode")
		}
	} else if s.ArrivalRate != 0 {
		return v, errors.New("arrival_rate requires mode=arrival")
	}
	if s.MinSamplesPerStep == 0 {
		s.MinSamplesPerStep = 1
	}
	if s.MinSamplesPerStep < 1 || s.MinSamplesPerStep > 10000 {
		return v, errors.New("min_samples_per_step must be 1..10000")
	}
	if len(s.Steps) < 1 || len(s.Steps) > 20 {
		return v, errors.New("steps must contain 1 to 20 entries")
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
	timeout, err := optionalDuration("request_timeout", s.Timeout, 8*time.Second, 30*time.Second)
	if err != nil {
		return v, err
	}
	seen := map[string]bool{}
	prepared := make([]PreparedStep, 0, len(s.Steps))
	if s.PerVUCookieEnvPrefix != "" {
		for _, step := range s.Steps {
			for k := range step.Headers {
				if strings.EqualFold(k, "Cookie") {
					return v, errors.New("cannot combine per_vu_cookie_env_prefix with a shared Cookie header")
				}
			}
		}
	}
	for _, step := range s.Steps {
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
				if canonical == "authorization" || canonical == "cookie" || canonical == "proxy-authorization" || strings.Contains(canonical, "api-key") {
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
		}
		step.Method = method
		prepared = append(prepared, PreparedStep{Step: step, URL: u.ResolveReference(p).String(), ResolvedHeaders: head, Body: body})
	}
	cookies, err := ResolveSessions(s.PerVUCookieEnvPrefix, s.Users)
	if err != nil {
		return v, err
	}
	v = Validated{Scenario: s, Base: u, Duration: duration, RampUp: ramp, ThinkTime: think, Timeout: timeout, Steps: prepared, Cookies: cookies}
	return v, nil
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
