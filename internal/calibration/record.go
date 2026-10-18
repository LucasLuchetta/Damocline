package calibration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"time"
)

// The preflight record is embedded in the existing calibration JSON. It
// identifies the tested loopback fixture, never a customer target or session.
const SchemaVersion = 2
const minimumSupportedSchemaVersion = 1

// Healthy response latency bounds are the fixed coarse guardrail used by the
// local reference fixture. They screen observed request percentiles; they are
// not a statistical confidence interval or an application SLO.
const (
	HealthyLatencyMinMS = 15.0
	HealthyLatencyMaxMS = 150.0
)

type Generator struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	LogicalCPUs   int    `json:"logical_cpus"`
	GOMAXPROCS    int    `json:"gomaxprocs"`
	GoVersion     string `json:"go_version"`
	RunnerVersion string `json:"runner_version"`
}

type Fixture struct {
	NetworkMode          string `json:"network_mode"`
	WorkloadModel        string `json:"workload_model"`
	Method               string `json:"method"`
	HealthyResponseBytes int    `json:"healthy_response_bytes"`
	HealthyDelay         string `json:"healthy_delay"`
	StageDuration        string `json:"stage_duration"`
	SoakDuration         string `json:"soak_duration"`
	StageUsers           int    `json:"stage_users"`
	MaximumRPS           int    `json:"maximum_rps"`
	Repeats              int    `json:"repeats"`
	Extended             bool   `json:"extended"`
	HealthyRates         []int  `json:"healthy_rates"`
}

type Stage struct {
	ArrivalRate int    `json:"arrival_rate"`
	Scheduled   int64  `json:"scheduled"`
	Started     int64  `json:"started"`
	Completed   int64  `json:"completed"`
	Dropped     int64  `json:"dropped"`
	Outcome     string `json:"outcome"`
}

type Record struct {
	SchemaVersion                   int       `json:"schema_version"`
	Kind                            string    `json:"kind"`
	Status                          string    `json:"status"`
	StartedAt                       string    `json:"started_at"`
	FinishedAt                      string    `json:"finished_at"`
	Generator                       Generator `json:"generator"`
	Fixture                         Fixture   `json:"fixture"`
	ScenarioFingerprintSHA256       string    `json:"scenario_fingerprint_sha256"`
	FixtureWorkloadFingerprint      string    `json:"fixture_workload_fingerprint,omitempty"`
	HealthyStages                   []Stage   `json:"healthy_stages"`
	HighestPassingTestedArrivalRate int       `json:"highest_passing_tested_arrival_rate"`
	Interpretation                  string    `json:"interpretation"`
}

