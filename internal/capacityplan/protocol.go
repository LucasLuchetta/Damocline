package capacityplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
)

// CapacityProtocol records decision rules before observations are collected.
// Its digest detects edits; it is not a cryptographic signature.
type CapacityProtocol struct {
	SchemaVersion                 int      `json:"schema_version"`
	MaxRPS                        int      `json:"max_rps,omitempty"`
	RequestTimeout                string   `json:"request_timeout,omitempty"`
	ProtocolID                    string   `json:"protocol_id,omitempty"`
	SealedAt                      string   `json:"sealed_at,omitempty"`
	Scenario                      string   `json:"scenario"`
	Target                        string   `json:"target"`
	Environment                   string   `json:"environment"`
	WorkloadRevision              string   `json:"workload_revision"`
	ApplicationRevision           string   `json:"application_revision"`
	WorkloadFingerprint           string   `json:"workload_fingerprint"`
	CandidateUsers                int      `json:"candidate_users"`
	FirstFailingUsers             int      `json:"first_failing_users,omitempty"`
	RequiredConfirmationCampaigns int      `json:"required_confirmation_campaigns"`
	Warmup                        string   `json:"warmup"`
	MinimumObservation            string   `json:"minimum_observation"`
	SpikeUsers                    int      `json:"spike_users,omitempty"`
	SpikeDuration                 string   `json:"spike_duration"`
	Recovery                      string   `json:"recovery"`
	RequireApplicationMetrics     bool     `json:"require_application_metrics"`
	RequireHostSensor             bool     `json:"require_host_sensor"`
	MaxP95MS                      float64  `json:"max_p95_ms"`
	MaxErrorRate                  float64  `json:"max_error_rate"`
	ReservePercent                float64  `json:"reserve_percent"`
	PeakDemandUsers               int      `json:"peak_demand_users"`
	GrowthPercent                 float64  `json:"growth_percent"`
	Preparation                   string   `json:"preparation"`
	StopConditions                []string `json:"stop_conditions"`
}

type ConfirmationAssessment struct {
	SchemaVersion                int      `json:"schema_version"`
	ProtocolID                   string   `json:"protocol_id"`
	State                        string   `json:"state"`
	CandidateUsers               int      `json:"candidate_users"`
	ConfirmedPassingCampaigns    int      `json:"confirmed_passing_campaigns"`
	ObservedFailingCampaigns     int      `json:"observed_failing_campaigns"`
	ConditionalPlanningUsers     *int     `json:"conditional_planning_users,omitempty"`
	RecommendedOperatingUsers    *int     `json:"recommended_operating_users,omitempty"`
	DeclaredPeakDemandUsers      int      `json:"declared_peak_demand_users"`
	GrowthPercent                float64  `json:"growth_percent"`
	ReservePercent               float64  `json:"reserve_percent"`
	DemandUsers                  int      `json:"projected_demand_users"`
	ConditionalDemandMarginUsers *int     `json:"conditional_planning_margin_users,omitempty"`
	OperationalDemandMarginUsers *int     `json:"operational_demand_margin_users,omitempty"`
	P95MinMS                     float64  `json:"observed_p95_min_ms"`
	P95MaxMS                     float64  `json:"observed_p95_max_ms"`
	ErrorRateMin                 float64  `json:"observed_error_rate_min"`
	ErrorRateMax                 float64  `json:"observed_error_rate_max"`
	ObservationRangeBasis        string   `json:"combined_observation_range_basis"`
	CandidateObservationCount    int      `json:"candidate_observation_samples"`
	CandidateP95MinMS            float64  `json:"candidate_observation_p95_min_ms"`
	CandidateP95MaxMS            float64  `json:"candidate_observation_p95_max_ms"`
	CandidateErrorRateMin        float64  `json:"candidate_observation_error_rate_min"`
	CandidateErrorRateMax        float64  `json:"candidate_observation_error_rate_max"`
	FailureObservationCount      int      `json:"failure_observation_samples"`
	FailureP95MinMS              float64  `json:"failure_observation_p95_min_ms"`
	FailureP95MaxMS              float64  `json:"failure_observation_p95_max_ms"`
	FailureErrorRateMin          float64  `json:"failure_observation_error_rate_min"`
	FailureErrorRateMax          float64  `json:"failure_observation_error_rate_max"`
	EvidenceIDs                  []string `json:"evidence_ids"`
	Findings                     []string `json:"findings"`
	Limitations                  []string `json:"limitations"`
}

