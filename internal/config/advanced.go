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
		canonical := normalizeCookieSet(v)
		if seen[canonical] {
			return nil, fmt.Errorf("duplicate session cookie in %s; independent identities are required", name)
		}
		seen[canonical] = true
		sessions[i] = v
	}
	return sessions, nil
}

func normalizeCookieSet(value string) string {
	pairs := strings.Split(value, ";")
	canonical := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			canonical = append(canonical, strings.TrimSpace(parts[0])+"="+strings.TrimSpace(parts[1]))
		} else if pair != "" {
			canonical = append(canonical, pair)
		}
	}
	sort.Strings(canonical)
	return strings.Join(canonical, ";")
}
