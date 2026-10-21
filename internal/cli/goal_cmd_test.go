package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestGoalCommandUsesSavedExploreWithoutGeneratingTraffic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "capacity.json")
	current := report.Result{
		SchemaVersion: 3, Outcome: "PASS", ElapsedSeconds: 30,
		Config:  report.RunConfig{Users: 30, Mode: "closed", MaxRPS: 100, SeparateSessions: true},
		Metrics: report.MetricSet{Total: 30, Success: 30},
		Generator: &generatorhealth.Summary{Samples: 4, GoCPUValid: true, GoCPUUtilizationRatio: 0.22,
			ProcessSamples: 4, ProcessCPUSamples: 3, ProcessCPUValid: true, PeakProcessRSSBytes: 1024},
	}
	highest := 30
	r := explorer.Result{SchemaVersion: 1, Scenario: "synthetic app", StageDuration: "30s",
		MinUsers: 2, MaxUsers: 50, Repeats: 2, HighestPassingUsers: &highest,
		Probes: []explorer.Probe{{Users: 30, Status: "PASS", Runs: []report.Result{current, current}}},
	}
	if err := report.WriteAnyJSON(file, r); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	exit := Run([]string{"goal", "--file", file, "--peak-users", "20", "--growth-percent", "25", "--reserve-percent", "20"}, &out, &errOut)
	if exit != 0 {
		t.Fatalf("exit %d: %s", exit, errOut.String())
	}
	for _, want := range []string{"DEMAND TO VALIDATE: 30", "SHORT_TRIAL_TARGET_PASSED", "not a certified server limit", "No load was generated", "upgrade is NOT justified"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q: %s", want, out.String())
		}
	}
}

func TestGoalCommandKeepsArrivalDemandInScheduledStartsPerSecond(t *testing.T) {
	file := filepath.Join(t.TempDir(), "arrival-capacity.json")
	saved := arrivalexplorer.Result{
		SchemaVersion: 1, Scenario: "synthetic arrival", StageDuration: "10s", Repeats: 2,
		WorkerLimit: 4, MinJourneysPerSecond: 1, MaxJourneysPerSecond: 1,
		Outcome: "PASSED_TO_CONFIGURED_CEILING",
		Probes:  []arrivalexplorer.Probe{{JourneysPerSecond: 1, Status: "PASS_OBSERVED", Runs: []report.Result{}}},
	}
	if err := report.WriteAnyJSON(file, saved); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"goal", "--file", file, "--peak-journeys-per-second", "2", "--growth-percent", "25", "--reserve-percent", "20"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit %d: %s", exit, stderr.String())
	}
	for _, want := range []string{"DEMAND TO VALIDATE: 3 scheduled journey starts/s", "OUTSIDE_TESTED_CEILING", "VUs, admitted journeys, completed journeys, and HTTP requests/s are separate", "No load was generated"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q: %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if exit = Run([]string{"goal", "--file", file, "--peak-users", "2"}, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "VUs cannot be substituted") {
		t.Fatalf("arrival artifact accepted VU demand: exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

func TestGoalCommandWritesOpenArrivalObjectiveReportOffline(t *testing.T) {
	dir := t.TempDir()
	campaignPath := filepath.Join(dir, "arrival-campaign.json")
	contract := campaign.ObjectiveContract{Version: 1, JourneyScope: "AGGREGATE_AND_EACH_WORKFLOW", JourneyP95LimitMS: 500,
		MaxJourneyErrorRate: .1, MinSamplesPerStep: 1, MinCompletedWorkRatio: 1, MinimumObservationWindows: 3,
		MinimumSamplesPerWindow: 20, WindowDuration: "10s"}
	policy := campaign.DefaultStabilityPolicy()
	saved := arrivalcampaign.Result{SchemaVersion: arrivalcampaign.CurrentSchemaVersion, ExecutionState: "COMPLETED", Scenario: "synthetic", Target: "http://127.0.0.1:18080",
		StartedAt: "2026-10-20T12:00:00Z", WorkloadFingerprint: "sha256:synthetic", WorkerLimit: 2, RequestTimeout: "2s", RequestRateCeiling: 20,
		P95LimitMS: 500, MaxErrorRate: .1, MinSamplesPerStep: 1, WindowDuration: "2s", BaseStartsPerSecond: 1,
		Warmup: "0s", Stabilization: "0s", Observation: "10s", Recovery: "10s", Soak: "0s", Cooldown: "0s", Budget: "2m40s", Repeats: 2, Outcome: "ABORTED",
		Objectives: &contract, StabilityPolicy: &policy,
		Trials: []arrivalcampaign.Trial{{Number: 1, RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Outcome: "ABORTED", AbortReason: "The campaign was interrupted by its caller."}}}
	if err := report.WriteAnyJSON(campaignPath, saved); err != nil {
		t.Fatal(err)
	}
	assessmentPath := filepath.Join(dir, "assessment.json")
	markdownPath := filepath.Join(dir, "capacity.md")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"goal", "--file", campaignPath, "--peak-journeys-per-second", "1", "--out", assessmentPath, "--report-out", markdownPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("goal exited %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Open-arrival capacity assessment", "Predeclared objectives", "maximum dropped arrivals: 0", "maximum p95 increase", "Offline replay", "--peak-journeys-per-second 1"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("capacity report missing %q:\n%s", want, data)
		}
	}
	assessmentData, err := os.ReadFile(assessmentPath)
	if err != nil || !strings.Contains(string(assessmentData), `"predeclared_objectives"`) || !strings.Contains(string(assessmentData), `"stability_policy"`) {
		t.Fatalf("assessment did not retain its replay criteria: err=%v json=%s", err, assessmentData)
	}
}

func TestGoalCommandRejectsInvalidJSONAndMissingFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if c := Run([]string{"goal"}, &out, &errOut); c != 2 {
		t.Fatalf("missing flags: exit %d", c)
	}
	out.Reset()
	errOut.Reset()
	p := filepath.Join(t.TempDir(), "not-a-report.json")
	if err := report.WriteAnyJSON(p, map[string]any{"hello": "world"}); err != nil {
		t.Fatal(err)
	}
	if c := Run([]string{"goal", "--file", p, "--peak-users", "20"}, &out, &errOut); c != 2 || !strings.Contains(errOut.String(), "invalid or empty capacity exploration") {
		t.Fatalf("invalid JSON schema not rejected: exit %d stderr %s", c, errOut.String())
	}
}
