package campaignprofile

import (
	"encoding/json"
	"testing"
)

// TestDecodeRequiresEveryPersistedCurrentProfileField removes, and separately
// nulls, every field that requireProfileFields declares for the current
// schema so omission can never silently select a Go zero value.
func TestDecodeRequiresEveryPersistedCurrentProfileField(t *testing.T) {
	base := mustJSON(t, fixtureProfile(t))
	if _, _, err := Decode(base); err != nil {
		t.Fatalf("current fixture profile was rejected: %v", err)
	}
	groups := []struct {
		path   []string
		fields []string
	}{
		{nil, []string{"name", "scenario", "options"}},
		{[]string{"scenario"}, []string{"name", "base_url", "users", "duration", "ramp_up", "think_time", "request_timeout", "max_rps", "p95_limit_ms", "max_error_rate"}},
		{[]string{"options"}, []string{"warmup", "stabilization", "observation", "spike_duration", "recovery", "soak", "cooldown", "repeats", "budget", "stability_policy"}},
		{[]string{"options", "stability_policy"}, []string{"minimum_windows", "minimum_samples_per_window", "max_p95_increase_percent", "max_p95_increase_ms", "max_throughput_drop_percent"}},
	}
	for _, group := range groups {
		for _, field := range group.fields {
			for _, null := range []bool{false, true} {
				var root map[string]any
				if err := json.Unmarshal(base, &root); err != nil {
					t.Fatal(err)
				}
				target := root
				for _, key := range group.path {
					target = target[key].(map[string]any)
				}
				if _, ok := target[field]; !ok {
					t.Fatalf("fixture lacks %v.%s", group.path, field)
				}
				if null {
					target[field] = nil
				} else {
					delete(target, field)
				}
				if _, _, err := Decode(mustJSON(t, root)); err == nil {
					t.Fatalf("current profile without %v.%s (null=%v) was accepted", group.path, field, null)
				}
			}
		}
	}
}