const Interpretation = "Passing fixture stages show only observed delivery in the recorded loopback envelope, not a maximum generator rate or application capacity."

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var workloadFingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,120}$`)

// SourceRunnerVersion records either the VCS source revision or the module
// build version. It never embeds working-tree paths or local build settings.
func SourceRunnerVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "source-development"
	}
	for _, item := range info.Settings {
		if item.Key == "vcs.revision" && regexp.MustCompile(`^[0-9a-fA-F]{40}$`).MatchString(item.Value) {
			return "source-" + item.Value[:12]
		}
	}
	if versionPattern.MatchString(info.Main.Version) && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "source-development"
}

func Fingerprint(fixture Fixture) (string, error) {
	data, err := json.Marshal(fixture)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// NewRecord accepts only fixed, program-owned metadata. It does not
// read target URLs, authentication headers, process environment or client files.
func NewRecord(start, end time.Time, generator Generator, fixture Fixture, stages []Stage, status string) (Record, error) {
	fingerprint, err := Fingerprint(fixture)
	if err != nil {
		return Record{}, err
	}
	workloadFingerprint, err := ReferenceWorkloadFingerprint()
	if err != nil {
		return Record{}, fmt.Errorf("construct reference workload fingerprint: %w", err)
	}
	rec := Record{
		SchemaVersion:              SchemaVersion,
		Kind:                       "separate_process_loopback_fixture",
		Status:                     status,
		StartedAt:                  start.UTC().Format(time.RFC3339Nano),
		FinishedAt:                 end.UTC().Format(time.RFC3339Nano),
		Generator:                  generator,
		Fixture:                    fixture,
		ScenarioFingerprintSHA256:  fingerprint,
		FixtureWorkloadFingerprint: workloadFingerprint,
		HealthyStages:              append([]Stage(nil), stages...),
		Interpretation:             Interpretation,
	}
	if status == "REFERENCE_SMOKE_PASSED" {
		for _, s := range stages {
			if s.Outcome != "PASS" || s.Dropped != 0 || s.Started != s.Scheduled || s.Completed != s.Scheduled {
				return Record{}, errors.New("passing calibration has an incomplete healthy stage")
			}
			if s.ArrivalRate > rec.HighestPassingTestedArrivalRate {
				rec.HighestPassingTestedArrivalRate = s.ArrivalRate
			}
		}
	}
	if err := ValidateRecord(rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// ValidateRecord is also the schema/compatibility check for locally
// archived metadata. Future incompatible versions must be rejected, not guessed.
func ValidateRecord(rec Record) error {
	if rec.SchemaVersion < minimumSupportedSchemaVersion || rec.SchemaVersion > SchemaVersion {
		return fmt.Errorf("unsupported preflight schema version %d", rec.SchemaVersion)
	}
	if rec.Kind != "separate_process_loopback_fixture" ||
		(rec.Status != "REFERENCE_SMOKE_PASSED" && rec.Status != "CHECKS_FAILED") {
		return errors.New("invalid preflight type or status")
	}
	start, err := time.Parse(time.RFC3339Nano, rec.StartedAt)
	if err != nil {
		return fmt.Errorf("invalid preflight start: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, rec.FinishedAt)
	if err != nil || end.Before(start) {
		return errors.New("preflight end precedes start or is invalid")
	}
	g := rec.Generator
	if !versionPattern.MatchString(g.OS) || !versionPattern.MatchString(g.Arch) ||
		!versionPattern.MatchString(g.GoVersion) || !versionPattern.MatchString(g.RunnerVersion) ||
		g.LogicalCPUs < 1 || g.GOMAXPROCS < 1 {
		return errors.New("invalid or unsafe generator metadata")
	}
	f := rec.Fixture
	if f.NetworkMode != "loopback_same_host_separate_process" ||
		f.WorkloadModel != "open_loop_journey_arrivals" || f.Method != "GET" ||
		f.HealthyResponseBytes != 4096 || f.HealthyDelay != "20ms" ||
		f.StageDuration != "5s" || f.SoakDuration != "30s" ||
		f.StageUsers != 10 || f.MaximumRPS != 100 ||
		f.Repeats < 1 || f.Repeats > 3 ||
		(f.Extended && f.Repeats > 2) ||
		len(f.HealthyRates) != len(rec.HealthyStages) ||
		len(f.HealthyRates) != f.Repeats*(3+func() int {
			if f.Extended {
				return 4
			}
			return 0
		}()) {
		return errors.New("invalid or unsupported loopback fixture parameters")
	}
	expected := []int{2, 5, 10}
	if f.Extended {
		expected = append(expected, 15, 20, 50, 100)
	}
	for i, stage := range rec.HealthyStages {
		rate := expected[i%len(expected)]
		if f.HealthyRates[i] != rate || stage.ArrivalRate != rate ||
			stage.Scheduled != int64(rate)*5 || stage.Started < 0 || stage.Started > stage.Scheduled ||
			stage.Completed < 0 || stage.Completed > stage.Started || stage.Dropped < 0 ||
			stage.Dropped != stage.Scheduled-stage.Started ||
			(stage.Outcome != "PASS" && stage.Outcome != "FAIL" && stage.Outcome != "INCONCLUSIVE") {
			return errors.New("invalid healthy-stage observation")
		}
	}
	fingerprint, err := Fingerprint(f)
	if err != nil || !fingerprintPattern.MatchString(rec.ScenarioFingerprintSHA256) ||
		fingerprint != rec.ScenarioFingerprintSHA256 {
		return errors.New("preflight scenario fingerprint mismatch")
	}
	if rec.Interpretation != Interpretation {
		return errors.New("preflight evidence limitations are missing")
	}
	if rec.SchemaVersion >= 2 {
		if !workloadFingerprintPattern.MatchString(rec.FixtureWorkloadFingerprint) {
			return errors.New("preflight fixture workload fingerprint is invalid")
		}
		workloadFingerprint, err := ReferenceWorkloadFingerprint()
		if err != nil || workloadFingerprint != rec.FixtureWorkloadFingerprint {
			return errors.New("preflight fixture workload fingerprint mismatch")
		}
	}
	if rec.Status == "CHECKS_FAILED" && rec.HighestPassingTestedArrivalRate != 0 {
		return errors.New("failed preflight cannot report a fully passing tested envelope")
	}
	if rec.Status == "REFERENCE_SMOKE_PASSED" {
		maximum := 0
		for _, s := range rec.HealthyStages {
			if s.Outcome != "PASS" || s.Dropped != 0 || s.Started != s.Scheduled || s.Completed != s.Scheduled {
				return errors.New("passing preflight cannot contain incomplete stages")
			}
			if s.ArrivalRate > maximum {
				maximum = s.ArrivalRate
			}
		}
		if rec.HighestPassingTestedArrivalRate != maximum {
			return errors.New("incorrect highest passing tested arrival rate")
		}
	}
	return nil
}
