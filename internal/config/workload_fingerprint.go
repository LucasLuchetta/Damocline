package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/textproto"
	"sort"
	"strings"
)

type fingerprintProfile struct {
	Name             string            `json:"name"`
	Weight           int               `json:"weight"`
	ThinkTime        string            `json:"think_time,omitempty"`
	ThinkTimeRange   *ThinkTimeRange   `json:"think_time_range,omitempty"`
	ThinkTimeSamples []string          `json:"think_time_samples,omitempty"`
	Steps            []fingerprintStep `json:"steps"`
}

type fingerprintStep struct {
	Name           string             `json:"name"`
	Method         string             `json:"method"`
	Path           string             `json:"path"`
	Headers        map[string]string  `json:"headers,omitempty"`
	ExpectedStatus []int              `json:"expected_status,omitempty"`
	MustContain    string             `json:"must_contain,omitempty"`
	JSONAssertions map[string]string  `json:"json_assertions,omitempty"`
	BodyEnv        string             `json:"body_env,omitempty"`
	BodyLength     int                `json:"body_length,omitempty"`
	Extract        map[string]Extract `json:"extract,omitempty"`
	Cleanup        bool               `json:"cleanup,omitempty"`
}

type fingerprintInput struct {
	ConnectionPolicy     string               `json:"connection_policy,omitempty"`
	ProfileSelection     string               `json:"profile_selection,omitempty"`
	Profiles             []fingerprintProfile `json:"profiles"`
	PerVUCookieSessions  bool                 `json:"per_vu_cookie_sessions"`
	SyntheticIdentifiers bool                 `json:"synthetic_identifiers"`
	SyntheticPrefix      string               `json:"synthetic_prefix,omitempty"`
	SessionCount         int                  `json:"session_count,omitempty"`
	WorkloadSeed         uint64               `json:"workload_seed,omitempty"`
}

// WorkloadFingerprint returns a stable digest of the declared request and
// journey shape. It hashes raw scenario templates, never resolved credentials,
// environment-backed request body contents, extracted response values, or
// generated synthetic identifiers. It includes only the byte length of a
// configured environment-backed body. An empty result means the scenario
// could not be encoded and must not be treated as comparable.
func WorkloadFingerprint(v Validated) string {
	input := fingerprintInput{
		Profiles:             make([]fingerprintProfile, 0, len(v.Profiles)),
		PerVUCookieSessions:  len(v.Cookies) > 0,
		SyntheticIdentifiers: v.SyntheticDataPrefix != "",
		SessionCount:         len(v.Cookies),
	}
	if v.Scenario.ConnectionPolicy != "reuse" {
		input.ConnectionPolicy = v.Scenario.ConnectionPolicy
	}
	if len(v.Scenario.Profiles) > 0 {
		input.ProfileSelection = "per_journey_start"
	}
	if v.SyntheticDataPrefix != "" {
		input.SyntheticPrefix = v.Scenario.SyntheticDataPrefixEnv
	}
	if hasVariableThinkTime(v.Profiles) {
		input.WorkloadSeed = v.Scenario.WorkloadSeed
	}
	for _, profile := range v.Profiles {
		p := fingerprintProfile{Name: profile.Name, Weight: profile.Weight, ThinkTime: profile.ThinkTime.String(), Steps: make([]fingerprintStep, 0, len(profile.Steps))}
		if profile.ThinkTimeMax > 0 {
			p.ThinkTimeRange = &ThinkTimeRange{Min: profile.ThinkTimeMin.String(), Max: profile.ThinkTimeMax.String()}
		}
		if len(profile.ThinkTimeSamples) > 0 {
			p.ThinkTime = ""
			for _, sample := range profile.ThinkTimeSamples {
				p.ThinkTimeSamples = append(p.ThinkTimeSamples, sample.String())
			}
		}
		for _, step := range profile.Steps {
			statuses := append([]int(nil), step.Step.ExpectedStatus...)
			sort.Ints(statuses)
			method := strings.ToUpper(step.Step.Method)
			if method == "" {
				method = "GET"
			}
			headers := make(map[string]string, len(step.Step.Headers))
			for name, value := range step.Step.Headers {
				headers[textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))] = value
			}
			p.Steps = append(p.Steps, fingerprintStep{
				Name: step.Step.Name, Method: method, Path: step.PathTemplate,
				Headers: headers, ExpectedStatus: statuses, MustContain: step.Step.MustContain,
				JSONAssertions: step.Step.JSONAssertions,
				BodyEnv:        step.Step.BodyEnv, BodyLength: len(step.Body), Extract: step.Step.Extract, Cleanup: step.Step.Cleanup,
			})
		}
		input.Profiles = append(input.Profiles, p)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}
