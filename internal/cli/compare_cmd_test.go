package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/comparison"
)

func TestCompareCmdUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"compare", "--before", "x"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: damocline compare") {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestCompareCmdSameCampaignWritesOutputs(t *testing.T) {
	src := filepath.Join("..", "capacityplan", "testdata", "campaigns", "schema-v4.json")
	dir := t.TempDir()
	beforeDir := filepath.Join(dir, "before")
	if err := os.Mkdir(beforeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beforeDir, "campaign.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	jsonOut, mdOut := filepath.Join(dir, "cmp.json"), filepath.Join(dir, "cmp.md")
	var out, errOut bytes.Buffer
	code := Run([]string{"compare", "--before", beforeDir, "--after", src, "--out", jsonOut, "--report-out", mdOut}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var result comparison.Result
	raw, err := os.ReadFile(jsonOut)
	if err != nil || json.Unmarshal(raw, &result) != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Comparability != comparison.Comparable || result.BeforeSource != "before/campaign.json" {
		t.Fatalf("result = %+v", result)
	}
	for _, d := range result.Deltas {
		if d.Verdict == comparison.VerdictImproved || d.Verdict == comparison.VerdictRegressed {
			t.Fatalf("identical campaigns produced %s for %s %s", d.Verdict, d.Scope, d.Metric)
		}
	}
	if md, err := os.ReadFile(mdOut); err != nil || !strings.Contains(string(md), "## Measured facts") {
		t.Fatalf("markdown: %v", err)
	}
	if code := Run([]string{"compare", "--before", beforeDir, "--after", src, "--out", jsonOut}, &out, &errOut); code != 2 {
		t.Fatalf("overwrite allowed: code=%d", code)
	}
}
