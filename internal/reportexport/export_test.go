package reportexport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
	"github.com/LucasLuchetta/Damocline/internal/arrivalexplorer"
	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestRedactedRunExportOmitsUncontrolledStringsAndPreservesEvidence(t *testing.T) {
	secret := "customer-731-secret.example.internal"
	start := "2026-04-03T12:30:00.123456789Z"
	count := int64(8)
	run := report.Result{
		SchemaVersion: 26, Name: secret, Target: "https://user:token@" + secret + "/private/route?customer=731", Outcome: "PASS",
		StartedAt: start, ElapsedSeconds: 8.5, ObservationSeconds: 8,
		Config: report.RunConfig{WorkloadRevision: secret, ApplicationRevision: secret, WorkloadFingerprint: secret,
			Environment: secret, ProfileMixReference: secret, Users: 4, Duration: "8s", RequestTimeout: "1s",
			ConnectionPolicy: "reuse", MaxRPS: 50, P95LimitMS: 250, MaxErrorRate: .01, Mode: "arrival", ArrivalRate: 8,
			WindowDuration: secret, ProfileSelection: "per_journey_start", Profiles: []report.ProfileConfig{{Name: "private-workflow", Weight: 2}}},
		Metrics:  report.MetricSet{Total: 8, Success: 7, Failures: 1, P95MS: 240, Reasons: map[string]int{secret: 1}},
		Journeys: report.MetricSet{Total: 4, Success: 3, Failures: 1, P95MS: 400},
		Steps:    []report.StepResult{{Profile: "private-workflow", Name: "private-step", Method: "GET", Path: "/private/customer-731", Metrics: report.MetricSet{Total: 8, Success: 7, Failures: 1}}},
		Profiles: []report.ProfileResult{{Name: "private-workflow", Weight: 2, SelectedStarts: 8, ActorsObserved: 4, Journeys: report.MetricSet{Total: 4, Success: 3, Failures: 1}}},
		Windows: []report.WindowResult{{StartSeconds: 0, EndSeconds: 8, Metrics: report.MetricSet{Total: 8, Success: 7, Failures: 1, P95MS: 240},
			Journeys: report.MetricSet{Total: 4, Success: 3, Failures: 1, P95MS: 400},
			Steps:    []report.WindowMetric{{Name: "private-step", Metrics: report.MetricSet{Total: 8, Success: 7, Failures: 1}}},
			Profiles: []report.WindowMetric{{Name: "private-workflow", Metrics: report.MetricSet{Total: 4, Success: 3, Failures: 1}}}}},
		ArrivalsScheduled: 8, ArrivalsAdmitted: &count, IterationsStarted: 8, IterationsCompleted: 4, ArrivalsDropped: 0,
		Warnings: []string{secret}, Note: secret,
		ApplicationMetrics: &appmetrics.Timeline{SchemaVersion: 1, Source: "https://" + secret + "/metrics", Interval: "1s", StartedAt: start,
			FinishedAt: "2026-04-03T12:30:08.100000000Z", Note: secret,
			Samples: []appmetrics.TimedSample{{FinishedAt: "2026-04-03T12:30:01.200000000Z", DurationMS: 2,
				Samples: []appmetrics.Sample{{Name: secret, Value: 731}}}}},
	}
	data, err := Marshal(FromRun(run))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, secret) || strings.Contains(text, "2026-04-03") || strings.Contains(text, "/private/") || strings.Contains(text, "731") {
		t.Fatalf("export leaked uncontrolled data: %s", text)
	}
	var exported Evidence
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Format != Format || exported.Kind != "run" || exported.Workload.Mode != "arrival" || exported.Workload.JourneyStartsPS != 8 {
		t.Fatalf("safe workload facts were not preserved: %+v", exported)
	}
	if exported.Run == nil || exported.Run.Metrics.Total != 8 || exported.Run.Metrics.P95MS != 240 || len(exported.Run.Windows) != 1 || len(exported.Run.Steps) != 1 {
		t.Fatalf("run evidence was not preserved: %+v", exported.Run)
	}
	if exported.Run.Steps[0].ID != "step_001" || exported.Run.Windows[0].Steps[0].ID != "step_001" || exported.Run.Profiles[0].ID != "workflow_001" {
		t.Fatalf("stable aliases did not preserve ledger links: %+v", exported.Run)
	}
	if exported.Run.ProfileSelection != "per_journey_start" || exported.Run.Profiles[0].SelectedStarts != 8 || exported.Run.Profiles[0].ActorsObserved != 4 {
		t.Fatalf("redacted workflow selection evidence was not preserved: %+v", exported.Run)
	}
	if exported.Run.ApplicationMetrics == nil || len(exported.Run.ApplicationMetrics.Samples) != 1 || exported.Run.ApplicationMetrics.Samples[0].MetricCount != 1 || exported.Run.ApplicationMetrics.Samples[0].ElapsedMS != 1076.543 {
		t.Fatalf("application metric timing was not safely retained: %+v", exported.Run.ApplicationMetrics)
	}
	if strings.Contains(text, "private-step") || strings.Contains(text, "private-workflow") || strings.Contains(text, "token") {
		t.Fatalf("export retained private labels or credentials: %s", text)
	}
}

