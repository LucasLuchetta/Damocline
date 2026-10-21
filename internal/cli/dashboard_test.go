package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestLoadDashboardHandlerSupportsRunAndCampaignArtifacts(t *testing.T) {
	runPath := filepath.Join("..", "insights", "testdata", "reports", "schema-v25.json")
	runHandler, err := loadDashboardHandler(runPath)
	if err != nil {
		t.Fatal(err)
	}
	runResponse := httptest.NewRecorder()
	runHandler.ServeHTTP(runResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if runResponse.Code != http.StatusOK || !strings.Contains(runResponse.Body.String(), "Application-metric timeline") {
		t.Fatalf("run dashboard did not preserve its application timeline: %d %s", runResponse.Code, runResponse.Body.String())
	}

	campaignPath := filepath.Join(t.TempDir(), "campaign.json")
	phaseRun, err := insights.ReadRun(runPath)
	if err != nil {
		t.Fatal(err)
	}
	phaseRun.SchemaVersion = report.CurrentSchemaVersion
	phaseRun.GeneratorIdentity = report.GeneratorIdentity{OS: "linux", Architecture: "amd64", GoVersion: "go1.24.0", RunnerIdentityMethod: "git_commit", RunnerIdentity: strings.Repeat("a", 40)}
	phaseRun.GeneratorIdentity = report.GeneratorIdentity{OS: "linux", Architecture: "amd64", GoVersion: "go1.24.0", RunnerIdentityMethod: "git_commit", RunnerIdentity: strings.Repeat("a", 40)}
	phaseRun.StatisticalMethods = report.CurrentStatisticalMethods()
	phaseRun.Config.Profiles = nil
	phaseRun.Config.ProfileSelection = ""
	phaseRun.Profiles = nil
	phaseRun.Name, phaseRun.Target, phaseRun.Outcome = "Synthetic", "http://127.0.0.1", "PASS"
	phaseRun.Config.Users = 4
	phase := func(name string) campaign.Phase {
		return campaign.Phase{Name: name, EvidenceState: "LIMITED", Run: phaseRun}
	}
	saved := campaign.Result{SchemaVersion: 1, Scenario: "Synthetic", Target: "http://127.0.0.1", BaseUsers: 4, Repeats: 2, Outcome: "PASS",
		Trials: []campaign.Trial{{Number: 1, Outcome: "PASS", Phases: []campaign.Phase{phase("observation"), phase("recovery")}},
			{Number: 2, Outcome: "PASS", Phases: []campaign.Phase{phase("observation"), phase("recovery")}}}}
	if err := report.WriteAnyJSON(campaignPath, saved); err != nil {
		t.Fatal(err)
	}
	campaignHandler, err := loadDashboardHandler(campaignPath)
	if err != nil {
		t.Fatal(err)
	}
	campaignResponse := httptest.NewRecorder()
	campaignHandler.ServeHTTP(campaignResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, expected := range []string{"Closed-loop capacity", "virtual users (VUs)", "distinct workload units", "not independently prove sustainable capacity"} {
		if campaignResponse.Code != http.StatusOK || !strings.Contains(campaignResponse.Body.String(), expected) {
			t.Fatalf("campaign dashboard omitted %q: %d %s", expected, campaignResponse.Code, campaignResponse.Body.String())
		}
	}
}

func TestLoadDashboardHandlerSupportsOpenArrivalCheckpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	base, err := config.Parse(config.Scenario{Name: "Synthetic", BaseURL: server.URL, Users: 2, Mode: "arrival", ArrivalRate: 1,
		Duration: "10s", WindowDuration: "2s", MaxRPS: 10, Timeout: "2s", P95LimitMS: 1000, MaxErrorRate: .1,
		MinSamplesPerStep: 1, Steps: []config.Step{{Name: "read", Method: http.MethodGet, Path: "/"}}}, config.Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join("..", "insights", "testdata", "reports", "schema-v25.json")
	phaseRun, err := insights.ReadRun(runPath)
	if err != nil {
		t.Fatal(err)
	}
	phaseRun.SchemaVersion = report.CurrentSchemaVersion
	phaseRun.GeneratorIdentity = report.GeneratorIdentity{OS: "linux", Architecture: "amd64", GoVersion: "go1.24.0", RunnerIdentityMethod: "git_commit", RunnerIdentity: strings.Repeat("a", 40)}
	phaseRun.StatisticalMethods = report.CurrentStatisticalMethods()
	phaseRun.Config.Profiles = nil
	phaseRun.Config.ProfileSelection = ""
	phaseRun.Profiles = nil
	var checkpoint arrivalcampaign.Result
	phaseOrdinal := 0
	_, err = arrivalcampaign.WithRunnerProgress(context.Background(), base, arrivalcampaign.Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2},
		func(_ context.Context, phaseBase config.Validated) report.Result {
			phaseOrdinal++
			run := phaseRun
			run.Name, run.Target, run.Outcome = phaseBase.Scenario.Name, phaseBase.Base.Scheme+"://"+phaseBase.Base.Host, "PASS"
			run.StartedAt = time.Now().UTC().Add(time.Duration(phaseOrdinal) * time.Millisecond).Format(time.RFC3339Nano)
			run.Config.Mode, run.Config.ArrivalRate, run.Config.Users = "arrival", phaseBase.Scenario.ArrivalRate, phaseBase.Scenario.Users
			run.Config.Duration, run.Config.WindowDuration = phaseBase.Scenario.Duration, phaseBase.Window.String()
			run.Config.WorkloadFingerprint, run.Config.MaxRPS = phaseBase.WorkloadFingerprint, phaseBase.Scenario.MaxRPS
			run.Config.P95LimitMS, run.Config.MaxErrorRate = phaseBase.Scenario.P95LimitMS, phaseBase.Scenario.MaxErrorRate
			run.Config.MinSamplesPerStep, run.Config.RequestTimeout = phaseBase.Scenario.MinSamplesPerStep, phaseBase.Timeout.String()
			return run
		}, func(snapshot arrivalcampaign.Result) error {
			if snapshot.ExecutionState == "RUNNING" && snapshot.PendingTrial != nil && len(snapshot.PendingTrial.Phases) > 0 {
				checkpoint = snapshot
				return errors.New("stop after saved checkpoint")
			}
			return nil
		})
	if err == nil || checkpoint.PendingTrial == nil {
		t.Fatalf("did not capture the intended in-progress checkpoint: %+v %v", checkpoint, err)
	}
	path := filepath.Join(t.TempDir(), "arrival-campaign.json")
	if err := report.WriteAnyJSON(path, checkpoint); err != nil {
		t.Fatal(err)
	}
	handler, err := loadDashboardHandler(path)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, expected := range []string{"Open-arrival capacity", "scheduled journey starts/s", "checkpoint in progress", "observation phase", "1 scheduled journey starts/s"} {
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), expected) {
			t.Fatalf("arrival campaign dashboard omitted %q: %d %s", expected, w.Code, w.Body.String())
		}
	}
}

func TestLoadDashboardHandlerRejectsMalformedAndOversizedArtifacts(t *testing.T) {
	malformedPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(malformedPath, []byte(`{"arrival_campaign_schema_version":6,"base_scheduled_journey_starts_per_second":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDashboardHandler(malformedPath); err == nil {
		t.Fatal("malformed arrival campaign artifact was accepted")
	}

	largePath := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDashboardArtifactBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDashboardHandler(largePath); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("oversized dashboard input was not bounded: %v", err)
	}
}
