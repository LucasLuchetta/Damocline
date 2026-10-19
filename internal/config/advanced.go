package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var envPrefixPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*_$`)

const trialCookiePrefixToken = "{trial}"

// TrialCookieSet contains resolved credentials for one independent campaign
// trial. Values are held in memory only and are never serialized.
type TrialCookieSet struct {
	Scenario []string
	Profiles [][]string
}

// TrialCookieSets contains fully validated credentials for all campaign
// trials. Rotated is true only when every configured cookie prefix is scoped
// to the one-based trial number.
type TrialCookieSets struct {
	Trials  []TrialCookieSet
	Rotated bool
}

// ResolveTrialCookieSets resolves and validates every trial's cookies before
// traffic starts. actorCounts permits a closed-loop spike to require a larger
// actor set than the base phases.
func ResolveTrialCookieSets(globalPrefix string, profiles []Profile, actorCounts []int) (TrialCookieSets, error) {
	rotated := strings.Contains(globalPrefix, trialCookiePrefixToken)
	configured := globalPrefix != ""
	for _, profile := range profiles {
		if profile.PerVUCookieEnvPrefix == "" {
			continue
		}
		profileRotated := strings.Contains(profile.PerVUCookieEnvPrefix, trialCookiePrefixToken)
		if !configured {
			rotated = profileRotated
			configured = true
		} else if profileRotated != rotated {
			return TrialCookieSets{}, fmt.Errorf("all configured per_vu_cookie_env_prefix values must use {trial} when any one does")
		}
	}
	sets := TrialCookieSets{Trials: make([]TrialCookieSet, len(actorCounts)), Rotated: rotated}
	registry := make(map[[32]byte]struct{})
	prefixRegistry := make(map[string]struct{})
	for index, users := range actorCounts {
		trial := index + 1
		resolvedGlobalPrefix, err := expandTrialCookiePrefix(globalPrefix, trial)
		if err != nil {
			return TrialCookieSets{}, err
		}
		if resolvedGlobalPrefix != "" {
			if !validCookieEnvPrefix(resolvedGlobalPrefix) {
				return TrialCookieSets{}, fmt.Errorf("per_vu_cookie_env_prefix must be uppercase and end with an underscore")
			}
		}
		for _, profile := range profiles {
			resolvedPrefix, err := expandTrialCookiePrefix(profile.PerVUCookieEnvPrefix, trial)
			if err != nil {
				return TrialCookieSets{}, fmt.Errorf("profile %q authentication context: %w", profile.Name, err)
			}
			if resolvedPrefix == "" {
				continue
			}
			if _, duplicate := prefixRegistry[resolvedPrefix]; duplicate && rotated {
				return TrialCookieSets{}, fmt.Errorf("trial-scoped cookie prefixes must be distinct across workflows and trials")
			}
			if rotated {
				prefixRegistry[resolvedPrefix] = struct{}{}
			}
		}
		cookies, err := ResolveSessionsForTrial(globalPrefix, users, trial)
		if err != nil {
			return TrialCookieSets{}, err
		}
		profileCookies, err := ResolveProfileSessionsForTrial(profiles, users, trial)
		if err != nil {
			return TrialCookieSets{}, err
		}
		sets.Trials[index] = TrialCookieSet{Scenario: cookies, Profiles: profileCookies}
		if rotated {
			for _, group := range append([][]string{cookies}, profileCookies...) {
				for _, cookie := range group {
					canonical, err := canonicalCookieSet(cookie)
					if err != nil {
						return TrialCookieSets{}, fmt.Errorf("invalid trial-scoped authentication cookie: %w", err)
					}
					key := sha256.Sum256([]byte(canonical))
					if _, duplicate := registry[key]; duplicate {
						return TrialCookieSets{}, fmt.Errorf("trial-scoped cookie sets must be distinct across all actors, workflows, and trials")
					}
					registry[key] = struct{}{}
				}
			}
		}
	}
	return sets, nil
}

// ResolveSessions loads trial 1 for ordinary runs and CLI configuration checks.
func ResolveSessions(prefix string, users int) ([]string, error) {
	return ResolveSessionsForTrial(prefix, users, 1)
}

// One environment variable per virtual user: PREFIX1, PREFIX2, ... PREFIXN.
func ResolveSessionsForTrial(prefix string, users, trial int) ([]string, error) {
	if prefix == "" {
		return nil, nil
	}
	var err error
	prefix, err = expandTrialCookiePrefix(prefix, trial)
	if err != nil {
		return nil, err
	}
	if !validCookieEnvPrefix(prefix) {
		return nil, fmt.Errorf("per_vu_cookie_env_prefix must be uppercase and end with an underscore")
	}
	sessions := make([]string, users)
	seen := make(map[string]bool, users)
	for i := range sessions {
		name := fmt.Sprintf("%s%d", prefix, i+1)
		v, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("missing required session environment variable %s", name)
		}
		if len(v) > 8192 || strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("invalid cookie in %s", name)
		}
		key, err := canonicalCookieSet(v)
		if err != nil {
			return nil, fmt.Errorf("invalid cookie in %s: %w", name, err)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate session cookie set in %s; each actor must have a distinct cookie set", name)
		}
		seen[key] = true
		sessions[i] = v
	}
	return sessions, nil
}

func canonicalCookieSet(raw string) (string, error) {
	parts := strings.Split(raw, ";")
	cookies := make([]string, 0, len(parts))
	names := make(map[string]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 {
			return "", fmt.Errorf("cookie entries must use name=value form")
		}
		name := strings.TrimSpace(pair[0])
		if name == "" || strings.ContainsAny(name, "\t\r\n =;") {
			return "", fmt.Errorf("cookie name is invalid")
		}
		if names[name] {
			return "", fmt.Errorf("cookie set contains a duplicate name")
		}
		names[name] = true
		cookies = append(cookies, name+"="+pair[1])
	}
	if len(cookies) == 0 {
		return "", fmt.Errorf("cookie set is empty")
	}
	sort.Strings(cookies)
	return strings.Join(cookies, ";"), nil
}

// ResolveProfileSessions loads isolated per-actor cookies for configured
// profiles. Cookie values and environment-variable names remain in memory only.
func ResolveProfileSessions(profiles []Profile, users int) ([][]string, error) {
	return ResolveProfileSessionsForTrial(profiles, users, 1)
}

// ResolveProfileSessionsForTrial resolves each profile's cookie prefix for a
// specific one-based campaign trial.
func ResolveProfileSessionsForTrial(profiles []Profile, users, trial int) ([][]string, error) {
	sessions := make([][]string, len(profiles))
	prefixes := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		prefix, err := expandTrialCookiePrefix(profile.PerVUCookieEnvPrefix, trial)
		if err != nil {
			return nil, fmt.Errorf("profile %q authentication context: %w", profile.Name, err)
		}
		if prefix == "" {
			continue
		}
		if !validCookieEnvPrefix(profile.PerVUCookieEnvPrefix) {
			return nil, fmt.Errorf("profile %q per_vu_cookie_env_prefix must be uppercase and end with an underscore", profile.Name)
		}
		if prefixes[prefix] {
			return nil, fmt.Errorf("profiles must use distinct per_vu_cookie_env_prefix values")
		}
		prefixes[prefix] = true
	}
	seen := make(map[string]string)
	for index, profile := range profiles {
		if profile.PerVUCookieEnvPrefix == "" {
			continue
		}
		values, err := ResolveSessionsForTrial(profile.PerVUCookieEnvPrefix, users, trial)
		if err != nil {
			return nil, fmt.Errorf("profile %q authentication context: %w", profile.Name, err)
		}
		for _, cookie := range values {
			key, err := canonicalCookieSet(cookie)
			if err != nil {
				return nil, fmt.Errorf("profile %q authentication context: %w", profile.Name, err)
			}
			if prior, duplicate := seen[key]; duplicate {
				return nil, fmt.Errorf("profiles %q and %q resolve to a duplicate session cookie across actor contexts", prior, profile.Name)
			}
			seen[key] = profile.Name
		}
		sessions[index] = values
	}
	return sessions, nil
}

func expandTrialCookiePrefix(prefix string, trial int) (string, error) {
	if prefix == "" {
		return "", nil
	}
	if trial < 1 || strings.Count(prefix, trialCookiePrefixToken) > 1 {
		return "", fmt.Errorf("per_vu_cookie_env_prefix may contain at most one {trial} token and trial numbers start at 1")
	}
	return strings.ReplaceAll(prefix, trialCookiePrefixToken, fmt.Sprint(trial)), nil
}

func validCookieEnvPrefix(prefix string) bool {
	if strings.Count(prefix, trialCookiePrefixToken) > 1 {
		return false
	}
	return envPrefixPattern.MatchString(strings.ReplaceAll(prefix, trialCookiePrefixToken, "1"))
}
