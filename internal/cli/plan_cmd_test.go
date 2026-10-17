package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestPlanCommandAssessesSavedCampaignWithoutGeneratingTraffic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "campaign.json")
	passed := report.Result{Outcome: "PASS"}
	evidence := campaign.Result{
		SchemaVersion: 1, Scenario: "synthetic app", BaseUsers: 30, Repeats: 2, Outcome: "PASS",
		Trials: []campaign.Trial{
			{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: passed}, {Name: "recovery", Run: passed}}},
			{Number: 2, Outcome: "PASS", Phases: []campaign.Phase{{Name: "observation", Run: passed}, {Name: "recovery", Run: passed}}},
		},
	}
	if err := report.WriteAnyJSON(file, evidence); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	exit := Run([]string{"plan", "--campaign", file, "--peak-users", "20", "--growth-percent", "25", "--reserve-percent", "20"}, &out, &errOut)
	if exit != 0 {
		t.Fatalf("exit %d: %s", exit, errOut.String())
	}
	for _, want := range []string{"Demand to validate: 30", "GOAL_MET_UNDER_TESTED_CONDITIONS", "No load was generated", "does not diagnose a bottleneck"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q: %s", want, out.String())
		}
	}
}

func TestPlanCommandRejectsInvalidCampaign(t *testing.T) {
	file := filepath.Join(t.TempDir(), "campaign.json")
	if err := report.WriteAnyJSON(file, campaign.Result{SchemaVersion: 1, Scenario: "synthetic", BaseUsers: 30, Repeats: 2, Outcome: "PASS"}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if exit := Run([]string{"plan", "--campaign", file, "--peak-users", "20"}, &out, &errOut); exit != 0 {
		t.Fatalf("exit %d: %s", exit, errOut.String())
	}
	if !strings.Contains(out.String(), "INSUFFICIENT_EVIDENCE") || !strings.Contains(out.String(), "incomplete or malformed") {
		t.Fatalf("invalid evidence was accepted: %s", out.String())
	}
}
