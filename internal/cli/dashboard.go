package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacitygoal"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/reportui"
)

const maxDashboardArtifactBytes = 16 << 20

type dashboardArtifactShape struct {
	ArrivalCampaignVersion *int            `json:"arrival_campaign_schema_version"`
	BaseUsers              *int            `json:"base_users"`
	Metrics                json.RawMessage `json:"metrics"`
}

func loadDashboardHandler(path string) (http.Handler, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDashboardArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDashboardArtifactBytes {
		return nil, errors.New("dashboard artifact exceeds the 16 MiB safety limit")
	}
	var shape dashboardArtifactShape
	if err := json.Unmarshal(data, &shape); err != nil {
		return nil, fmt.Errorf("invalid dashboard artifact JSON: %w", err)
	}
	if shape.ArrivalCampaignVersion != nil {
		var saved arrivalcampaign.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, fmt.Errorf("invalid arrival campaign JSON: %w", err)
		}
		goal := capacitygoal.JourneyStartGoal{PeakStartsPerSecond: saved.PeakStartsPerSecond, GrowthPercent: saved.GrowthPercent, ReservePercent: saved.ReservePercent}
		if goal.PeakStartsPerSecond == 0 {
			goal = capacitygoal.JourneyStartGoal{PeakStartsPerSecond: saved.BaseStartsPerSecond}
		}
		if _, err := capacitygoal.EvaluateJourneyCampaign(saved, goal); err != nil {
			return nil, fmt.Errorf("invalid arrival campaign evidence: %w", err)
		}
		presentation := reportui.CampaignView{Kind: "Open-arrival capacity", Unit: "scheduled journey starts/s", Scenario: saved.Scenario,
			Target: saved.Target, StartedAt: saved.StartedAt, Outcome: saved.Outcome, Pending: saved.ExecutionState == "RUNNING"}
		presentation.Trials = arrivalTrials(saved)
		if err := validateCampaignRuns(presentation.Trials); err != nil {
			return nil, err
		}
		return reportui.CampaignHandler(presentation), nil
	}
	if shape.BaseUsers != nil {
		var saved campaign.Result
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, fmt.Errorf("invalid closed-loop campaign JSON: %w", err)
		}
		if err := capacityplan.ValidateSavedCampaign(saved); err != nil {
			return nil, fmt.Errorf("invalid closed-loop campaign evidence: %w", err)
		}
		presentation := reportui.CampaignView{Kind: "Closed-loop capacity", Unit: "virtual users (VUs)", Scenario: saved.Scenario,
			Target: saved.Target, StartedAt: saved.StartedAt, Outcome: saved.Outcome, Pending: saved.ExecutionState == "RUNNING"}
		presentation.Trials = closedLoopTrials(saved)
		if err := validateCampaignRuns(presentation.Trials); err != nil {
			return nil, err
		}
		return reportui.CampaignHandler(presentation), nil
	}
	if len(shape.Metrics) == 0 || string(shape.Metrics) == "null" {
		return nil, errors.New("artifact is not a run or supported campaign report")
	}
	var saved report.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("invalid run report JSON: %w", err)
	}
	if err := insights.ValidateRunReport(saved); err != nil {
		return nil, err
	}
	return reportui.Handler(saved), nil
}

func closedLoopTrials(saved campaign.Result) []reportui.TrialView {
	trials := make([]reportui.TrialView, 0, len(saved.Trials)+1)
	for _, trial := range saved.Trials {
		trials = append(trials, reportui.TrialView{Number: trial.Number, Outcome: trial.Outcome, Reason: trial.AbortReason, Phases: closedLoopPhases(trial.Phases)})
	}
	if saved.PendingTrial != nil {
		trials = append(trials, reportui.TrialView{Number: saved.PendingTrial.Number, Outcome: saved.PendingTrial.Outcome, Reason: saved.PendingTrial.AbortReason, Pending: true,
			Phases: closedLoopPhases(saved.PendingTrial.Phases)})
	}
	return trials
}

func arrivalTrials(saved arrivalcampaign.Result) []reportui.TrialView {
	trials := make([]reportui.TrialView, 0, len(saved.Trials)+1)
	for _, trial := range saved.Trials {
		trials = append(trials, reportui.TrialView{Number: trial.Number, Outcome: trial.Outcome, Reason: trial.AbortReason, Phases: arrivalPhases(trial.Phases)})
	}
	if saved.PendingTrial != nil {
		trials = append(trials, reportui.TrialView{Number: saved.PendingTrial.Number, Outcome: saved.PendingTrial.Outcome, Reason: saved.PendingTrial.AbortReason, Pending: true,
			Phases: arrivalPhases(saved.PendingTrial.Phases)})
	}
	return trials
}

func closedLoopPhases(phases []campaign.Phase) []reportui.PhaseView {
	views := make([]reportui.PhaseView, 0, len(phases))
	for _, phase := range phases {
		views = append(views, makePhaseView(phase.Name, phase.EvidenceState, phase.Reason, phase.Run, fmt.Sprintf("%d VUs", phase.Run.Config.Users)))
	}
	return views
}

func arrivalPhases(phases []arrivalcampaign.Phase) []reportui.PhaseView {
	views := make([]reportui.PhaseView, 0, len(phases))
	for _, phase := range phases {
		views = append(views, makePhaseView(phase.Name, phase.EvidenceState, phase.Reason, phase.Run,
			fmt.Sprintf("%d scheduled journey starts/s", phase.Run.Config.ArrivalRate)))
	}
	return views
}

func makePhaseView(name, state, reason string, run report.Result, load string) reportui.PhaseView {
	errorRate := "not available"
	if run.Metrics.Total > 0 {
		errorRate = fmt.Sprintf("%.2f%%", 100*float64(run.Metrics.Failures)/float64(run.Metrics.Total))
	}
	return reportui.PhaseView{Name: name, EvidenceState: state, Reason: reason, Load: load, ErrorRate: errorRate, Run: run,
		Metrics: reportui.PhaseMetricCharts(run)}
}

func validateCampaignRuns(trials []reportui.TrialView) error {
	for _, trial := range trials {
		for _, phase := range trial.Phases {
			if err := insights.ValidateRunReport(phase.Run); err != nil {
				return fmt.Errorf("invalid run report in trial %d %s phase: %w", trial.Number, phase.Name, err)
			}
		}
	}
	return nil
}