func SealProtocol(p CapacityProtocol, at time.Time) (CapacityProtocol, error) {
	p.SchemaVersion = 2
	p.SealedAt = at.UTC().Format(time.RFC3339Nano)
	p.ProtocolID = ""
	if err := ValidateProtocol(p); err != nil {
		return CapacityProtocol{}, err
	}
	digest, err := protocolDigest(p)
	if err != nil {
		return CapacityProtocol{}, err
	}
	p.ProtocolID = "sha256:" + digest
	return p, nil
}

func ValidateProtocol(p CapacityProtocol) error {
	if (p.SchemaVersion != 1 && p.SchemaVersion != 2) || strings.TrimSpace(p.Scenario) == "" || strings.TrimSpace(p.Environment) == "" || strings.TrimSpace(p.WorkloadRevision) == "" || strings.TrimSpace(p.ApplicationRevision) == "" || !validSHA256(p.WorkloadFingerprint) || !validTarget(p.Target) {
		return errors.New("protocol requires a supported schema and declared target, scenario, environment, revisions, and workload fingerprint")
	}
	if p.SchemaVersion >= 2 {
		timeout, err := time.ParseDuration(p.RequestTimeout)
		if p.MaxRPS < 1 || p.MaxRPS > 100 || err != nil || timeout < time.Millisecond || timeout > 30*time.Second {
			return errors.New("protocol generator controls require max_rps 1-100 and request_timeout 1ms-30s")
		}
	}
	if p.CandidateUsers < 1 || p.CandidateUsers > 50 || p.FirstFailingUsers != 0 && (p.FirstFailingUsers <= p.CandidateUsers || p.FirstFailingUsers > 50) {
		return errors.New("candidate must be 1-50 VUs and first failing level, when set, must exceed candidate")
	}
	if p.RequiredConfirmationCampaigns < 2 || p.RequiredConfirmationCampaigns > 5 {
		return errors.New("required confirmation campaigns must be 2-5")
	}
	d, err := time.ParseDuration(p.MinimumObservation)
	if err != nil || d < 10*time.Second || d > 7*time.Minute {
		return errors.New("minimum observation must be 10s-7m")
	}
	for name, value := range map[string]string{"warmup": p.Warmup, "recovery": p.Recovery, "spike duration": p.SpikeDuration} {
		parsed, parseErr := time.ParseDuration(value)
		if parseErr != nil || parsed < 0 || parsed > 3*time.Minute {
			return fmt.Errorf("%s must be between 0 and 3m", name)
		}
	}
	spikeDuration, _ := time.ParseDuration(p.SpikeDuration)
	if (p.SpikeUsers == 0) != (spikeDuration == 0) || p.SpikeUsers < 0 || p.SpikeUsers > 50 || p.SpikeUsers > 0 && p.SpikeUsers <= p.CandidateUsers {
		return errors.New("spike must be absent or exceed candidate with a bounded duration")
	}
	warmup, _ := time.ParseDuration(p.Warmup)
	recovery, _ := time.ParseDuration(p.Recovery)
	phases := 1
	if warmup > 0 {
		phases++
	}
	if recovery > 0 {
		phases++
	}
	if spikeDuration > 0 {
		phases++
	}
	if 2*(warmup+d+recovery+spikeDuration+time.Duration(phases)*30*time.Second) > 15*time.Minute {
		return errors.New("protocol schedule cannot fit the bounded two-trial campaign budget")
	}
	if !finite(p.MaxP95MS) || p.MaxP95MS <= 0 || !finite(p.MaxErrorRate) || p.MaxErrorRate < 0 || p.MaxErrorRate >= 1 || !finite(p.ReservePercent) || p.ReservePercent < 0 || p.ReservePercent >= 100 || p.PeakDemandUsers < 1 || p.GrowthPercent < 0 || p.GrowthPercent > 1000 || !finite(p.GrowthPercent) {
		return errors.New("protocol SLO, reserve, or demand values are outside supported bounds")
	}
	if strings.TrimSpace(p.Preparation) == "" || len(p.Preparation) > 2048 || len(p.StopConditions) == 0 || len(p.StopConditions) > 16 {
		return errors.New("protocol requires bounded preparation and explicit stop conditions")
	}
	for _, stop := range p.StopConditions {
		if strings.TrimSpace(stop) == "" || len(stop) > 512 {
			return errors.New("stop conditions must be nonempty and bounded")
		}
	}
	if p.SealedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.SealedAt); err != nil {
			return errors.New("invalid protocol seal time")
		}
	}
	return nil
}

