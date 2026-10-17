// Package capacityplan compares a business demand target with campaign evidence.
package capacityplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const maxCampaignReportBytes = 16 << 20

type Result struct {
	State       string   `json:"state"`
	TargetUsers int      `json:"target_users"`
	TestedUsers int      `json:"tested_users"`
	Findings    []string `json:"findings"`
}

func Evaluate(evidence campaign.Result, goal capacitygoal.Goal) (Result, error) {
	target, err := capacitygoal.EvaluatedUsers(goal)
	if err != nil {
		return Result{}, err
	}
	result := Result{State: "INSUFFICIENT_EVIDENCE", TargetUsers: target, TestedUsers: evidence.BaseUsers, Findings: []string{
		"The target is a business demand calculation, not a prediction of server capacity.",
		"A result applies only to the recorded workload, environment, SLOs, observation windows, and generator evidence.",
	}}
	if err := validateEvidence(evidence); err != nil {
		result.Findings = append(result.Findings, "Campaign evidence is incomplete or malformed: "+err.Error())
		return result, nil
	}
	if evidence.Outcome != "PASS" {
		result.Findings = append(result.Findings, fmt.Sprintf("Campaign outcome is %s, so it cannot support a sustained-demand conclusion.", evidence.Outcome))
		return result, nil
	}
	if evidence.BaseUsers != target {
		result.Findings = append(result.Findings, fmt.Sprintf("The passing base level (%d VUs) differs from the required target (%d VUs). Test the exact target before deciding on capacity.", evidence.BaseUsers, target))
		return result, nil
	}
	if blocker := qualityBlocker(evidence); blocker != "" {
		result.Findings = append(result.Findings, blocker)
		return result, nil
	}
	result.State = "GOAL_MET_UNDER_TESTED_CONDITIONS"
	result.Findings = append(result.Findings, fmt.Sprintf("The repeated campaign passed at %d base VUs, meeting the calculated target of %d VUs under the recorded conditions.", evidence.BaseUsers, target))
	if evidence.ObservationVariability != nil {
		result.Findings = append(result.Findings, fmt.Sprintf("Observed observation p95 ranged from %.0f to %.0f ms across %d trials; review the full report before changing infrastructure.", evidence.ObservationVariability.P95MinMS, evidence.ObservationVariability.P95MaxMS, evidence.ObservationVariability.Runs))
	}
	return result, nil
}

func qualityBlocker(evidence campaign.Result) string {
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name == "warmup" {
				continue
			}
			if phase.Run.Quality == nil {
				return "A required campaign phase is missing explicit data-quality evidence; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
			switch phase.Run.Quality.State {
			case report.QualityValid:
				continue
			case report.QualityInvalid:
				return "A required campaign phase has INVALID measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			case report.QualityLimited:
				return "A required campaign phase has LIMITED measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			default:
				return "A required campaign phase has unknown measurement quality; the tested demand remains INSUFFICIENT_EVIDENCE."
			}
		}
	}
	return ""
}

// ReadCampaign reads a bounded saved campaign report. It never makes a network
// request and accepts only the report schema emitted by the local campaign command.
func ReadCampaign(path string) (campaign.Result, error) {
	file, err := os.Open(path)
	if err != nil {
		return campaign.Result{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCampaignReportBytes+1))
	if err != nil {
		return campaign.Result{}, err
	}
	if len(data) > maxCampaignReportBytes {
		return campaign.Result{}, errors.New("campaign report exceeds the 16 MiB safety limit")
	}
	var result campaign.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return campaign.Result{}, err
	}
	return result, nil
}

func validateEvidence(evidence campaign.Result) error {
	if evidence.SchemaVersion != 1 || evidence.Scenario == "" || evidence.BaseUsers < 1 || evidence.Repeats < 2 || evidence.Repeats > 5 || len(evidence.Trials) != evidence.Repeats {
		return errors.New("expected a complete schema-v1 repeated campaign")
	}
	if evidence.Outcome != "PASS" {
		return nil
	}
	for _, trial := range evidence.Trials {
		if trial.Outcome != "PASS" {
			return errors.New("a passing campaign contains a non-passing trial")
		}
		foundObservation, foundRecovery := false, false
		for _, phase := range trial.Phases {
			switch phase.Name {
			case "observation":
				foundObservation = true
			case "recovery":
				foundRecovery = true
			}
			if phase.Name != "warmup" && phase.Run.Outcome != "PASS" {
				return errors.New("a required campaign phase did not pass")
			}
		}
		if !foundObservation || !foundRecovery {
			return errors.New("a trial is missing observation or recovery evidence")
		}
	}
	return nil
}
