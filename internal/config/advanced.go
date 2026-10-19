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
		canonical, err := normalizeCookieSet(v)
		if err != nil {
			return nil, fmt.Errorf("invalid cookie in %s: %w", name, err)
		}
		if seen[canonical] {
			return nil, fmt.Errorf("duplicate session cookie in %s; independent identities are required", name)
		}
		seen[canonical] = true
		sessions[i] = v
	}
	return sessions, nil
}

func normalizeCookieSet(value string) (string, error) {
	pairs := strings.Split(value, ";")
	canonical := make([]string, 0, len(pairs))
	names := make(map[string]bool, len(pairs))
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("cookie entries must use name=value form")
		}
		name, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if name == "" || strings.ContainsAny(name, "\t\r\n =;") {
			return "", fmt.Errorf("cookie name is invalid")
		}
		if names[name] {
			return "", fmt.Errorf("cookie set contains a duplicate name")
		}
		names[name] = true
		canonical = append(canonical, name+"="+value)
	}
	if len(canonical) == 0 {
		return "", fmt.Errorf("cookie set is empty")
	}
	sort.Strings(canonical)
	return strings.Join(canonical, ";"), nil
}
