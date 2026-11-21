package insights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestSchemaV40ProfileMixFixture(t *testing.T) {
	fixture := filepath.Join("testdata", "reports", "schema-v40.json")
	r, err := ReadRun(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if r.ProfileMix == nil || r.ProfileMix.State != report.MixInsufficientSamples || r.ProfileMix.Reference == nil ||
		r.ProfileMix.Reference.Fit.State != report.ReferenceMatched || r.ProfileMix.Reference.Provenance != report.ReferenceProvenance ||
		len(r.Config.ProfileMixReferenceCounts) != 2 || !r.Config.Profiles[1].Writes || r.Config.Profiles[1].DataVariability != report.DataVariabilitySyntheticPerJourney {
		t.Fatalf("schema-v40 mix evidence was not preserved: %+v", r.ProfileMix)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"tampered fit": func(s string) string {
			return strings.Replace(s, `"state":"INSUFFICIENT_SAMPLES","workflows"`, `"state":"MIX_MATCHED","workflows"`, 1)
		},
		"downgraded schema": func(s string) string {
			return strings.Replace(s, `"schema_version": 40`, `"schema_version": 39`, 1)
		},
		"missing evidence": func(s string) string {
			start := strings.Index(s, `"profile_mix": {`)
			end := strings.Index(s, `"generator_identity"`)
			return s[:start] + s[end:]
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run.json")
			if err := os.WriteFile(path, []byte(mutate(string(data))), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRun(path); err == nil {
				t.Fatal("invalid workflow-mix evidence was accepted")
			}
		})
	}
}
