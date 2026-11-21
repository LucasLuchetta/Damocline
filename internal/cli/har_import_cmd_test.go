package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestImportHARCommandWritesValidDraftAndReportsDrops(t *testing.T) {
	dir := t.TempDir()
	har := filepath.Join(dir, "capture.har")
	input := `{"log":{"entries":[
	 {"request":{"method":"GET","url":"https://app.example.com/home","headers":[{"name":"Authorization","value":"Bearer synthetic-fixture"}]},"response":{"status":200}},
	 {"request":{"method":"GET","url":"https://app.example.com/site.css"},"response":{"status":200}},
	 {"request":{"method":"POST","url":"https://app.example.com/items"},"response":{"status":201}}
	]}}`
	if err := os.WriteFile(har, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "scenario.json")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"import-har", "--har", har, "--base-url", "https://app.example.com", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"static_asset: 1", "write_method_not_opted_in: 1", "DAMOCLINE_AUTHORIZATION", "No traffic was sent"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, stdout.String())
		}
	}
	raw, err := os.ReadFile(out)
	if err != nil || strings.Contains(string(raw), "synthetic-fixture") {
		t.Fatalf("draft unreadable or leaked a credential: %v %s", err, raw)
	}
	t.Setenv("DAMOCLINE_AUTHORIZATION", "synthetic")
	s, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse(s, config.Options{Authorized: true}); err != nil {
		t.Fatalf("draft must validate: %v", err)
	}
	if code := Run([]string{"import-har", "--har", har, "--base-url", "https://app.example.com", "--out", out}, &stdout, &stderr); code != 2 {
		t.Fatal("existing draft must not be overwritten")
	}
}
