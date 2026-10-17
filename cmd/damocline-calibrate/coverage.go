package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

// Coverage checks are strictly offline. They compare only the portions of a
// user-supplied scenario that the fixed reference fixture actually measured.
// A passing loopback smoke check does NOT establish client workload capacity.
const (
	coverageSchemaVersion = 1
	coverageInconclusive = "INCONCLUSIVE"
	coverageMatched = "MATCHED"
	coverageOutside = "OUTSIDE_REFERENCE"
	coverageUnknown = "UNVERIFIED"
	maxCalibrationBytes = 16 << 20
)

var errInconclusive = errors.New("preflight evidence is inconclusive; no application capacity can be certified")

type coverageDimension struct {
	Name string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type coverageAssessment struct {
	SchemaVersion int `json:"schema_version"`
	Status string `json:"status"`
	ReferenceStatus string `json:"reference_status"`
	Shape string `json:"request_shape"`
	TestedArrivalRates []int `json:"tested_arrival_rates,omitempty"`
	Dimensions []coverageDimension `json:"dimensions"`
	Note string `json:"note"`
}

const coverageCaveat = "REFERENCE_SMOKE_PASSED is local fixture evidence only. Neither configuration resemblance nor a rate below a tested maximum certifies generator headroom, a different payload/transport, or application capacity."

func newCoverageAssessment() coverageAssessment {
	return coverageAssessment{SchemaVersion: coverageSchemaVersion, Status: coverageInconclusive,
		ReferenceStatus: "UNVERIFIED", Shape: "NOT_ESTABLISHED", Note: coverageCaveat}
}

func (r *coverageAssessment) add(name, status, reason string) {
	r.Dimensions = append(r.Dimensions, coverageDimension{Name: name, Status: status, Reason: reason})
}

// loadCalibrationForCoverage reads a bounded local artifact. It intentionally
// omits arbitrary strings from errors and never contacts the recorded target.
func loadCalibrationForCoverage(path string) (assessment, error) {
	var result assessment
	f, err := os.Open(path)
	if err != nil {
		return result, errors.New("local calibration file is unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCalibrationBytes+1))
	if err != nil || len(data) > maxCalibrationBytes {
		return result, errors.New("local calibration file exceeds the read limit or cannot be read")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, errors.New("local calibration file is not valid JSON")
	}
	if result.SchemaVersion != 1 || result.Status != result.Preflight.Status ||
		validatePreflightRecord(result.Preflight) != nil {
		return result, errors.New("local calibration has an unsupported, inconsistent, or invalid preflight record")
	}
	if len(result.Runs) != len(result.Preflight.HealthyStages) {
		return result, errors.New("local calibration stage evidence is incomplete")
	}
	for i, stage := range result.Preflight.HealthyStages {
		run := result.Runs[i]
		if run.ArrivalsScheduled != stage.Scheduled || run.IterationsStarted != stage.Started ||
			run.IterationsCompleted != stage.Completed || run.ArrivalsDropped != stage.Dropped ||
			run.Outcome != stage.Outcome || int64(stage.ArrivalRate)*5 != stage.Scheduled {
			return result, errors.New("local calibration stage observations disagree with the recorded run")
		}
	}
	if result.Status == "REFERENCE_SMOKE_PASSED" && len(result.Checks) > 0 {
		return result, errors.New("local calibration claims PASS but contains discrepancies")
	}
	return result, nil
}

func evaluatedReferenceRates(a assessment) []int {
	if a.Status != "REFERENCE_SMOKE_PASSED" || len(a.Checks) != 0 {
		return nil
	}
	unique := make(map[int]bool)
	for _, stage := range a.Preflight.HealthyStages {
		if stage.Outcome != "PASS" || stage.Dropped != 0 || stage.Started != stage.Scheduled ||
			stage.Completed != stage.Scheduled {
			return nil
		}
		unique[stage.ArrivalRate] = true
	}
	var rates []int
	for r := range unique { rates = append(rates, r) }
	sort.Ints(rates)
	return rates
}

func containsRate(rates []int, rate int) bool {
	for _, r := range rates { if r == rate { return true } }
	return false
}

// compareScenarioToReference inspects declarative inputs only, not credentials,
// target contents, DNS, connections, or customer services. Its overall status
// is conservatively INCONCLUSIVE until other evidence gates are implemented.
func compareScenarioToReference(s config.Scenario, a assessment, calibrationErr error) coverageAssessment {
	r := newCoverageAssessment()
	rates := evaluatedReferenceRates(a)
	if calibrationErr != nil {
		// Never reuse rate evidence from an artifact rejected for inconsistency.
		rates = nil
		r.add("calibration", coverageUnknown, "A compatible and internally consistent reference artifact was not available.")
	} else if a.Status != "REFERENCE_SMOKE_PASSED" || len(a.Checks) != 0 {
		r.ReferenceStatus = "CHECKS_FAILED"
		r.add("calibration", coverageOutside, "The local reference checks did not all pass.")
	} else {
		r.ReferenceStatus = "REFERENCE_SMOKE_PASSED"
		r.TestedArrivalRates = rates
		r.add("calibration", coverageMatched, "Recorded reference stages passed; this is not a client-workload certification.")
	}

	mode := s.Mode
	if mode == "" { mode = "closed" }
	if mode != "arrival" {
		r.add("workload_model", coverageOutside, "Only independent open-loop journey arrivals were tested; closed-loop VUs were not covered by this fixture.")
	} else {
		r.add("workload_model", coverageMatched, "The configured journey-arrival model matches the fixture.")
	}

	if len(rates) == 0 {
		r.add("arrival_rate", coverageUnknown, "No complete passing reference rate is available for comparison.")
	} else if mode != "arrival" || !containsRate(rates, s.ArrivalRate) {
		r.add("arrival_rate", coverageOutside, "The requested journey arrival rate was not tested exactly; do not interpolate between recorded rates.")
	} else {
		r.add("arrival_rate", coverageMatched, "The requested journey arrival rate is one of the exact locally tested rates.")
	}

	if calibrationErr != nil {
		r.add("actor_budget", coverageUnknown, "The local worker budget could not be verified from a valid reference artifact.")
	} else if s.Users != a.Preflight.Fixture.StageUsers || s.Users < 1 {
		r.add("actor_budget", coverageOutside, "The requested worker/VU count differs from the fixture's ten workers.")
	} else {
		r.add("actor_budget", coverageMatched, "The actor budget matches the ten-worker reference configuration.")
	}

	maxSteps := len(s.Steps)
	if len(s.Profiles) > 0 {
		maxSteps = 0
		for _, profile := range s.Profiles {
			if len(profile.Steps) > maxSteps { maxSteps = len(profile.Steps) }
		}
	}
	if calibrationErr != nil {
		r.add("rate_ceiling", coverageUnknown, "The fixture rate ceiling could not be verified from a valid reference artifact.")
	} else if s.MaxRPS < 1 || s.MaxRPS > a.Preflight.Fixture.MaximumRPS ||
		(mode == "arrival" && (s.ArrivalRate < 1 || int64(s.ArrivalRate)*int64(maxSteps) > int64(s.MaxRPS))) {
		r.add("rate_ceiling", coverageOutside, "The request rate ceiling is invalid, outside the fixture bound, or too low for the proposed journey starts.")
	} else {
		r.add("rate_ceiling", coverageMatched, "The declared rate cap is within the fixture's safety ceiling; this does not establish delivered RPS.")
	}

	singleGET := len(s.Profiles) == 0 && len(s.Steps) == 1
	if singleGET {
		step := s.Steps[0]
		method := strings.ToUpper(step.Method)
		if method == "" { method = "GET" }
		singleGET = method == "GET" && len(step.Headers) == 0 && step.BodyEnv == "" &&
			len(step.Extract) == 0 && s.PerVUCookieEnvPrefix == "" &&
			s.SyntheticDataPrefixEnv == ""
	}
	if !singleGET {
		r.add("request_mix", coverageOutside, "The reference tested one unauthenticated GET step per journey; multiple profiles, writes, custom headers or dynamic steps are not covered.")
	} else {
		r.add("request_mix", coverageMatched, "The declared journey has the same simple GET-only request shape.")
	}

	duration, durErr := time.ParseDuration(s.Duration)
	if durErr != nil || duration <= 0 || duration > 30*time.Second {
		r.add("observation_duration", coverageOutside, "The requested duration is invalid or longer than any reference observation.")
	} else if mode != "arrival" || (duration > 5*time.Second && (s.ArrivalRate != 10 || duration > 30*time.Second)) {
		r.add("observation_duration", coverageOutside, "Only five-second stages and a 30-second stage at exactly ten arrivals/s were observed.")
	} else {
		r.add("observation_duration", coverageMatched, "The duration fits a recorded local fixture stage; this is not proof of steady-state behavior.")
	}

	timeout, timeoutErr := time.ParseDuration(s.Timeout)
	if s.Timeout == "" { timeout = 8*time.Second; timeoutErr = nil }
	if timeoutErr != nil || timeout != time.Second ||
		(s.ThinkTime != "" && s.ThinkTime != "0s") ||
		(s.RampUp != "" && s.RampUp != "0s") {
		r.add("timing_options", coverageOutside, "The reference used a one-second timeout, no ramp-up and no think time.")
	} else {
		r.add("timing_options", coverageMatched, "The declared timing options match the tested fixture.")
	}

	u, err := url.Parse(s.BaseURL)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil ||
		!net.ParseIP(u.Hostname()).Equal(net.ParseIP("127.0.0.1")) {
		r.add("transport", coverageOutside, "The fixture used unencrypted IPv4 loopback; HTTPS, DNS and remote networking were not validated.")
	} else {
		r.add("transport", coverageMatched, "The URL declares IPv4 HTTP loopback; it is not proof that the target is the controlled fixture.")
	}

	r.add("response_body_size", coverageUnknown, "The fixture returned exactly 4096 bytes; an HTTP scenario does not attest the target's actual response-size distribution.")
	r.add("connections_and_server_identity", coverageUnknown, "A scenario cannot prove connection reuse, HTTP response behavior or that its endpoint is the instrumented loopback fixture.")
	r.add("generator_equivalence", coverageUnknown, "The calibration source host, age and workload-specific generator headroom are not yet checked (G0-08).")

	outside := false
	for _, item := range r.Dimensions { if item.Status == coverageOutside { outside = true; break } }
	if !outside && calibrationErr == nil && a.Status == "REFERENCE_SMOKE_PASSED" {
		r.Shape = "DECLARED_SHAPE_RESEMBLES_REFERENCE"
	} else {
		r.Shape = "NOT_ESTABLISHED"
	}
	return r
}

// runOfflineCoverage is invoked before the fixture runner and never spawns a
// process or makes an HTTP request. The JSON result contains no scenario input.
func runOfflineCoverage(calibrationPath, scenarioPath string, w io.Writer) error {
	s, err := config.Load(scenarioPath)
	if err != nil {
		return errors.New("scenario file is unreadable or invalid")
	}
	a, readErr := loadCalibrationForCoverage(calibrationPath)
	result := compareScenarioToReference(s, a, readErr)
	result.checkProvenance(a, readErr, time.Now().UTC(), currentRuntime())
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("write offline preflight result: %w", err)
	}
	return errInconclusive
}