func TestCampaignExportsDropFreeTextAndPreservePhaseMetrics(t *testing.T) {
	secret := "tenant-998.example.private"
	phaseRun := report.Result{SchemaVersion: report.CurrentSchemaVersion, Name: secret, Target: "https://" + secret,
		Config:  report.RunConfig{Mode: "closed", Users: 3, MaxRPS: 30, P95LimitMS: 250, MaxErrorRate: .01, ConnectionPolicy: "reuse"},
		Metrics: report.MetricSet{Total: 60, Success: 60, P95MS: 100}}
	closed := campaign.Result{SchemaVersion: 10, Scenario: secret, Target: "https://" + secret, BaseUsers: 3,
		Outcome: "PASS", Notes: []string{secret}, Trials: []campaign.Trial{{Number: 1, Outcome: "PASS", AbortReason: secret,
			Phases: []campaign.Phase{{Name: "observation", EvidenceState: "PASS_OBSERVED", Reason: secret,
				Stability: "NO_LARGE_DRIFT_DETECTED", Run: phaseRun}}}}}
	data, err := Marshal(FromClosedCampaign(closed))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "AbortReason") || strings.Contains(string(data), "reason") {
		t.Fatalf("closed campaign export retained free text: %s", data)
	}
	var exported Evidence
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Kind != "closed_loop_campaign" || exported.Workload.VirtualUsers != 3 || len(exported.Trials) != 1 || exported.Trials[0].Phases[0].Run.Metrics.Total != 60 {
		t.Fatalf("closed campaign evidence missing: %+v", exported)
	}
	closed.CalibrationRequired = true
	closed.Calibration = &calibration.ClosedLoopEnvelope{Status: "BLOCKED", Levels: []calibration.ClosedLoopAssessment{{Users: 3, Status: "BLOCKED", Reason: secret, Dimensions: []calibration.Dimension{{Name: "workload_model", Status: "OUTSIDE_REFERENCE", Reason: secret}}}}}
	closed.CalibrationSource = "C:\\private\\calibration.json"
	data, err = Marshal(FromClosedCampaign(closed))
	if err != nil || strings.Contains(string(data), "private") || strings.Contains(string(data), secret) {
		t.Fatalf("calibration export leaked its local path or reason: err=%v export=%s", err, data)
	}
	var calibrated Evidence
	if err := json.Unmarshal(data, &calibrated); err != nil || calibrated.Calibration == nil || !calibrated.Calibration.Required || calibrated.Calibration.Status != "BLOCKED" {
		t.Fatalf("calibration aggregate was not retained: err=%v evidence=%+v", err, calibrated.Calibration)
	}

	arrival := arrivalcampaign.Result{SchemaVersion: 6, Scenario: secret, Target: "https://" + secret, WorkerLimit: 20,
		BaseStartsPerSecond: 7, RequestRateCeiling: 50, Outcome: "INCONCLUSIVE",
		Trials: []arrivalcampaign.Trial{{Number: 1, Outcome: "IN_PROGRESS", Phases: []arrivalcampaign.Phase{{Name: "spike", EvidenceState: "INSUFFICIENT_EVIDENCE", Reason: secret, Run: phaseRun}}}}}
	data, err = Marshal(FromArrivalCampaign(arrival))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "Reason") || strings.Contains(string(data), "target") {
		t.Fatalf("arrival export retained private campaign details: %s", data)
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Kind != "open_arrival_campaign" || exported.Workload.JourneyStartsPS != 7 || exported.Workload.WorkerLimit != 20 {
		t.Fatalf("open-arrival units were not preserved separately: %+v", exported.Workload)
	}
}