func VerifyProtocol(p CapacityProtocol) error {
	id := p.ProtocolID
	if !strings.HasPrefix(id, "sha256:") {
		return errors.New("protocol is not sealed")
	}
	if p.SealedAt == "" {
		return errors.New("sealed protocol timestamp is missing")
	}
	if err := ValidateProtocol(p); err != nil {
		return err
	}
	p.ProtocolID = ""
	digest, err := protocolDigest(p)
	if err != nil {
		return err
	}
	if id != "sha256:"+digest {
		return errors.New("protocol digest does not match its contents")
	}
	return nil
}

func protocolDigest(p CapacityProtocol) (string, error) {
	p.ProtocolID = ""
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func AssessConfirmations(protocol CapacityProtocol, exploration []campaign.Result, confirmations, failures []campaign.Result) (ConfirmationAssessment, error) {
	if err := VerifyProtocol(protocol); err != nil {
		return ConfirmationAssessment{}, err
	}
	if len(exploration) == 0 {
		return ConfirmationAssessment{}, errors.New("at least one separate exploratory campaign is required")
	}
	if len(confirmations) != protocol.RequiredConfirmationCampaigns {
		return ConfirmationAssessment{}, fmt.Errorf("protocol requires exactly %d separate confirmation campaigns", protocol.RequiredConfirmationCampaigns)
	}
	if protocol.FirstFailingUsers > 0 && len(failures) != protocol.RequiredConfirmationCampaigns {
		return ConfirmationAssessment{}, fmt.Errorf("protocol requires exactly %d separate failure-confirmation campaigns", protocol.RequiredConfirmationCampaigns)
	}
	if protocol.FirstFailingUsers == 0 && len(failures) != 0 {
		return ConfirmationAssessment{}, errors.New("failure confirmation campaigns supplied but no failing level was predeclared")
	}
	if len(exploration)+len(confirmations)+len(failures) > MaxCampaignCollectionSize {
		return ConfirmationAssessment{}, fmt.Errorf("protocol assessment is limited to %d campaign artifacts", MaxCampaignCollectionSize)
	}
	sealedAt, _ := time.Parse(time.RFC3339Nano, protocol.SealedAt)
	out := ConfirmationAssessment{SchemaVersion: 1, ProtocolID: protocol.ProtocolID, State: "INSUFFICIENT_EVIDENCE", CandidateUsers: protocol.CandidateUsers, DeclaredPeakDemandUsers: protocol.PeakDemandUsers, GrowthPercent: protocol.GrowthPercent, ReservePercent: protocol.ReservePercent, DemandUsers: int(math.Ceil(float64(protocol.PeakDemandUsers) * (1 + protocol.GrowthPercent/100))), ObservationRangeBasis: "combined candidate and predeclared failure levels; inspect level-specific ranges", EvidenceIDs: []string{}, Findings: []string{}, Limitations: []string{"Observed level-specific ranges are descriptive, not dependence-aware confidence intervals.", "No operational recommendation is issued unless measurement quality and workload-specific generator qualification are established."}}
	if protocol.FirstFailingUsers > 0 {
		out.Limitations = append(out.Limitations, "A replicated breach at the predeclared failure level does not establish the lowest failing workload level; interpret it alongside the separately reported exploration campaigns.")
	}
	seen := map[string]bool{}
	findingSeen := map[string]bool{}
	addFinding := func(finding string) {
		if !findingSeen[finding] {
			out.Findings = append(out.Findings, finding)
			findingSeen[finding] = true
		}
	}
	allPass, allFail := true, true
	measurementQualified := true
	passCount, failCount := 0, 0
	combinedObservationSeen, candidateObservationSeen, failureObservationSeen := false, false, false
	allCampaigns := append(append(append([]campaign.Result{}, exploration...), confirmations...), failures...)
	for idx, c := range allCampaigns {
		if c.Scenario != protocol.Scenario || c.BaseUsers < 1 || c.StartedAt == "" || len(c.Trials) == 0 || c.Repeats < len(c.Trials) || c.Repeats > 5 {
			return ConfirmationAssessment{}, errors.New("campaign identity or start time is missing")
		}
		started, err := time.Parse(time.RFC3339, c.StartedAt)
		if err != nil || !started.After(sealedAt) {
			return ConfirmationAssessment{}, errors.New("protocol must be sealed before every campaign starts")
		}
		id := evidenceDigest(c)
		if seen[id] {
			return ConfirmationAssessment{}, errors.New("campaign evidence is duplicated; confirmations must be separate campaigns")
		}
		seen[id] = true
		out.EvidenceIDs = append(out.EvidenceIDs, id)
		isPassingConfirmation := idx >= len(exploration) && idx < len(exploration)+len(confirmations)
		isFailureConfirmation := idx >= len(exploration)+len(confirmations)
		if isPassingConfirmation && c.BaseUsers != protocol.CandidateUsers {
			return ConfirmationAssessment{}, errors.New("confirmation campaign does not use the exact candidate level")
		}
		if isFailureConfirmation && c.BaseUsers != protocol.FirstFailingUsers {
			return ConfirmationAssessment{}, errors.New("failure confirmation campaign does not use the predeclared failing level")
		}
		if err := matchProtocolCampaign(protocol, c); err != nil {
			return ConfirmationAssessment{}, err
		}
		assessment, err := BuildAssessment(c, nil)
		if err != nil {
			return ConfirmationAssessment{}, err
		}
		if assessment.Audit.State == "INVALID_EVIDENCE" {
			addFinding(fmt.Sprintf("Campaign evidence %s failed offline integrity checks.", id))
			allPass = false
			if isFailureConfirmation {
				allFail = false
			}
			continue
		}
		completeConfirmation := len(c.Trials) >= 2 && c.Repeats == len(c.Trials)
		observedFirstFailure := isFailureConfirmation && c.Outcome == "FAIL" && len(c.Trials) == 1 && c.Trials[0].Outcome == "FAIL" && assessment.Audit.ObservedOutcome == "FAIL" && assessment.Audit.RequiredPhases > 0
		if isPassingConfirmation || isFailureConfirmation {
			if !completeConfirmation && !(isFailureConfirmation && observedFirstFailure) {
				addFinding(fmt.Sprintf("Campaign evidence %s was safely interrupted before all planned trials completed; it is observed evidence, not a confirmation.", id))
				if isPassingConfirmation {
					allPass = false
				} else {
					allFail = false
				}
			}
			if completeConfirmation && isPassingConfirmation {
				if assessment.Audit.State == "OBSERVED_PASS_ONLY" {
					passCount++
				} else {
					allPass = false
				}
			}
			if observedFirstFailure || completeConfirmation && isFailureConfirmation {
				observedAtLevel := false
				for _, level := range assessment.Audit.FailingLevels {
					observedAtLevel = observedAtLevel || level == protocol.FirstFailingUsers
				}
				if observedFirstFailure && assessment.Audit.ObservedOutcome == "FAIL" && observedAtLevel || completeConfirmation && assessment.Audit.State == "OBSERVED_FAIL" {
					failCount++
					if observedFirstFailure && !completeConfirmation {
						addFinding(fmt.Sprintf("Failure campaign %s stopped after its first valid SLO breach; counted as an independent observed failure, not a completed multi-trial confirmation.", id))
					}
				} else {
					allFail = false
				}
			}
			if assessment.Audit.MeasurementQuality != "VALID_WITHIN_ENVELOPE" {
				measurementQualified = false
				addFinding("Confirmation measurement quality is not VALID_WITHIN_ENVELOPE.")
			}
			for _, trial := range c.Trials {
				for _, phase := range trial.Phases {
					if phase.Name == "observation" {
						if d, err := time.ParseDuration(phase.Run.Config.Duration); err != nil || d < mustDuration(protocol.MinimumObservation) {
							return ConfirmationAssessment{}, errors.New("confirmation observation is shorter than protocol minimum")
						}
						// The protocol SLO is defined on complete journeys. HTTP request
						// percentiles remain a separate diagnostic metric.
						p95, errorRate := phase.Run.Journeys.P95MS, phase.Run.ErrorsRate
						if !combinedObservationSeen || p95 < out.P95MinMS {
							out.P95MinMS = p95
						}
						if !combinedObservationSeen || p95 > out.P95MaxMS {
							out.P95MaxMS = p95
						}
						if !combinedObservationSeen || errorRate < out.ErrorRateMin {
							out.ErrorRateMin = errorRate
						}
						if !combinedObservationSeen || errorRate > out.ErrorRateMax {
							out.ErrorRateMax = errorRate
						}
						combinedObservationSeen = true
						if isPassingConfirmation {
							if !candidateObservationSeen || p95 < out.CandidateP95MinMS {
								out.CandidateP95MinMS = p95
							}
							if !candidateObservationSeen || p95 > out.CandidateP95MaxMS {
								out.CandidateP95MaxMS = p95
							}
							if !candidateObservationSeen || errorRate < out.CandidateErrorRateMin {
								out.CandidateErrorRateMin = errorRate
							}
							if !candidateObservationSeen || errorRate > out.CandidateErrorRateMax {
								out.CandidateErrorRateMax = errorRate
							}
							candidateObservationSeen = true
							out.CandidateObservationCount++
						} else if isFailureConfirmation {
							if !failureObservationSeen || p95 < out.FailureP95MinMS {
								out.FailureP95MinMS = p95
							}
							if !failureObservationSeen || p95 > out.FailureP95MaxMS {
								out.FailureP95MaxMS = p95
							}
							if !failureObservationSeen || errorRate < out.FailureErrorRateMin {
								out.FailureErrorRateMin = errorRate
							}
							if !failureObservationSeen || errorRate > out.FailureErrorRateMax {
								out.FailureErrorRateMax = errorRate
							}
							failureObservationSeen = true
							out.FailureObservationCount++
						}
					}
				}
			}
		}
	}
	out.ConfirmedPassingCampaigns = passCount
	out.ObservedFailingCampaigns = failCount
	if allPass {
		planningUsers := int(math.Floor(float64(protocol.CandidateUsers) * (1 - protocol.ReservePercent/100)))
		marginUsers := planningUsers - out.DemandUsers
		out.ConditionalPlanningUsers = &planningUsers
		out.ConditionalDemandMarginUsers = &marginUsers
	}
	if allPass {
		addFinding("Candidate passed all separately executed confirmation campaigns under the declared configuration.")
	}
	if protocol.FirstFailingUsers > 0 && allFail {
		addFinding("The predeclared failure level produced a valid observed breach in every separate failure campaign; each execution stopped after its first breach.")
	}
	if !allPass || protocol.FirstFailingUsers > 0 && !allFail {
		if !allPass {
			addFinding("Candidate confirmation outcomes vary or are incomplete; capacity decision remains inconclusive.")
		} else {
			addFinding("The predeclared failing level lacks a valid observed breach in at least one separate failure campaign; capacity decision remains inconclusive.")
		}
	}
	if allPass && measurementQualified {
		out.State = "OBSERVED_CONFIRMATIONS_PASS"
	}
	if out.ConditionalPlanningUsers != nil && out.DemandUsers > *out.ConditionalPlanningUsers {
		addFinding("Projected demand exceeds the reserve-adjusted conditional planning level; no extrapolation is made.")
	}
	if out.RecommendedOperatingUsers != nil {
		margin := *out.RecommendedOperatingUsers - out.DemandUsers
		out.OperationalDemandMarginUsers = &margin
	}
	return out, nil
}

func matchProtocolCampaign(p CapacityProtocol, c campaign.Result) error {
	minObs := mustDuration(p.MinimumObservation)
	if c.SchemaVersion < 3 || c.Repeats < 2 {
		return errors.New("campaign schema or repetitions do not satisfy protocol")
	}
	if c.Target != p.Target {
		return errors.New("campaign target differs from sealed protocol")
	}
	if p.SchemaVersion >= 2 {
		for _, trial := range c.Trials {
			for _, phase := range trial.Phases {
				r := phase.Run.Config
				if r.Mode != "closed" || r.MaxRPS != p.MaxRPS || r.RequestTimeout != p.RequestTimeout {
					return errors.New("campaign generator controls differ from sealed protocol")
				}
			}
		}
	}
	if c.Warmup != p.Warmup || c.Recovery != p.Recovery || c.SpikeUsers != p.SpikeUsers || c.SpikeDuration != p.SpikeDuration {
		return errors.New("campaign phase schedule differs from sealed protocol")
	}
	if p.RequireHostSensor && !c.HostSensorConfigured {
		return errors.New("protocol requires host sensor but campaign has none")
	}
	for _, trial := range c.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			r := phase.Run
			_, metricsState, metricsErr := phaseApplicationTimeline(phase)
			if metricsErr != nil {
				return fmt.Errorf("application metrics evidence is invalid: %w", metricsErr)
			}
			if p.RequireApplicationMetrics && metricsState != "COLLECTED" {
				return fmt.Errorf("protocol requires complete application metrics in every required phase; %s phase coverage is %s", phase.Name, metricsState)
			}
			if p.RequireHostSensor && (phase.HostTelemetry == nil || phase.HostTelemetry.Coverage != "COMPLETE") {
				return errors.New("protocol requires complete host sensor evidence in every required phase")
			}
			if r.Config.Environment != p.Environment || r.Config.WorkloadRevision != p.WorkloadRevision || r.Config.ApplicationRevision != p.ApplicationRevision || r.Config.WorkloadFingerprint != p.WorkloadFingerprint || r.Config.P95LimitMS != p.MaxP95MS || r.Config.MaxErrorRate != p.MaxErrorRate {
				return errors.New("campaign workload, environment, revision, or SLO differs from sealed protocol")
			}
			if phase.Name == "observation" {
				d, err := time.ParseDuration(r.Config.Duration)
				if err != nil || d < minObs {
					return errors.New("campaign observation duration is shorter than protocol minimum")
				}
			}
		}
	}
	return nil
}

