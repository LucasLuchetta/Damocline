package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/campaignprofile"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

type campaignProfileVerification struct {
	SchemaVersion      int      `json:"schema_version"`
	State              string   `json:"state"`
	Profile            string   `json:"profile"`
	ProfileFingerprint string   `json:"profile_fingerprint"`
	CampaignEvidenceID string   `json:"campaign_evidence_id"`
	ComparedPhases     int      `json:"compared_phases"`
	Mismatches         []string `json:"mismatches"`
	KnownLimitations   []string `json:"known_limitations"`
}

func campaignProfileVerifyCmd(args []string, store campaignprofile.Store, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("campaign profile verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "saved profile name")
	campaignPath := fs.String("campaign", "", "saved campaign report")
	out := fs.String("out", "", "write machine-readable verification JSON without overwriting")
	if fs.Parse(args) != nil || *name == "" || *campaignPath == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: damocline campaign profile verify --name NAME --campaign FILE [--out FILE]")
		return 2
	}
	profile, _, err := store.Load(*name)
	if err != nil {
		fmt.Fprintf(stderr, "profile load error: %v\n", err)
		return 2
	}
	evidence, err := capacityplan.ReadCampaign(*campaignPath)
	if err != nil {
		fmt.Fprintf(stderr, "campaign read error: %v\n", err)
		return 2
	}
	verification, err := verifyCampaignProfile(profile, evidence)
	if err != nil {
		fmt.Fprintf(stderr, "profile verification error: %v\n", err)
		return 2
	}
	if *out != "" {
		if err := writeProfileVerification(*out, verification); err != nil {
			fmt.Fprintf(stderr, "verification output error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Verification JSON: %s\n", *out)
	}
	fmt.Fprintf(stdout, "Campaign profile verification: %s\n", verification.State)
	fmt.Fprintf(stdout, "Profile: %s | profile fingerprint: %s\n", verification.Profile, verification.ProfileFingerprint)
	fmt.Fprintf(stdout, "Campaign evidence: %s | compared phases: %d\n", verification.CampaignEvidenceID, verification.ComparedPhases)
	for _, mismatch := range verification.Mismatches {
		fmt.Fprintf(stdout, "  - %s\n", mismatch)
	}
	for _, limitation := range verification.KnownLimitations {
		fmt.Fprintf(stdout, "  - Limitation: %s\n", limitation)
	}
	fmt.Fprintln(stdout, "No traffic was generated; profile and campaign files were read locally.")
	if verification.State == "MATCHED" {
		return 0
	}
	return 1
}

func verifyCampaignProfile(profile campaignprofile.Profile, evidence campaign.Result) (campaignProfileVerification, error) {
	if err := campaignprofile.Validate(profile); err != nil {
		return campaignProfileVerification{}, fmt.Errorf("invalid saved profile: %w", err)
	}
	if err := capacityplan.ValidateSavedCampaign(evidence); err != nil {
		return campaignProfileVerification{}, fmt.Errorf("invalid saved campaign evidence: %w", err)
	}
	result := campaignProfileVerification{
		SchemaVersion: 1, State: "MATCHED", Profile: profile.Name,
		ProfileFingerprint: digestProfile(profile), CampaignEvidenceID: digestCampaign(evidence),
		Mismatches: []string{}, KnownLimitations: []string{
			"A matching declaration does not authenticate who produced or preserved either file.",
			"Credential values, account identity, target data state, and application-metrics endpoint settings are not stored in the profile and are not verified.",
			"Environment-backed body contents are excluded from workload fingerprints; only their configured length is included.",
			"The environment and application revision are self-declared; profile matching does not attest the physical host or target identity.",
		},
	}
	options, err := profile.CampaignOptions()
	if err != nil {
		return campaignProfileVerification{}, fmt.Errorf("invalid profile campaign controls: %w", err)
	}
	if options.Stability == (campaign.StabilityPolicy{}) {
		options.Stability = campaign.DefaultStabilityPolicy()
	}
	// This parse validates declarations and resolves their declared workload
	// fingerprint from the current environment. It does not create a client or
	// send network traffic. The three opt-ins only allow validation of profiles
	// that would require them during an actual run.
	validated, err := config.Parse(profile.Scenario, config.Options{Authorized: true, AllowWrites: true, AllowInsecureHTTP: true})
	if err != nil {
		result.State = "UNVERIFIABLE"
		result.Mismatches = append(result.Mismatches, "profile scenario could not be reconstructed from the current environment")
		return result, nil
	}
	addMismatch := func(field string) {
		result.Mismatches = append(result.Mismatches, "campaign does not match saved profile field: "+field)
	}
	if evidence.SchemaVersion != campaign.CurrentSchemaVersion {
		addMismatch("campaign schema version")
	}
	if evidence.Scenario != profile.Scenario.Name {
		addMismatch("scenario name")
	}
	if evidence.BaseUsers != profile.Scenario.Users {
		addMismatch("base users")
	}
	if evidence.Target != validated.Base.Scheme+"://"+validated.Base.Host {
		addMismatch("target origin")
	}
	if evidence.DeclaredEnvironment != profile.Scenario.Environment {
		addMismatch("declared environment")
	}
	expectedSoak := ""
	if options.Soak > 0 {
		expectedSoak = options.Soak.String()
	}
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"warm-up duration", evidence.Warmup, options.Warmup.String()},
		{"stabilization duration", evidence.Stabilization, options.Stabilization.String()},
		{"observation duration", evidence.Observation, options.Observation.String()},
		{"spike users", evidence.SpikeUsers, options.SpikeUsers},
		{"spike duration", evidence.SpikeDuration, options.SpikeDuration.String()},
		{"recovery duration", evidence.Recovery, options.Recovery.String()},
		{"soak duration", evidence.Soak, expectedSoak},
		{"cooldown duration", evidence.Cooldown, options.Cooldown.String()},
		{"repeat count", evidence.Repeats, options.Repeats},
		{"wall-time budget", evidence.Budget, options.Budget.String()},
		{"long-run mode", evidence.LongRun, options.LongRun},
		{"stability policy", evidence.EffectiveStabilityPolicy(), options.Stability},
		{"calibration-required policy", evidence.CalibrationRequired, options.RequireCalibration},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			addMismatch(check.field)
		}
	}
	wantGuardrails := options.Guardrails
	if wantGuardrails.Enabled() && wantGuardrails.ConsecutiveSamples == 0 {
		wantGuardrails.ConsecutiveSamples = 2
	}
	gotGuardrails := generatorhealth.Guardrails{}
	if evidence.GeneratorGuardrails != nil {
		gotGuardrails = *evidence.GeneratorGuardrails
	}
	if wantGuardrails.Enabled() != (evidence.GeneratorGuardrails != nil) || gotGuardrails != wantGuardrails {
		addMismatch("generator guardrails")
	}
	if options.RequireCalibration && evidence.Calibration == nil {
		addMismatch("required calibration evidence")
	}
	for _, trial := range evidence.Trials {
		planned := campaign.PlannedTrialPhases(options, profile.Scenario.Users, trial.Number)
		if len(trial.Phases) > len(planned) {
			addMismatch(fmt.Sprintf("trial %d phase count", trial.Number))
			continue
		}
		for phaseIndex, phase := range trial.Phases {
			want := planned[phaseIndex]
			if phase.Name != want.Name || phase.Run.Target != evidence.Target {
				addMismatch(fmt.Sprintf("trial %d phase %d schedule", trial.Number, phaseIndex+1))
				continue
			}
			phaseConfig := validated
			phaseConfig.Duration = want.Duration
			phaseConfig.Scenario.Duration = want.Duration.String()
			phaseConfig.Scenario.Users = want.Users
			if phase.Name != "warmup" && phaseConfig.Window == 0 && want.Duration >= 4*time.Second {
				phaseConfig.Window = min(30*time.Second, want.Duration/4)
				phaseConfig.Scenario.WindowDuration = phaseConfig.Window.String()
			}
			if !reflect.DeepEqual(engine.RunConfigFor(phaseConfig), phase.Run.Config) {
				addMismatch(fmt.Sprintf("trial %d phase %d workload and SLO controls", trial.Number, phaseIndex+1))
				continue
			}
			result.ComparedPhases++
		}
	}
	if result.State != "UNVERIFIABLE" && len(result.Mismatches) > 0 {
		result.State = "MISMATCHED"
	}
	if result.State == "MATCHED" && result.ComparedPhases == 0 {
		result.State = "UNVERIFIABLE"
		result.Mismatches = append(result.Mismatches, "campaign contains no phase with a comparable run configuration")
	}
	return result, nil
}

func digestProfile(profile campaignprofile.Profile) string {
	data, _ := json.Marshal(struct {
		SchemaVersion int                     `json:"schema_version"`
		Scenario      config.Scenario         `json:"scenario"`
		Options       campaignprofile.Options `json:"options"`
	}{profile.SchemaVersion, profile.Scenario, profile.Options})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestCampaign(evidence campaign.Result) string {
	data, _ := json.Marshal(evidence)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeProfileVerification(path string, verification campaignProfileVerification) error {
	data, err := json.MarshalIndent(verification, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output already exists: %s", path)
		}
		return err
	}
	removeOnError := true
	defer func() {
		_ = file.Close()
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	removeOnError = false
	return nil
}