func TestExplorerExportsPreserveLoadUnitsAndDropPrivateText(t *testing.T) {
	secret := "private-company-route-tenant-88"
	closedRun := report.Result{Outcome: "PASS", ElapsedSeconds: 10, ObservationSeconds: 10,
		Config:  report.RunConfig{Mode: "closed", Users: 3, Duration: "10s", MaxRPS: 30, P95LimitMS: 250, MaxErrorRate: .01},
		Metrics: report.MetricSet{Total: 300, Success: 300, P95MS: 75}}
	passed := 3
	closed := explorer.Result{SchemaVersion: 2, Scenario: secret, Target: "https://" + secret, StartedAt: "2026-04-03T12:30:00Z",
		StageDuration: "10s", Repeats: 2, MinUsers: 2, MaxUsers: 10, HighestPassingUsers: &passed, Outcome: "PASS",
		Reason: secret, Notes: []string{secret}, Probes: []explorer.Probe{{Users: 3, Status: "PASS", Reason: secret, Runs: []report.Result{closedRun}}}}
	data, err := Marshal(FromClosedExplorer(closed))
	if err != nil {
		t.Fatal(err)
	}
	var exported Evidence
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || exported.Kind != "closed_loop_exploration" || exported.Workload.LoadUnit != "virtual_users" ||
		exported.Workload.HighestPassingLoad == nil || *exported.Workload.HighestPassingLoad != 3 || len(exported.Probes) != 1 || exported.Probes[0].Runs[0].Metrics.Total != 300 {
		t.Fatalf("closed exploration export lost safe evidence or retained private text: %+v %s", exported, data)
	}

	arrivalRun := report.Result{Outcome: "PASS", ElapsedSeconds: 10, ObservationSeconds: 10,
		Config:  report.RunConfig{Mode: "arrival", Users: 15, ArrivalRate: 4, Duration: "10s", MaxRPS: 20, P95LimitMS: 250, MaxErrorRate: .01},
		Metrics: report.MetricSet{Total: 40, Success: 40, P95MS: 80}}
	arrival := arrivalexplorer.Result{SchemaVersion: 2, Scenario: secret, Target: "https://" + secret, StartedAt: "2026-04-03T12:30:00Z",
		StageDuration: "10s", Repeats: 2, WorkerLimit: 15, MinJourneysPerSecond: 2, MaxJourneysPerSecond: 10,
		RequestedMinJourneysPerSecond: 1, RequestedMaxJourneysPerSecond: 20, CalibratedProbeRates: []int{2, 4, 10},
		TargetJourneysPerSecond: 4, TargetTested: true, Outcome: "PASS", Reason: secret, Notes: []string{secret},
		Probes: []arrivalexplorer.Probe{{JourneysPerSecond: 4, Status: "PASS", Reason: secret, Runs: []report.Result{arrivalRun}}}}
	data, err = Marshal(FromArrivalExplorer(arrival))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || exported.Kind != "open_arrival_exploration" || exported.Workload.LoadUnit != "scheduled_journey_starts_per_second" ||
		exported.Workload.RequestedMinimum != 1 || exported.Workload.RequestedMaximum != 20 || len(exported.Workload.CalibratedLoads) != 3 ||
		exported.Workload.JourneyStartsPS != 0 || exported.Workload.WorkerLimit != 15 || exported.Workload.TargetLoad != 4 || !exported.Workload.TargetTested || len(exported.Probes) != 1 || exported.Probes[0].TestedLoad != 4 {
		t.Fatalf("arrival exploration export mixed load units or retained private text: %+v %s", exported.Workload, data)
	}
}

func TestSafeEnumFallbacksCannotCopyArbitraryText(t *testing.T) {
	for _, value := range []string{"private-path", "customer-123", "https://secret.example", "\u001b[31mPASS"} {
		if safeOutcome(value) != "UNKNOWN" || safePhase(value) != "phase" || safeMethod(value) != "OTHER" {
			t.Fatalf("unrecognized value escaped enum fallback: %q", value)
		}
	}
}

func FuzzRedactedRunExportDoesNotCopyCallerStrings(f *testing.F) {
	f.Add("customer-731-secret.example.internal/private/path")
	f.Add("token=secret-abc-123")
	f.Add("\u001b[31mforged\nfield")
	f.Fuzz(func(t *testing.T, secret string) {
		if secret == "" || len(secret) > 2048 {
			t.Skip()
		}
		marker := "private-marker-" + secret
		run := report.Result{SchemaVersion: 25, Name: marker, Target: "https://" + marker,
			StartedAt: "2026-04-03T12:30:00Z", Outcome: "PASS", ElapsedSeconds: 2, ObservationSeconds: 2,
			Config: report.RunConfig{Users: 1, Mode: "closed", Duration: marker, WindowDuration: marker,
				WorkloadRevision: marker, ApplicationRevision: marker, ProfileMixReference: marker,
				Profiles: []report.ProfileConfig{{Name: marker, Weight: 1}}},
			Metrics:  report.MetricSet{Total: 1, Success: 1, Reasons: map[string]int{marker: 1}},
			Steps:    []report.StepResult{{Profile: marker, Name: marker, Path: "/" + marker, Method: "GET"}},
			Warnings: []string{marker}, Note: marker,
			ApplicationMetrics: &appmetrics.Timeline{SchemaVersion: 1, Source: "https://" + marker, Interval: "1s", StartedAt: "2026-04-03T12:30:00Z",
				FinishedAt: "2026-04-03T12:30:02Z", Note: marker,
				Samples: []appmetrics.TimedSample{{FinishedAt: "2026-04-03T12:30:01Z", Samples: []appmetrics.Sample{{Name: marker, Value: 1}}}}},
		}
		data, err := Marshal(FromRun(run))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), marker) {
			t.Fatalf("caller-controlled value escaped allowlist export: %q", marker)
		}
	})
}