func phaseApplicationTimeline(phase campaign.Phase) (*appmetrics.Timeline, string, error) {
	if phase.Telemetry != nil && phase.Run.ApplicationMetrics != nil {
		return nil, "INVALID", errors.New("phase contains both legacy and run-level application metrics")
	}
	if phase.Telemetry != nil {
		state, err := appmetrics.ReassessCoverage(*phase.Telemetry)
		if err != nil {
			return nil, "INVALID", err
		}
		if state == "COMPLETE" {
			state = "COLLECTED"
		} else {
			state = "PARTIAL"
		}
		return appmetrics.TimelineFromObservation(*phase.Telemetry), state, nil
	}
	if phase.Run.ApplicationMetrics == nil {
		return nil, "MISSING", nil
	}
	if err := appmetrics.ValidateTimeline(*phase.Run.ApplicationMetrics); err != nil {
		return nil, "INVALID", err
	}
	return phase.Run.ApplicationMetrics, phase.Run.ApplicationMetrics.State(), nil
}

func validSHA256(s string) bool {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "sha256:"))
	return strings.HasPrefix(s, "sha256:") && err == nil && len(b) == sha256.Size
}

func validTarget(target string) bool {
	u, err := url.Parse(target)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}
func finite(v float64) bool               { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func mustDuration(s string) time.Duration { d, _ := time.ParseDuration(s); return d }
