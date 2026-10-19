package config

import (
	"fmt"
	"os"
	"regexp"
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
		if seen[v] {
			return nil, fmt.Errorf("duplicate session cookie in %s; independent identities are required", name)
		}
		seen[v] = true
		sessions[i] = v
	}
	return sessions, nil
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
			if prior, duplicate := seen[cookie]; duplicate {
				return nil, fmt.Errorf("profiles %q and %q resolve to a duplicate session cookie across actor contexts", prior, profile.Name)
			}
			seen[cookie] = profile.Name
		}
		sessions[index] = values
	}
	return sessions, nil
}
