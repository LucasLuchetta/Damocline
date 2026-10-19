package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var envPrefixPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*_$`)

// ResolveSessions loads distinct test sessions into memory without serializing them.
// One environment variable per virtual user: PREFIX1, PREFIX2, ... PREFIXN.
func ResolveSessions(prefix string, users int) ([]string, error) {
	if prefix == "" {
		return nil, nil
	}
	if !envPrefixPattern.MatchString(prefix) {
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
	sessions := make([][]string, len(profiles))
	prefixes := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		prefix := profile.PerVUCookieEnvPrefix
		if prefix == "" {
			continue
		}
		if !envPrefixPattern.MatchString(prefix) {
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
		values, err := ResolveSessions(profile.PerVUCookieEnvPrefix, users)
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
