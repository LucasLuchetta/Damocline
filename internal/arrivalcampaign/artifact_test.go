package arrivalcampaign

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArtifactJSONRequiresPersistedEvidence(t *testing.T) {
	valid := map[string]any{
		"arrival_campaign_schema_version": CurrentSchemaVersion,
		"execution_state":                 "COMPLETED",
		"scenario":                        "synthetic",
		"target":                          "https://app.example.com",
		"started_at":                      "2026-10-21T00:00:00Z",
		"journey_worker_limit":            0,
		"workload_fingerprint":            "fixture",
		"request_timeout":                 "1s",
		"max_rps":                         0,
		"p95_limit_ms":                    0,
		"max_error_rate":                  0,
		"min_samples_per_step":            0,
		"window_duration":                 "",
		"base_scheduled_journey_starts_per_second": 0,
		"warmup":           "0s",
		"observation":      "10s",
		"recovery":         "10s",
		"cooldown":         "0s",
		"budget":           "1m",
		"repeats":          2,
		"trials":           []any{map[string]any{"run_id": strings.Repeat("a", 32), "number": 1, "phases": []any{map[string]any{"name": "observation", "evidence_state": "PASS_OBSERVED", "run": map[string]any{"schema_version": 32}}}, "outcome": "PASS"}},
		"outcome":          "PASS",
		"objectives":       map[string]any{},
		"stability_policy": map[string]any{},
	}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifactJSON(encoded); err != nil {
		t.Fatalf("explicit zero values and complete nested evidence were rejected: %v", err)
	}
}

func TestValidateArtifactJSONRejectsMissingCurrentAndNestedFields(t *testing.T) {
	base := `{"arrival_campaign_schema_version":8,"execution_state":"COMPLETED","scenario":"synthetic","target":"https://app.example.com","started_at":"2026-10-21T00:00:00Z","journey_worker_limit":2,"workload_fingerprint":"fixture","request_timeout":"1s","max_rps":10,"p95_limit_ms":100,"max_error_rate":0.1,"min_samples_per_step":1,"window_duration":"2s","base_scheduled_journey_starts_per_second":1,"warmup":"0s","observation":"10s","recovery":"10s","cooldown":"0s","budget":"1m","repeats":2,"trials":[],"outcome":"INCONCLUSIVE","objectives":{},"stability_policy":{}}`
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "root field", data: strings.Replace(base, `,"budget":"1m"`, "", 1), wantErr: `"budget"`},
		{name: "schema field", data: strings.Replace(base, `,"objectives":{},"stability_policy":{}`, `,"stability_policy":{}`, 1), wantErr: `"objectives"`},
		{name: "trial number", data: strings.Replace(base, `"trials":[]`, `"trials":[{"phases":[],"outcome":"ABORTED"}]`, 1), wantErr: `"number"`},
		{name: "phase name", data: strings.Replace(base, `"trials":[]`, `"trials":[{"run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","number":1,"phases":[{"evidence_state":"PASS_OBSERVED","run":{"schema_version":32}}],"outcome":"PASS"}]`, 1), wantErr: `"name"`},
		{name: "run schema", data: strings.Replace(base, `"trials":[]`, `"trials":[{"run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","number":1,"phases":[{"name":"observation","evidence_state":"PASS_OBSERVED","run":{}}],"outcome":"PASS"}]`, 1), wantErr: `"schema_version"`},
		{name: "pending trial", data: strings.Replace(base, `,"outcome":"INCONCLUSIVE","objectives"`, `,"pending_trial":{"number":1,"outcome":"IN_PROGRESS"},"outcome":"INCONCLUSIVE","objectives"`, 1), wantErr: `"phases"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactJSON([]byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateArtifactJSON() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateArtifactJSONPreservesHistoricalSchemaSemantics(t *testing.T) {
	legacy := `{"arrival_campaign_schema_version":4,"scenario":"synthetic","target":"https://app.example.com","started_at":"2026-10-21T00:00:00Z","journey_worker_limit":2,"workload_fingerprint":"fixture","request_timeout":"1s","max_rps":10,"p95_limit_ms":100,"max_error_rate":0.1,"min_samples_per_step":1,"window_duration":"2s","base_scheduled_journey_starts_per_second":1,"warmup":"0s","observation":"10s","recovery":"10s","budget":"1m","repeats":2,"trials":[{"number":1,"phases":null,"outcome":"ABORTED"}],"outcome":"ABORTED"}`
	if err := ValidateArtifactJSON([]byte(legacy)); err != nil {
		t.Fatalf("historical aborted trial with no completed phases was rejected: %v", err)
	}
}

func TestValidateArtifactJSONRejectsDuplicateKeysAndNullArrays(t *testing.T) {
	duplicate := `{"arrival_campaign_schema_version":8,"arrival_campaign_schema_version":8}`
	if err := ValidateArtifactJSON([]byte(duplicate)); err == nil || !strings.Contains(err.Error(), "ambiguous JSON") {
		t.Fatalf("duplicate keys accepted: %v", err)
	}
	current := `{"arrival_campaign_schema_version":8,"execution_state":"COMPLETED","scenario":"synthetic","target":"https://app.example.com","started_at":"2026-10-21T00:00:00Z","journey_worker_limit":2,"workload_fingerprint":"fixture","request_timeout":"1s","max_rps":10,"p95_limit_ms":100,"max_error_rate":0.1,"min_samples_per_step":1,"window_duration":"2s","base_scheduled_journey_starts_per_second":1,"warmup":"0s","observation":"10s","recovery":"10s","cooldown":"0s","budget":"1m","repeats":2,"trials":null,"outcome":"INCONCLUSIVE","objectives":{},"stability_policy":{}}`
	if err := ValidateArtifactJSON([]byte(current)); err == nil || !strings.Contains(err.Error(), `"trials"`) {
		t.Fatalf("null trials accepted as an empty array: %v", err)
	}
	currentWithNullPhases := strings.Replace(current, `"trials":null`, `"trials":[{"run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","number":1,"phases":null,"outcome":"ABORTED"}]`, 1)
	if err := ValidateArtifactJSON([]byte(currentWithNullPhases)); err == nil || !strings.Contains(err.Error(), "phases must be a JSON array") {
		t.Fatalf("current null phase ledger accepted: %v", err)
	}
}
