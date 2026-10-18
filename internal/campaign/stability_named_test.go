package campaign

import (
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestStabilityDetectsMinorityWorkflowDriftHiddenByAggregate(t *testing.T) {
	run := report.Result{ObservationSeconds: 10}
	for i := 0; i < 4; i++ {
		minority := 100.0
		if i == 2 {
			minority = 160 // A transient middle-window regression, not an aggregate SLO failure.
		}
		run.Windows = append(run.Windows, report.WindowResult{
			StartSeconds: float64(i) * 2.5,
			EndSeconds: float64(i+1) * 2.5,
			Metrics: report.MetricSet{Total: 100, Success: 100, P95MS: 100},
			Steps: []report.WindowMetric{
				{Name: "dashboard", Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100}},
				{Name: "review", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: minority}},
			},
			Profiles: []report.WindowMetric{
				{Name: "general", Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100}},
				{Name: "critical", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: minority}},
			},
		})
	}
	if got := AssessStability(run); got != "NON_STATIONARY" {
		t.Fatalf("minority regression hidden by aggregate: %s", got)
	}
	run.Windows[2].Steps[1].Metrics.P95MS = 100
	run.Windows[2].Profiles[1].Metrics.P95MS = 100
	if got := AssessStability(run); got != "NO_LARGE_DRIFT_DETECTED" {
		t.Fatalf("stable named windows rejected: %s", got)
	}
}

func TestStabilityCannotCertifyMissingOrLowSampleMinorityWorkflow(t *testing.T) {
	run := report.Result{ObservationSeconds: 10}
	for i := 0; i < 4; i++ {
		run.Windows = append(run.Windows, report.WindowResult{
			StartSeconds: float64(i) * 2.5,
			EndSeconds: float64(i+1) * 2.5,
			Metrics: report.MetricSet{Total: 100, Success: 100, P95MS: 100},
			Profiles: []report.WindowMetric{
				{Name: "main", Metrics: report.MetricSet{Total: 80, Success: 80, P95MS: 100}},
				{Name: "minority", Metrics: report.MetricSet{Total: 20, Success: 20, P95MS: 100}},
			},
		})
	}
	run.Windows[1].Profiles[1].Metrics.Total = 5
	run.Windows[1].Profiles[1].Metrics.Success = 5
	if got := AssessStability(run); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("low-sample workflow incorrectly accepted: %s", got)
	}
	run.Windows[1].Profiles[1].Metrics.Total = 20
	run.Windows[1].Profiles[1].Metrics.Success = 20
	run.Windows[3].Profiles[1].Name = "unknown"
	if got := AssessStability(run); got != "INSUFFICIENT_WINDOWS" {
		t.Fatalf("changed workflow identity incorrectly accepted: %s", got)
	}
}
