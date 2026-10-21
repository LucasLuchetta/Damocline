// Package campaignprofile stores reusable, private local closed-loop campaign
// settings. Execution evidence remains in the normal campaign report format.
package campaignprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/jsonartifact"
)

const CurrentSchemaVersion = 1

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
var envReference = regexp.MustCompile(`^\$\{[A-Z_][A-Z0-9_]*\}$`)

type Profile struct {
	SchemaVersion int             `json:"schema_version"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	CreatedAt     string          `json:"created_at,omitempty"`
	UpdatedAt     string          `json:"updated_at,omitempty"`
	Scenario      config.Scenario `json:"scenario"`
	Options       Options         `json:"options"`
}

// Options stores all reproducible campaign controls. Consent flags, resolved
// credentials, calibration file paths, and external metrics endpoints are not
// persisted; operators supply them again when executing a profile.
type Options struct {
	Warmup             string                     `json:"warmup"`
	Stabilization      string                     `json:"stabilization"`
	Observation        string                     `json:"observation"`
	SpikeUsers         int                        `json:"spike_users,omitempty"`
	SpikeDuration      string                     `json:"spike_duration"`
	Recovery           string                     `json:"recovery"`
	Soak               string                     `json:"soak"`
	Cooldown           string                     `json:"cooldown"`
	Repeats            int                        `json:"repeats"`
	Budget             string                     `json:"budget"`
	Stability          campaign.StabilityPolicy   `json:"stability_policy"`
	Guardrails         generatorhealth.Guardrails `json:"generator_guardrails,omitempty"`
	LongRun            bool                       `json:"long_run,omitempty"`
	RequireCalibration bool                       `json:"require_calibration,omitempty"`
}

func New(name, description string, scenario config.Scenario, options campaign.Options) (Profile, error) {
	if !ValidName(name) {
		return Profile{}, errors.New("profile name must use 1-48 lowercase letters, digits, hyphens, or underscores and start with a letter or digit")
	}
	if err := ValidateScenarioSecrets(scenario); err != nil {
		return Profile{}, err
	}
	options.StagingConfirmed = options.LongRun
	if options.Stability == (campaign.StabilityPolicy{}) {
		options.Stability = campaign.DefaultStabilityPolicy()
	}
	if options.Guardrails.Enabled() && options.Guardrails.ConsecutiveSamples == 0 {
		options.Guardrails.ConsecutiveSamples = 2
	}
	if options.Budget == 0 {
		options.Budget = campaign.PlannedBudget(options)
	}
	if err := campaign.Validate(options); err != nil {
		return Profile{}, fmt.Errorf("invalid campaign profile options: %w", err)
	}
	created := time.Now().UTC().Format(time.RFC3339Nano)
	profile := Profile{SchemaVersion: CurrentSchemaVersion, Name: name, Description: description, CreatedAt: created, UpdatedAt: created, Scenario: scenario,
		Options: Options{Warmup: options.Warmup.String(), Stabilization: options.Stabilization.String(), Observation: options.Observation.String(),
			SpikeUsers: options.SpikeUsers, SpikeDuration: options.SpikeDuration.String(), Recovery: options.Recovery.String(), Soak: options.Soak.String(),
			Cooldown: options.Cooldown.String(), Repeats: options.Repeats, Budget: options.Budget.String(), Stability: options.Stability,
			Guardrails: options.Guardrails, LongRun: options.LongRun, RequireCalibration: options.RequireCalibration}}
	return profile, nil
}

func (p Profile) CampaignOptions() (campaign.Options, error) {
	return p.parseCampaignOptions(true)
}

func parse(name, raw string) (time.Duration, error) {
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration: %w", name, err)
	}
	return value, nil
}

func (p Profile) parseCampaignOptions(validate bool) (campaign.Options, error) {
	var o campaign.Options
	var err error
	if o.Warmup, err = parse("warmup", p.Options.Warmup); err != nil {
		return o, err
	}
	if o.Stabilization, err = parse("stabilization", p.Options.Stabilization); err != nil {
		return o, err
	}
	if o.Observation, err = parse("observation", p.Options.Observation); err != nil {
		return o, err
	}
	if o.SpikeDuration, err = parse("spike", p.Options.SpikeDuration); err != nil {
		return o, err
	}
	if o.Recovery, err = parse("recovery", p.Options.Recovery); err != nil {
		return o, err
	}
	if o.Soak, err = parse("soak", p.Options.Soak); err != nil {
		return o, err
	}
	if o.Cooldown, err = parse("cooldown", p.Options.Cooldown); err != nil {
		return o, err
	}
	if o.Budget, err = parse("budget", p.Options.Budget); err != nil {
		return o, err
	}
	o.SpikeUsers, o.Repeats, o.Stability, o.Guardrails = p.Options.SpikeUsers, p.Options.Repeats, p.Options.Stability, p.Options.Guardrails
	o.LongRun, o.StagingConfirmed, o.RequireCalibration = p.Options.LongRun, p.Options.LongRun, p.Options.RequireCalibration
	if validate {
		if o.Budget <= 0 {
			return o, errors.New("saved campaign profile must preserve an explicit wall-time budget")
		}
		if err := campaign.Validate(o); err != nil {
			return o, fmt.Errorf("invalid saved campaign options: %w", err)
		}
	}
	return o, nil
}

func ValidName(name string) bool { return namePattern.MatchString(name) }

// ValidateScenarioSecrets prevents literal header values from being copied to
// reusable presets; environment variable references remain external.
func ValidateScenarioSecrets(s config.Scenario) error {
	base, err := url.Parse(s.BaseURL)
	if err != nil || base.User != nil || strings.ContainsAny(s.BaseURL, "\r\n") {
		return errors.New("campaign profile base_url must not contain credentials or control characters")
	}
	workflows := make([][]config.Step, 0, len(s.Workflows)+len(s.Profiles)+1)
	workflows = append(workflows, s.Steps)
	for _, workflow := range s.Workflows {
		workflows = append(workflows, workflow.Steps)
	}
	for _, profile := range s.Profiles {
		workflows = append(workflows, profile.Steps)
	}
	for _, steps := range workflows {
		for _, step := range steps {
			if strings.ContainsAny(step.Path, "\r\n") {
				return errors.New("campaign profile paths must not contain credentials or control characters")
			}
			if _, query, ok := strings.Cut(step.Path, "?"); ok {
				values, parseErr := url.ParseQuery(query)
				if parseErr != nil {
					return errors.New("campaign profile path has invalid query data")
				}
				for key := range values {
					normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
					if strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "authorization") || normalized == "api_key" {
						return fmt.Errorf("campaign profile query parameter %q may contain a credential", key)
					}
				}
			}
			for name, value := range step.Headers {
				if !envReference.MatchString(value) {
					return fmt.Errorf("cannot save literal %s header; use an environment variable reference", name)
				}
			}
		}
	}
	return nil
}

// Decode migrates schema-less and schema-v0 presets in memory to schema v1.
// Unknown future versions and unknown fields fail closed.
func Decode(data []byte) (Profile, bool, error) {
	if err := jsonartifact.RejectDuplicateKeys(data); err != nil {
		return Profile{}, false, fmt.Errorf("campaign profile contains ambiguous JSON: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil || envelope == nil {
		return Profile{}, false, errors.New("campaign profile must be a JSON object")
	}
	version := 0
	if raw, ok := envelope["schema_version"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &version) != nil || version < 0 {
			return Profile{}, false, errors.New("campaign profile has an invalid schema_version")
		}
	}
	if version > CurrentSchemaVersion {
		return Profile{}, false, fmt.Errorf("unsupported campaign profile schema %d (maximum %d)", version, CurrentSchemaVersion)
	}
	if version == CurrentSchemaVersion {
		if err := requireProfileFields(envelope); err != nil {
			return Profile{}, false, err
		}
	}
	var p Profile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return Profile{}, false, fmt.Errorf("decode campaign profile: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Profile{}, false, errors.New("campaign profile contains trailing JSON data")
	}
	if version == 0 {
		p.SchemaVersion = CurrentSchemaVersion
		if p.Options.Warmup == "" {
			p.Options.Warmup = "30s"
		}
		if p.Options.Stabilization == "" {
			p.Options.Stabilization = "0s"
		}
		if p.Options.Observation == "" {
			p.Options.Observation = "2m"
		}
		if p.Options.SpikeDuration == "" {
			p.Options.SpikeDuration = "0s"
		}
		if p.Options.Recovery == "" {
			p.Options.Recovery = "30s"
		}
		if p.Options.Soak == "" {
			p.Options.Soak = "0s"
		}
		if p.Options.Cooldown == "" {
			p.Options.Cooldown = "0s"
		}
		if p.Options.Repeats == 0 {
			p.Options.Repeats = 2
		}
		if p.Options.Stability == (campaign.StabilityPolicy{}) {
			p.Options.Stability = campaign.DefaultStabilityPolicy()
		}
		if p.Options.Budget == "" {
			p.Options.Budget = "0s"
			o, err := p.parseCampaignOptions(false)
			if err != nil {
				return Profile{}, true, err
			}
			o.Budget = campaign.PlannedBudget(o)
			p.Options.Budget = o.Budget.String()
		}
	}
	if err := Validate(p); err != nil {
		return Profile{}, version == 0, err
	}
	return p, version == 0, nil
}

// Current profiles require explicit persisted controls. Zero-valued controls
// remain valid when written; omission must not silently select Go zero values.
func requireProfileFields(envelope map[string]json.RawMessage) error {
	var scenario, options map[string]json.RawMessage
	if err := json.Unmarshal(envelope["scenario"], &scenario); err != nil || scenario == nil {
		return errors.New("campaign profile scenario is missing or invalid")
	}
	if err := json.Unmarshal(envelope["options"], &options); err != nil || options == nil {
		return errors.New("campaign profile options are missing or invalid")
	}
	required := func(fields map[string]json.RawMessage, names ...string) bool {
		for _, name := range names {
			value, ok := fields[name]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
		return true
	}
	if !required(envelope, "name", "scenario", "options") ||
		!required(scenario, "name", "base_url", "users", "duration", "ramp_up", "think_time", "request_timeout", "max_rps", "p95_limit_ms", "max_error_rate") ||
		!required(options, "warmup", "stabilization", "observation", "spike_duration", "recovery", "soak", "cooldown", "repeats", "budget", "stability_policy") {
		return errors.New("campaign profile omits required persisted fields")
	}
	var stability map[string]json.RawMessage
	if err := json.Unmarshal(options["stability_policy"], &stability); err != nil || stability == nil ||
		!required(stability, "minimum_windows", "minimum_samples_per_window", "max_p95_increase_percent", "max_p95_increase_ms", "max_throughput_drop_percent") {
		return errors.New("campaign profile stability policy omits required fields")
	}
	return nil
}

func Validate(p Profile) error {
	if p.SchemaVersion != CurrentSchemaVersion || !ValidName(p.Name) || p.Scenario.Name == "" || p.Scenario.BaseURL == "" {
		return errors.New("campaign profile metadata or scenario is invalid")
	}
	if err := ValidateScenarioSecrets(p.Scenario); err != nil {
		return err
	}
	o, err := p.CampaignOptions()
	if err != nil {
		return err
	}
	if o.LongRun && p.Scenario.Environment != "staging" {
		return errors.New("long-run profiles require scenario environment to be explicitly declared as staging")
	}
	if len(p.Description) > 500 {
		return errors.New("campaign profile description exceeds its 500-byte limit")
	}
	for _, stamp := range []string{p.CreatedAt, p.UpdatedAt} {
		if stamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return errors.New("campaign profile timestamp is invalid")
			}
		}
	}
	return nil
}
