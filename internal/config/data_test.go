package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDataScenario(t *testing.T, csv, mode, path string, users int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rows.csv"), []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}
	modeField := ""
	if mode != "" {
		modeField = `,"mode":"` + mode + `"`
	}
	scenario := `{"scenario_version":1,"name":"data","base_url":"http://127.0.0.1:9","users":` + itoa(users) + `,"duration":"1s","request_timeout":"1s","max_rps":10,"p95_limit_ms":1000,"max_error_rate":0.1,
"data":{"file":"rows.csv"` + modeField + `},
"workflows":[{"name":"w","weight":1,"steps":[{"name":"s","path":"` + path + `","headers":{"X-Item":"${data.item}"}}]}]}`
	file := filepath.Join(dir, "scenario.json")
	if err := os.WriteFile(file, []byte(scenario), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func itoa(n int) string { return string(rune('0' + n)) }

func parseDataScenario(t *testing.T, file string) (Validated, error) {
	t.Helper()
	s, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(s, Options{Authorized: true})
}

func TestDataSourceRewritesPlaceholdersAndResolvesRelativeFile(t *testing.T) {
	v, err := parseDataScenario(t, writeDataScenario(t, string([]byte{0xef, 0xbb, 0xbf})+"user,item\nalpha,1\nbeta,2\n", "", "/users/${data.user}?item=${data.item}", 1))
	if err != nil {
		t.Fatal(err)
	}
	step := v.Profiles[0].Steps[0]
	if step.PathTemplate != "/users/{{data_user}}?item={{data_item}}" || step.ResolvedHeaders["X-Item"] != "{{data_item}}" {
		t.Fatalf("placeholders were not rewritten: %q %v", step.PathTemplate, step.ResolvedHeaders)
	}
	if v.Data == nil || v.Data.Mode != DataModeSequential || len(v.Data.Rows) != 2 || !strings.HasPrefix(v.Data.SHA256, "sha256:") {
		t.Fatalf("unexpected data set: %+v", v.Data)
	}
	vars := map[string]string{}
	v.Data.Variables(v.Data.Select(0, 3, 0), vars)
	if vars["data_user"] != "alpha" || vars["data_item"] != "1" {
		t.Fatalf("sequential selection must wrap: %v", vars)
	}
}

func TestDataSourceValidationErrors(t *testing.T) {
	cases := []struct {
		name, csv, mode, path string
		users                 int
		want                  string
	}{
		{"unknown column", "user,item\na,1\n", "", "/u/${data.missing}", 1, "names no column"},
		{"bad mode", "user,item\na,1\n", "shuffle", "/u/${data.user}", 1, "data.mode"},
		{"no rows", "user,item\n", "", "/u/${data.user}", 1, "at least one data row"},
		{"duplicate column", "user,user\na,b\n", "", "/u/${data.user}", 1, "must be unique"},
		{"ragged row", "user,item\na\n", "", "/u/${data.user}", 1, "invalid CSV"},
		{"unique too few rows", "user,item\na,1\n", "unique_per_vu", "/u/${data.user}", 2, "at least 2 rows"},
		{"partial segment", "user,item\na,1\n", "", "/u/x-${data.user}", 1, "complete path segment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseDataScenario(t, writeDataScenario(t, c.csv, c.mode, c.path, c.users))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestDataPlaceholderWithoutSourceIsRejected(t *testing.T) {
	s := Scenario{ScenarioVersion: 1, Name: "x", BaseURL: "http://127.0.0.1:9", Users: 1, Duration: "1s", MaxRPS: 5, P95LimitMS: 100,
		Workflows: []Profile{{Name: "w", Weight: 1, Steps: []Step{{Name: "s", Path: "/u/${data.user}"}}}}}
	if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "require a data source") {
		t.Fatalf("expected missing data source error, got %v", err)
	}
}

func TestDataSelectionModesAreDeterministic(t *testing.T) {
	d := &DataSet{Mode: DataModeUniquePerVU, Columns: []string{"c"}, Rows: [][]string{{"a"}, {"b"}, {"c"}}}
	if d.Select(1, 99, 0)[0] != "b" {
		t.Fatal("unique_per_vu must bind a row to the virtual user")
	}
	d.Mode = DataModeRandom
	first := []string{}
	for i := uint64(1); i <= 20; i++ {
		first = append(first, d.Select(0, i, 7)[0])
	}
	for i := uint64(1); i <= 20; i++ {
		if d.Select(0, i, 7)[0] != first[i-1] {
			t.Fatal("random selection must be reproducible for one seed")
		}
	}
}

func TestDataSourceChangesWorkloadFingerprint(t *testing.T) {
	a, err := parseDataScenario(t, writeDataScenario(t, "user,item\na,1\n", "", "/u/${data.user}", 1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseDataScenario(t, writeDataScenario(t, "user,item\nb,2\n", "", "/u/${data.user}", 1))
	if err != nil {
		t.Fatal(err)
	}
	if a.WorkloadFingerprint == "" || a.WorkloadFingerprint == b.WorkloadFingerprint {
		t.Fatal("different CSV bytes must produce different workload fingerprints")
	}
}
