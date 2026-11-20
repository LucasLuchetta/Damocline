package campaign_test

import (
	"encoding/json"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"strings"
	"testing"
)

func recordedObjectives() campaign.ObjectiveContract {
	return campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 100, MaxJourneyErrorRate: 0, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MaxDroppedArrivals: 0, MinimumObservationWindows: 3, MinimumSamplesPerWindow: 20, WindowDuration: "AUTO_PER_PHASE"}
}

func campaignObject(t *testing.T, arrival bool, schema int, objectives map[string]json.RawMessage) []byte {
	t.Helper()
	root := map[string]any{"schema_version": schema, "scenario": "synthetic", "base_users": 1, "target": "https://app.example.com", "started_at": "2026-01-01T00:00:00Z", "warmup": "0s", "observation": "10s", "recovery": "10s", "repeats": 2, "trials": []any{}, "outcome": "INCONCLUSIVE", "objectives": objectives}
	if arrival {
		delete(root, "schema_version")
		root["arrival_campaign_schema_version"] = schema
		for key, value := range map[string]any{"journey_worker_limit": 1, "workload_fingerprint": "fixture", "request_timeout": "1s", "max_rps": 10, "p95_limit_ms": 100, "max_error_rate": 0, "min_samples_per_step": 1, "window_duration": "2s", "base_scheduled_journey_starts_per_second": 1, "budget": "1m", "cooldown": "0s", "execution_state": "COMPLETED", "stability_policy": campaign.DefaultStabilityPolicy()} {
			root[key] = value
		}
	}
	root["stability_policy"] = campaign.DefaultStabilityPolicy()
	if !arrival && schema >= 9 {
		root["cooldown"] = "0s"
	}
	if !arrival && schema >= 10 {
		root["execution_state"] = "COMPLETED"
	}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCampaignReadersRejectOmittedStabilityThreshold(t *testing.T) {
	objectives, _ := json.Marshal(recordedObjectives())
	for _, arrival := range []bool{false, true} {
		schema, validate := 13, capacityplan.ValidateCampaignArtifact
		if arrival {
			schema, validate = 8, arrivalcampaign.ValidateArtifactJSON
		}
		data := campaignObject(t, arrival, schema, nil)
		var root map[string]json.RawMessage
		if err := json.Unmarshal(data, &root); err != nil {
			t.Fatal(err)
		}
		root["objectives"] = objectives
		root["stability_policy"] = json.RawMessage(`{"minimum_windows":3,"minimum_samples_per_window":20,"max_p95_increase_percent":0,"max_p95_increase_ms":0,"max_throughput_drop_percent":0}`)
		valid, _ := json.Marshal(root)
		if err := validate(valid); err != nil {
			t.Fatalf("explicit stability thresholds rejected: %v", err)
		}
		var policy map[string]json.RawMessage
		if err := json.Unmarshal(root["stability_policy"], &policy); err != nil {
			t.Fatal(err)
		}
		for field, value := range policy {
			delete(policy, field)
			root["stability_policy"], _ = json.Marshal(policy)
			missing, _ := json.Marshal(root)
			if err := validate(missing); err == nil || !strings.Contains(err.Error(), field) {
				t.Errorf("arrival=%v missing %s: %v", arrival, field, err)
			}
			policy[field] = json.RawMessage("null")
			root["stability_policy"], _ = json.Marshal(policy)
			null, _ := json.Marshal(root)
			if err := validate(null); err == nil || !strings.Contains(err.Error(), field) {
				t.Errorf("arrival=%v null %s: %v", arrival, field, err)
			}
			policy[field] = value
		}
	}
}

func TestCampaignReadersRejectOmittedObjectiveFields(t *testing.T) {
	contract := recordedObjectives()
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(contract)
	var original map[string]json.RawMessage
	_ = json.Unmarshal(data, &original)
	for _, tc := range []struct {
		name     string
		arrival  bool
		schema   int
		validate func([]byte) error
	}{
		{"closed boundary", false, 13, capacityplan.ValidateCampaignArtifact},
		{"closed current", false, campaign.CurrentSchemaVersion, capacityplan.ValidateCampaignArtifact},
		{"arrival current", true, 8, arrivalcampaign.ValidateArtifactJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.validate(campaignObject(t, tc.arrival, tc.schema, original)); err != nil {
				t.Fatalf("explicit zero criteria rejected: %v", err)
			}
			for field := range original {
				for _, null := range []bool{false, true} {
					candidate := make(map[string]json.RawMessage, len(original))
					for name, value := range original {
						candidate[name] = value
					}
					if null {
						candidate[field] = json.RawMessage(`null`)
					} else {
						delete(candidate, field)
					}
					if err := tc.validate(campaignObject(t, tc.arrival, tc.schema, candidate)); err == nil || !strings.Contains(err.Error(), field) {
						t.Fatalf("missing/null %s accepted: %v", field, err)
					}
				}
			}
		})
	}
}

func TestMissingZeroObjectiveRemainsSemanticallyPlausibleButIsNotRecorded(t *testing.T) {
	data, _ := json.Marshal(recordedObjectives())
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for _, name := range []string{"max_journey_error_rate", "max_dropped_arrivals"} {
		original := fields[name]
		delete(fields, name)
		candidate, _ := json.Marshal(fields)
		var decoded campaign.ObjectiveContract
		if err := json.Unmarshal(candidate, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("fixture no longer isolates zero-value ambiguity: %v", err)
		}
		if err := campaign.ValidateObjectiveArtifactJSON(candidate); err == nil {
			t.Fatalf("undeclared %s became an explicit zero criterion", name)
		}
		fields[name] = original
	}
}

func TestObjectivePresencePreservesOlderCampaignSchemas(t *testing.T) {
	for _, tc := range []struct {
		arrival  bool
		schema   int
		validate func([]byte) error
	}{{false, 12, capacityplan.ValidateCampaignArtifact}, {true, 7, arrivalcampaign.ValidateArtifactJSON}} {
		data := campaignObject(t, tc.arrival, tc.schema, nil)
		var root map[string]json.RawMessage
		_ = json.Unmarshal(data, &root)
		delete(root, "objectives")
		if tc.arrival {
			delete(root, "stability_policy")
		}
		data, _ = json.Marshal(root)
		if err := tc.validate(data); err != nil {
			t.Fatalf("historical campaign without objective contract rejected: %v", err)
		}
	}
	for _, invalid := range []string{`null`, `[]`, `true`, `{}`} {
		if err := campaign.ValidateObjectiveArtifactJSON([]byte(invalid)); err == nil {
			t.Fatalf("invalid objectives accepted: %s", invalid)
		}
	}
}
