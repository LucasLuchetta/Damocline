package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/campaignprofile"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestCampaignProfileRunDryRunAndLoopbackReport(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(40 * time.Millisecond)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()
	stability := campaign.DefaultStabilityPolicy()
	stability.MaxP95IncreasePercent = 100
	stability.MaxP95IncreaseMS = 100
	stability.MaxThroughputDropPercent = 100
	profile, err := campaignprofile.New("loopback", "", config.Scenario{Name: "loopback", Environment: "development", BaseURL: server.URL, Users: 2, Duration: "10s", Timeout: "1s", MaxRPS: 100, P95LimitMS: 1000, MaxErrorRate: 0.1, Steps: []config.Step{{Name: "health", Method: http.MethodGet, Path: "/"}}}, campaign.Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2, Stability: stability})
	if err != nil {
		t.Fatal(err)
	}
	store := campaignprofile.Store{Dir: filepath.Join(t.TempDir(), "profiles")}
	if err := store.Save(profile, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := campaignProfileRun([]string{"--name", "loopback", "--dry-run"}, store, &stdout, &stderr); code != 0 {
		t.Fatalf("dry run code=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("DRY RUN")) {
		t.Fatalf("dry run summary missing: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileRun([]string{"--name", "loopback"}, store, &stdout, &stderr); code != 2 {
		t.Fatalf("unauthorized run code=%d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unauthorized run sent %d request(s)", got)
	}

	outDir := t.TempDir()
	firstOut := filepath.Join(outDir, "first.json")
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileRun([]string{"--name", "loopback", "--authorized", "--out", firstOut}, store, &stdout, &stderr); code == 2 {
		t.Fatalf("loopback campaign rejected: %s", stderr.String())
	}
	firstData, err := os.ReadFile(firstOut)
	if err != nil {
		t.Fatalf("campaign report not written: %v; stderr=%s", err, stderr.String())
	}
	var first campaign.Result
	if err := json.Unmarshal(firstData, &first); err != nil {
		t.Fatalf("decode first campaign report: %v", err)
	}
	if first.SchemaVersion != campaign.CurrentSchemaVersion || len(first.Trials) != 2 || len(first.Trials[0].Phases) == 0 || first.Trials[0].Phases[0].Run.Config.WorkloadFingerprint == "" || first.WorkloadSource == nil || first.WorkloadSource.Type != "campaign_profile" || first.WorkloadSource.FileSHA256 == "" {
		t.Fatalf("first report lacks reproducible campaign evidence: schema=%d trials=%+v stability=%+v", first.SchemaVersion, first.Trials, first.StabilityPolicy)
	}
	verification, err := verifyCampaignProfile(profile, first)
	if err != nil || verification.State != "MATCHED" || verification.SourceIdentityStatus != "UNVERIFIABLE" || verification.ComparedPhases != 4 {
		t.Fatalf("saved profile did not match its campaign report: state=%s phases=%d mismatches=%v err=%v", verification.State, verification.ComparedPhases, verification.Mismatches, err)
	}
	changedProfile := profile
	changedProfile.Scenario.P95LimitMS++
	changed, err := verifyCampaignProfile(changedProfile, first)
	if err != nil || changed.State != "MISMATCHED" {
		t.Fatalf("changed profile was not rejected: state=%s mismatches=%v err=%v", changed.State, changed.Mismatches, err)
	}
	verificationOut := filepath.Join(outDir, "verification.json")
	beforeVerificationRequests := requests.Load()
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileVerifyCmd([]string{"--name", "loopback", "--campaign", firstOut, "--out", verificationOut}, store, &stdout, &stderr); code != 0 {
		t.Fatalf("offline profile verification code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(verificationOut); err != nil {
		t.Fatalf("verification command did not create output: stdout=%s stderr=%s err=%v", stdout.String(), stderr.String(), err)
	}
	if got, _, _, digestErr := store.LoadWithSHA256("loopback"); digestErr != nil || got.Name != profile.Name {
		t.Fatalf("could not load hash-identified profile: %+v err=%v", got, digestErr)
	}
	if got := requests.Load(); got != beforeVerificationRequests {
		t.Fatalf("offline profile verification sent %d request(s)", got-beforeVerificationRequests)
	}
	verificationData, err := os.ReadFile(verificationOut)
	if err != nil {
		t.Fatalf("verification artifact not written: %v", err)
	}
	var savedVerification campaignProfileVerification
	if err := json.Unmarshal(verificationData, &savedVerification); err != nil || savedVerification.State != "MATCHED" || savedVerification.SourceIdentityStatus != "MATCHED" || savedVerification.CampaignEvidenceID != verification.CampaignEvidenceID {
		t.Fatalf("invalid saved verification: %+v err=%v", savedVerification, err)
	}
	changedBytes, err := os.ReadFile(store.Path("loopback"))
	if err != nil {
		t.Fatal(err)
	}
	changedBytes = append(changedBytes, ' ')
	if err := os.WriteFile(store.Path("loopback"), changedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileVerifyCmd([]string{"--name", "loopback", "--campaign", firstOut}, store, &stdout, &stderr); code != 1 || !strings.Contains(stdout.String(), "MISMATCHED") {
		t.Fatalf("changed exact profile bytes were not rejected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(store.Path("loopback"), changedBytes[:len(changedBytes)-1], 0600); err != nil {
		t.Fatal(err)
	}

	campaignEvidenceID := capacityplan.AuditCampaign(first).EvidenceID
	assessmentOut := filepath.Join(outDir, "assessment.json")
	capacityReportOut := filepath.Join(outDir, "capacity.md")
	auditArgs := []string{"audit", "--campaign", firstOut, "--expect-evidence-id", campaignEvidenceID, "--peak-users", "2", "--out", assessmentOut, "--report-out", capacityReportOut}
	beforeAuditRequests := requests.Load()
	stdout.Reset()
	stderr.Reset()
	if code := Run(auditArgs, &stdout, &stderr); code != 0 {
		t.Fatalf("offline capacity audit code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	assessmentData, err := os.ReadFile(assessmentOut)
	if err != nil {
		t.Fatalf("capacity assessment was not written: %v", err)
	}
	capacityReportData, err := os.ReadFile(capacityReportOut)
	if err != nil {
		t.Fatalf("capacity report was not written: %v", err)
	}
	if !strings.Contains(string(capacityReportData), "--expect-evidence-id '"+campaignEvidenceID+"'") {
		t.Fatalf("capacity report replay does not pin its campaign evidence: %s", capacityReportData)
	}
	if got := requests.Load(); got != beforeAuditRequests {
		t.Fatalf("offline audit generated %d target request(s)", got-beforeAuditRequests)
	}

	replayAssessmentOut := filepath.Join(outDir, "replay-assessment.json")
	replayReportOut := filepath.Join(outDir, "replay-capacity.md")
	replayArgs := []string{"audit", "--campaign", firstOut, "--expect-evidence-id", campaignEvidenceID, "--peak-users", "2", "--out", replayAssessmentOut, "--report-out", replayReportOut}
	stdout.Reset()
	stderr.Reset()
	if code := Run(replayArgs, &stdout, &stderr); code != 0 {
		t.Fatalf("reported offline replay code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	replayedAssessmentData, err := os.ReadFile(replayAssessmentOut)
	if err != nil {
		t.Fatalf("replayed assessment was not written: %v", err)
	}
	if string(replayedAssessmentData) != string(assessmentData) {
		t.Fatalf("offline replay changed the machine assessment:\nfirst: %s\nreplay: %s", assessmentData, replayedAssessmentData)
	}
	replayedReportData, err := os.ReadFile(replayReportOut)
	if err != nil {
		t.Fatalf("replayed capacity report was not written: %v", err)
	}
	if string(replayedReportData) != string(capacityReportData) {
		t.Fatalf("offline replay changed the Markdown capacity report:\nfirst: %s\nreplay: %s", capacityReportData, replayedReportData)
	}
	if got := requests.Load(); got != beforeAuditRequests {
		t.Fatalf("offline assessment replay generated %d target request(s)", got-beforeAuditRequests)
	}

	secondOut := filepath.Join(outDir, "second.json")
	stdout.Reset()
	stderr.Reset()
	if code := campaignProfileRun([]string{"--name", "loopback", "--authorized", "--out", secondOut}, store, &stdout, &stderr); code == 2 {
		t.Fatalf("second loopback campaign rejected: %s", stderr.String())
	}
	secondData, err := os.ReadFile(secondOut)
	if err != nil {
		t.Fatalf("second campaign report not written: %v; stderr=%s", err, stderr.String())
	}
	var second campaign.Result
	if err := json.Unmarshal(secondData, &second); err != nil {
		t.Fatalf("decode second campaign report: %v", err)
	}
	if second.SchemaVersion != first.SchemaVersion || second.BaseUsers != first.BaseUsers || second.Warmup != first.Warmup || second.Observation != first.Observation || second.Stabilization != first.Stabilization || second.Recovery != first.Recovery || second.Soak != first.Soak || second.Cooldown != first.Cooldown || second.Budget != first.Budget || second.Repeats != first.Repeats || second.EffectiveStabilityPolicy() != first.EffectiveStabilityPolicy() || len(second.Trials) != len(first.Trials) {
		t.Fatalf("repeated profile changed campaign controls: first=%+v second=%+v", first, second)
	}
	for trialIndex := range first.Trials {
		firstPhases, secondPhases := first.Trials[trialIndex].Phases, second.Trials[trialIndex].Phases
		if len(firstPhases) != len(secondPhases) {
			t.Fatalf("trial %d phase count changed: first=%d second=%d", trialIndex+1, len(firstPhases), len(secondPhases))
		}
		for phaseIndex := range firstPhases {
			firstRun, secondRun := firstPhases[phaseIndex].Run, secondPhases[phaseIndex].Run
			if firstPhases[phaseIndex].Name != secondPhases[phaseIndex].Name || firstRun.Config.WorkloadFingerprint != secondRun.Config.WorkloadFingerprint || firstRun.Config.Users != secondRun.Config.Users || firstRun.Config.Duration != secondRun.Config.Duration || firstRun.Config.Mode != secondRun.Config.Mode || firstRun.Config.RequestTimeout != secondRun.Config.RequestTimeout || firstRun.Config.ConnectionPolicy != secondRun.Config.ConnectionPolicy || firstRun.Config.MaxRPS != secondRun.Config.MaxRPS || firstRun.Config.P95LimitMS != secondRun.Config.P95LimitMS || firstRun.Config.MaxErrorRate != secondRun.Config.MaxErrorRate || firstRun.Config.Environment != secondRun.Config.Environment {
				t.Fatalf("trial %d phase %d did not reproduce saved workload and criteria", trialIndex+1, phaseIndex+1)
			}
		}
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("authorized profile executions sent no requests")
	}
}

func TestLongRunProfileDryRunNeedsNoExecutionConfirmation(t *testing.T) {
	profile, err := campaignprofile.New("staging-preview", "", config.Scenario{
		Name: "staging-preview", Environment: "staging", BaseURL: "https://app.example.com",
		Users: 2, Duration: "10s", Timeout: "1s", MaxRPS: 10, P95LimitMS: 1000,
		MaxErrorRate: 0.01, Steps: []config.Step{{Name: "health", Method: "GET", Path: "/health"}},
	}, campaign.Options{Observation: 10 * time.Second, Recovery: 10 * time.Second, Repeats: 2, LongRun: true})
	if err != nil {
		t.Fatal(err)
	}
	store := campaignprofile.Store{Dir: filepath.Join(t.TempDir(), "profiles")}
	if err := store.Save(profile, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := campaignProfileRun([]string{"--name", profile.Name, "--dry-run"}, store, &stdout, &stderr); code != 0 {
		t.Fatalf("long-run dry run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("DRY RUN")) {
		t.Fatalf("long-run dry run summary missing: %s", stdout.String())
	}
}
