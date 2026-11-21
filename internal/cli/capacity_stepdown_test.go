package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func stepDownExplore() explorer.Result {
	pass := func(users int) explorer.Probe {
		return explorer.Probe{Users: users, Status: "PASS", Runs: make([]report.Result, 2)}
	}
	return explorer.Result{Repeats: 2, Probes: []explorer.Probe{
		pass(25), pass(50), pass(100), pass(150),
		{Users: 125, Status: "PASS", Runs: make([]report.Result, 1)}, // incomplete repeats
		{Users: 175, Status: "FAIL", Runs: make([]report.Result, 2)},
		pass(100), // duplicate refinement entry
	}}
}

func TestCapacityStepDownLevelsAreLowerRepeatedPasses(t *testing.T) {
	if got := capacityStepDownLevels(stepDownExplore(), 150, capacityMaxStepDowns); !reflect.DeepEqual(got, []int{100, 50}) {
		t.Fatalf("step-down levels = %v", got)
	}
	result := stepDownExplore()
	result.Probes[4].Runs = make([]report.Result, 2)
	if got := capacityStepDownLevels(result, 150, capacityMaxStepDowns); !reflect.DeepEqual(got, []int{125, 100}) {
		t.Fatalf("calibrated 125 must be preferred: %v", got)
	}
	if got := capacityStepDownLevels(stepDownExplore(), 25, capacityMaxStepDowns); len(got) != 0 {
		t.Fatalf("no level below the lowest pass: %v", got)
	}
}

func TestCapacityStepDownOnlyAfterTargetFailure(t *testing.T) {
	for outcome, want := range map[string]bool{"FAIL": true, "PASS": false, "INCONCLUSIVE": false, "ABORTED": false} {
		if got := capacityStepDownAllowed(campaign.Result{ExecutionState: "COMPLETED", Outcome: outcome}); got != want {
			t.Errorf("%s: step-down=%v", outcome, got)
		}
	}
	if capacityStepDownAllowed(campaign.Result{ExecutionState: "RUNNING", Outcome: "FAIL"}) {
		t.Error("running campaign triggered a step-down")
	}
}

func TestCapacityLadderReportsHighestSustainedPass(t *testing.T) {
	ladder := capacityLadder{SchemaVersion: 1, ExploreStage: "20s", MaxStepDowns: 2, Entries: []capacityLadderEntry{
		{Users: 150, Campaign: "campaign.json", Outcome: "FAIL"},
		{Users: 100, Campaign: "campaign-stepdown-100.json", Outcome: "PASS"},
	}}
	finishCapacityLadder(&ladder)
	if ladder.SustainableUsers == nil || *ladder.SustainableUsers != 100 || ladder.AuditedCampaign != "campaign-stepdown-100.json" {
		t.Fatalf("unexpected ladder selection: %+v", ladder)
	}
	md := renderCapacityLadderMarkdown(ladder)
	for _, want := range []string{"Sustainable level: 100 VUs", "150 VUs passed the short explore stages but FAILED the sustained campaign", "explore stages lasted 20s"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}

	dir := t.TempDir()
	mdSource, htmlSource := filepath.Join(dir, "a.md"), filepath.Join(dir, "a.html")
	os.WriteFile(mdSource, []byte("# Audit\n"), 0600)
	os.WriteFile(htmlSource, []byte("<main>\n<header>\n<h1>x</h1>\n</header>\n<p>rest</p>\n</main>\n"), 0600)
	mdOut, htmlOut := filepath.Join(dir, "capacity.md"), filepath.Join(dir, "capacity.html")
	if err := writeCapacityLadderReports(mdSource, htmlSource, mdOut, htmlOut, ladder); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(htmlOut)
	if !strings.Contains(string(page), "</header>\n<section aria-labelledby=\"step-down\">") || !strings.Contains(string(page), "Sustainable level: 100 VUs") {
		t.Fatalf("html lacks step-down section:\n%s", page)
	}
	if err := writeCapacityLadderReports(mdSource, htmlSource, mdOut, htmlOut, ladder); err != nil {
		t.Fatalf("existing reports must be kept on resume: %v", err)
	}
}

func TestCapacityLadderStatesWhenNothingSustains(t *testing.T) {
	ladder := capacityLadder{SchemaVersion: 1, ExploreStage: "20s", MaxStepDowns: 2, Entries: []capacityLadderEntry{
		{Users: 150, Campaign: "campaign.json", Outcome: "FAIL"},
		{Users: 100, Campaign: "campaign-stepdown-100.json", Outcome: "FAIL"},
		{Users: 50, Campaign: "campaign-stepdown-50.json", Outcome: "FAIL"},
	}}
	finishCapacityLadder(&ladder)
	if ladder.SustainableUsers != nil || ladder.AuditedCampaign != "campaign.json" {
		t.Fatalf("unexpected selection: %+v", ladder)
	}
	if md := renderCapacityLadderMarkdown(ladder); !strings.Contains(md, "No sustained campaign passed") {
		t.Fatalf("missing no-pass statement:\n%s", md)
	}
	dir := t.TempDir()
	if err := report.WriteAnyJSON(filepath.Join(dir, capacityLadderFile), ladder); err != nil {
		t.Fatal(err)
	}
	saved, err := readCapacityLadder(dir)
	if err != nil || saved == nil || saved.AuditedCampaign != "campaign.json" {
		t.Fatalf("ledger round trip: %+v %v", saved, err)
	}
}
