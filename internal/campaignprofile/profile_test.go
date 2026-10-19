package campaignprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func fixtureProfile(t *testing.T) Profile {
	t.Helper()
	profile, err := New("checkout", "synthetic local fixture", config.Scenario{Name: "fixture", BaseURL: "http://127.0.0.1:8080", Users: 2, Duration: "10s", Timeout: "1s", MaxRPS: 10, P95LimitMS: 1000, MaxErrorRate: 1, Steps: []config.Step{{Name: "read", Method: "GET", Path: "/"}}}, campaign.Options{Warmup: 0, Observation: 10 * time.Second, Recovery: 0, Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestProfileRoundTripAndRuntimeConsentAbsent(t *testing.T) {
	want := fixtureProfile(t)
	got, migrated, err := Decode(mustJSON(t, want))
	if err != nil || migrated {
		t.Fatalf("Decode()=(%+v,%v,%v)", got, migrated, err)
	}
	if got.Name != want.Name || got.Options != want.Options || got.Scenario.BaseURL != want.Scenario.BaseURL {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	raw := string(mustJSON(t, got))
	for _, forbidden := range []string{"authorized", "allow_writes", "staging_confirmed", "calibration_source"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("profile persisted runtime-only field %q", forbidden)
		}
	}
}

func TestV0MigrationAndFutureSchemaRejected(t *testing.T) {
	legacy := fixtureProfile(t)
	legacy.SchemaVersion = 0
	legacy.Options.Budget = ""
	legacy.Options.Warmup = ""
	legacy.Options.Stability = campaign.StabilityPolicy{}
	data := mustJSON(t, legacy)
	got, migrated, err := Decode(data)
	if err != nil || !migrated {
		t.Fatalf("migration=(%+v,%v,%v)", got, migrated, err)
	}
	if got.SchemaVersion != CurrentSchemaVersion || got.Options.Budget == "" || got.Options.Warmup != "30s" {
		t.Fatalf("migration defaults missing: %+v", got.Options)
	}
	if _, _, err := Decode([]byte(`{"schema_version":999}`)); err == nil {
		t.Fatal("accepted future schema")
	}
}

func TestDecodeRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	base := string(mustJSON(t, fixtureProfile(t)))
	for _, data := range []string{strings.Replace(base, `"name":`, `"surprise":1,"name":`, 1), base + ` {}`} {
		if _, _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("accepted malformed extension: %s", data)
		}
	}
}

func TestSecretsMustRemainExternalReferences(t *testing.T) {
	p := fixtureProfile(t)
	p.Scenario.Steps[0].Headers = map[string]string{"Authorization": "Bearer abc"}
	if _, err := New("secret", "", p.Scenario, campaign.Options{Observation: 10 * time.Second, Repeats: 2}); err == nil {
		t.Fatal("accepted literal credential")
	}
	p.Scenario.Steps[0].Headers["Authorization"] = "${DAMOCLINE_TOKEN}"
	if err := ValidateScenarioSecrets(p.Scenario); err != nil {
		t.Fatal(err)
	}
}

func TestStorePrivateReplaceMigrateAndDelete(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "profiles")}
	p := fixtureProfile(t)
	if err := store.Save(p, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path(p.Name))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("profile permissions too broad: %o", info.Mode().Perm())
	}
	if err := store.Save(p, false); err == nil {
		t.Fatal("overwrote existing profile without explicit replace")
	}
	p.Description = "updated"
	if err := store.Save(p, true); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(p.Name)
	if err != nil || loaded.Description != "updated" {
		t.Fatalf("load after replacement: %+v %v", loaded, err)
	}
	legacy := loaded
	legacy.SchemaVersion = 0
	raw := mustJSON(t, legacy)
	if err := os.WriteFile(store.Path(p.Name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := store.Migrate(p.Name)
	if err != nil || !changed {
		t.Fatalf("Migrate()=(%v,%v)", changed, err)
	}
	loaded, changed, err = store.Load(p.Name)
	if err != nil || changed || loaded.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("migrated load=(%+v,%v,%v)", loaded, changed, err)
	}
	if err := store.Delete(p.Name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(p.Name); err == nil {
		t.Fatal("deleted profile still exists")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
