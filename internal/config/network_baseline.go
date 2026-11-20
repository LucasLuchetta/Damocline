package config

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Network-path baseline bounds (ADR 0153). The baseline is a small, fixed,
// sequential, low-rate probe sent before load; it is never part of the load
// observation and never scales with users or duration.
const (
	NetworkBaselineRequests       = 20
	NetworkBaselineMinInterval    = 100 * time.Millisecond
	NetworkBaselineMaxRequestTime = 5 * time.Second
	NetworkBaselineWallBudget     = 30 * time.Second
	maxNetworkBaselinePathBytes   = 2048
	NetworkBaselineSourceStep     = "first_get_step"
	NetworkBaselineSourcePath     = "baseline_path"
	NetworkBaselineSourceSkipped  = "skipped"
)

var networkBaselineSkipPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// NetworkBaselinePlan is the resolved, safe baseline request. A non-empty
// SkipReason means the operator explicitly opted out; no request is sent.
type NetworkBaselinePlan struct {
	Method     string
	URL        string
	Path       string
	Source     string
	Requests   int
	Interval   time.Duration
	Timeout    time.Duration
	WallBudget time.Duration
	SkipReason string
}

func validateNetworkBaselineFields(s Scenario) error {
	if s.NetworkBaselineSkipReason != "" && !networkBaselineSkipPattern.MatchString(s.NetworkBaselineSkipReason) {
		return errors.New("network_baseline_skip_reason must be a safe 1-80 character label")
	}
	if s.NetworkBaselineSkipReason != "" && s.BaselinePath != "" {
		return errors.New("baseline_path cannot be combined with network_baseline_skip_reason")
	}
	if s.BaselinePath != "" {
		if _, err := baselinePath(s.BaselinePath); err != nil {
			return err
		}
	}
	return nil
}

func baselinePath(raw string) (*url.URL, error) {
	if len(raw) > maxNetworkBaselinePathBytes || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.Contains(raw, "{{") || strings.ContainsAny(raw, "\r\n#\\") {
		return nil, errors.New("baseline_path must be a literal same-origin absolute path (starting with a single /) without templates or fragments")
	}
	p, err := url.Parse(raw)
	if err != nil || p.IsAbs() || p.Host != "" || p.User != nil {
		return nil, errors.New("baseline_path must be a literal same-origin path")
	}
	for _, segment := range strings.Split(p.Path, "/") {
		if segment == ".." {
			return nil, errors.New("baseline_path must not contain dot-dot segments")
		}
	}
	return p, nil
}

// planNetworkBaseline selects the explicit baseline_path or the first GET or
// HEAD step whose path has no template variables and no body. Only GET/HEAD
// are used, without cookies, custom headers, or bodies, so the baseline cannot
// write data or forward credentials.
func planNetworkBaseline(s Scenario, base *url.URL, steps []PreparedStep, timeout time.Duration) (*NetworkBaselinePlan, error) {
	if err := validateNetworkBaselineFields(s); err != nil {
		return nil, err
	}
	if s.NetworkBaselineSkipReason != "" {
		return &NetworkBaselinePlan{Source: NetworkBaselineSourceSkipped, SkipReason: s.NetworkBaselineSkipReason}, nil
	}
	interval := NetworkBaselineMinInterval
	if s.MaxRPS > 0 && time.Second/time.Duration(s.MaxRPS) > interval {
		interval = time.Second / time.Duration(s.MaxRPS)
	}
	requestTimeout := timeout
	if requestTimeout <= 0 || requestTimeout > NetworkBaselineMaxRequestTime {
		requestTimeout = NetworkBaselineMaxRequestTime
	}
	plan := &NetworkBaselinePlan{Requests: NetworkBaselineRequests, Interval: interval, Timeout: requestTimeout, WallBudget: NetworkBaselineWallBudget}
	if s.BaselinePath != "" {
		p, _ := baselinePath(s.BaselinePath)
		resolved := base.ResolveReference(p)
		if resolved.Scheme != base.Scheme || resolved.Host != base.Host {
			return nil, errors.New("baseline_path must resolve to the scenario origin")
		}
		plan.Method, plan.URL, plan.Path, plan.Source = http.MethodGet, resolved.String(), resolved.Path, NetworkBaselineSourcePath
		return plan, nil
	}
	for _, step := range steps {
		if step.Cleanup || len(step.Body) > 0 || strings.Contains(step.PathTemplate, "{{") ||
			(step.Method != http.MethodGet && step.Method != http.MethodHead) {
			continue
		}
		resolved, err := url.Parse(step.URL)
		if err != nil || resolved.Scheme != base.Scheme || resolved.Host != base.Host {
			continue
		}
		plan.Method, plan.URL, plan.Path, plan.Source = step.Method, resolved.String(), resolved.Path, NetworkBaselineSourceStep
		return plan, nil
	}
	return nil, errors.New("network baseline requires a GET/HEAD step without template variables or body, an explicit baseline_path, or an explicit network_baseline_skip_reason")
}
