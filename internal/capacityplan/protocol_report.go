package capacityplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

type ReproductionBundle struct {
	SchemaVersion    int                    `json:"schema_version"`
	DamoclineVersion string                 `json:"damocline_version"`
	Protocol         CapacityProtocol       `json:"protocol"`
	Assessment       ConfirmationAssessment `json:"assessment"`
	Campaigns        []CampaignArtifact     `json:"campaigns"`
	Commands         []string               `json:"reproduction_commands"`
	Manifest         ReproductionManifest   `json:"reproduction_manifest"`
	Notes            []string               `json:"notes"`
}

// ReproductionManifest restores embedded inputs beneath relative paths and
// defines a portable offline replay command.
type ReproductionManifest struct {
	ProtocolPath      string                     `json:"protocol_path"`
	Campaigns         []ReproductionCampaignFile `json:"campaign_files"`
	RestoreScript     string                     `json:"restore_script_powershell"`
	AssessmentCommand string                     `json:"assessment_command"`
	Requirements      []string                   `json:"requirements"`
}

type ReproductionCampaignFile struct {
	Role       string `json:"role"`
	Path       string `json:"path"`
	EvidenceID string `json:"evidence_id"`
}

type CampaignArtifact struct {
	Role       string          `json:"role"`
	Path       string          `json:"path"`
	EvidenceID string          `json:"evidence_id"`
	Result     campaign.Result `json:"result"`
}

// CampaignAssessmentSummary keeps the report's campaign-level summary tied to
// the same offline audits used to interpret its protocol assessment.
type CampaignAssessmentSummary struct {
	Role                         string  `json:"role"`
	EvidenceID                   string  `json:"evidence_id"`
	Scenario                     string  `json:"scenario"`
	Users                        int     `json:"users"`
	Outcome                      string  `json:"outcome"`
	TrialsPlanned                int     `json:"trials_planned"`
	TrialsCompleted              int     `json:"trials_completed"`
	AuditState                   string  `json:"audit_state"`
	MeasurementQuality           string  `json:"measurement_quality"`
	ObservationP95MinMS          float64 `json:"observation_p95_min_ms"`
	ObservationP95MaxMS          float64 `json:"observation_p95_max_ms"`
	ObservationErrorMin          float64 `json:"observation_error_rate_min"`
	ObservationErrorMax          float64 `json:"observation_error_rate_max"`
	ApplicationTelemetryComplete int     `json:"application_telemetry_complete_phases"`
	ApplicationTelemetryTotal    int     `json:"application_telemetry_total_phases"`
	HostTelemetryComplete        int     `json:"host_telemetry_complete_phases"`
	HostTelemetryTotal           int     `json:"host_telemetry_total_phases"`
	CgroupSamplesAvailable       int     `json:"cgroup_samples_available"`
	CgroupSamplesTotal           int     `json:"cgroup_samples_total"`
	ProcessRSSMinBytes           uint64  `json:"process_rss_min_bytes"`
	ProcessRSSMaxBytes           uint64  `json:"process_rss_max_bytes"`
	ProcessRSSSamples            int     `json:"process_rss_samples"`
	ProcessCPUMinPercent         float64 `json:"process_cpu_min_percent"`
	ProcessCPUMaxPercent         float64 `json:"process_cpu_max_percent"`
	ProcessCPUSamples            int     `json:"process_cpu_samples"`
	SwapUsedMinBytes             uint64  `json:"host_swap_used_min_bytes"`
	SwapUsedMaxBytes             uint64  `json:"host_swap_used_max_bytes"`
	SwapTotalMinBytes            uint64  `json:"host_swap_total_min_bytes"`
	SwapTotalMaxBytes            uint64  `json:"host_swap_total_max_bytes"`
	SwapSamples                  int     `json:"host_swap_samples"`
	DiskReadMinBytes             uint64  `json:"disk_read_min_bytes"`
	DiskReadMaxBytes             uint64  `json:"disk_read_max_bytes"`
	DiskWriteMinBytes            uint64  `json:"disk_write_min_bytes"`
	DiskWriteMaxBytes            uint64  `json:"disk_write_max_bytes"`
	DiskSamples                  int     `json:"disk_io_samples"`
	DiskWriteSamples             int     `json:"disk_write_samples"`
	NetworkReceiveMinBytes       uint64  `json:"network_receive_min_bytes"`
	NetworkReceiveMaxBytes       uint64  `json:"network_receive_max_bytes"`
	NetworkTransmitMinBytes      uint64  `json:"network_transmit_min_bytes"`
	NetworkTransmitMaxBytes      uint64  `json:"network_transmit_max_bytes"`
	NetworkSamples               int     `json:"network_io_samples"`
	NetworkTransmitSamples       int     `json:"network_transmit_samples"`
	ProcessRestartCoverage       string  `json:"process_restart_coverage"`
	ProcessRestartSamples        int     `json:"process_start_time_samples"`
	ProcessRestartSeries         int     `json:"process_start_time_series"`
	ExporterReportedRestarts     uint64  `json:"exporter_reported_process_restarts"`
}

// ProtocolReport is a machine-readable executive report with an auditable,
// bounded summary for each source campaign.
type ProtocolReport struct {
	SchemaVersion             int                         `json:"schema_version"`
	ProtocolID                string                      `json:"protocol_id"`
	State                     string                      `json:"state"`
	Assessment                ConfirmationAssessment      `json:"assessment"`
	Campaigns                 []CampaignAssessmentSummary `json:"campaigns"`
	ApplicationMetrics        []ApplicationMetricSummary  `json:"application_metrics"`
	ApplicationMetricsOmitted int                         `json:"application_metrics_omitted"`
	KeyLimits                 []string                    `json:"key_limits"`
}

const maxApplicationMetricSummaries = 256

// ApplicationMetricSummary reports observed ranges without exposing label values
// or the private exporter source. Ranges span all samples and series in a phase.
type ApplicationMetricSummary struct {
	Campaign           int      `json:"campaign"`
	Trial              int      `json:"trial"`
	Phase              string   `json:"phase"`
	Name               string   `json:"name"`
	Type               string   `json:"type,omitempty"`
	Unit               string   `json:"unit,omitempty"`
	LabelKeys          []string `json:"label_keys,omitempty"`
	Series             int      `json:"series"`
	Observations       int      `json:"observations"`
	Min                float64  `json:"min"`
	Max                float64  `json:"max"`
	CounterDeltaSeries int      `json:"counter_delta_series,omitempty"`
	CounterDeltaMin    float64  `json:"counter_delta_min,omitempty"`
	CounterDeltaMax    float64  `json:"counter_delta_max,omitempty"`
	CounterResets      int      `json:"counter_resets,omitempty"`
}

func BuildProtocolReport(p CapacityProtocol, a ConfirmationAssessment, campaigns []CampaignArtifact) ProtocolReport {
	result := ProtocolReport{SchemaVersion: 1, ProtocolID: p.ProtocolID, State: a.State, Assessment: a, Campaigns: make([]CampaignAssessmentSummary, 0, len(campaigns)), KeyLimits: append([]string{}, a.Limitations...)}
	for _, artifact := range campaigns {
		audit := AuditCampaign(artifact.Result)
		summary := CampaignAssessmentSummary{Role: artifact.Role, EvidenceID: artifact.EvidenceID, Scenario: artifact.Result.Scenario, Users: artifact.Result.BaseUsers, Outcome: artifact.Result.Outcome, TrialsPlanned: artifact.Result.Repeats, TrialsCompleted: len(artifact.Result.Trials), AuditState: audit.State, MeasurementQuality: string(audit.MeasurementQuality)}
		metricObservations := make([]*appmetrics.Timeline, 0)
		observationSeen := false
		for _, trial := range artifact.Result.Trials {
			for _, phase := range trial.Phases {
				if phase.Name == "observation" {
					if !observationSeen || phase.Run.Metrics.P95MS < summary.ObservationP95MinMS {
						summary.ObservationP95MinMS = phase.Run.Metrics.P95MS
					}
					if !observationSeen || phase.Run.Metrics.P95MS > summary.ObservationP95MaxMS {
						summary.ObservationP95MaxMS = phase.Run.Metrics.P95MS
					}
					if !observationSeen || phase.Run.ErrorsRate < summary.ObservationErrorMin {
						summary.ObservationErrorMin = phase.Run.ErrorsRate
					}
					if !observationSeen || phase.Run.ErrorsRate > summary.ObservationErrorMax {
						summary.ObservationErrorMax = phase.Run.ErrorsRate
					}
					observationSeen = true
				}
				timeline, appState, appErr := phaseApplicationTimeline(phase)
				if appErr == nil && timeline != nil {
					metricObservations = append(metricObservations, timeline)
					summary.ApplicationTelemetryTotal++
					if appState == "COLLECTED" {
						summary.ApplicationTelemetryComplete++
					}
				}
				if phase.HostTelemetry != nil {
					summary.HostTelemetryTotal++
					if phase.HostTelemetry.Coverage == "COMPLETE" {
						summary.HostTelemetryComplete++
					}
					for _, sample := range phase.HostTelemetry.Samples {
						for _, hostSample := range sample.Reading.Host.Samples {
							accumulateRange(&summary.DiskReadMinBytes, &summary.DiskReadMaxBytes, &summary.DiskSamples, hostSample.DiskReadBytes)
							accumulateRange(&summary.DiskWriteMinBytes, &summary.DiskWriteMaxBytes, &summary.DiskWriteSamples, hostSample.DiskWriteBytes)
							accumulateRange(&summary.NetworkReceiveMinBytes, &summary.NetworkReceiveMaxBytes, &summary.NetworkSamples, hostSample.NetworkRxBytes)
							accumulateRange(&summary.NetworkTransmitMinBytes, &summary.NetworkTransmitMaxBytes, &summary.NetworkTransmitSamples, hostSample.NetworkTxBytes)
							if hostSample.SwapUsedBytes != nil && hostSample.SwapTotalBytes != nil {
								if summary.SwapSamples == 0 || *hostSample.SwapUsedBytes < summary.SwapUsedMinBytes {
									summary.SwapUsedMinBytes = *hostSample.SwapUsedBytes
								}
								if summary.SwapSamples == 0 || *hostSample.SwapUsedBytes > summary.SwapUsedMaxBytes {
									summary.SwapUsedMaxBytes = *hostSample.SwapUsedBytes
								}
								if summary.SwapSamples == 0 || *hostSample.SwapTotalBytes < summary.SwapTotalMinBytes {
									summary.SwapTotalMinBytes = *hostSample.SwapTotalBytes
								}
								if summary.SwapSamples == 0 || *hostSample.SwapTotalBytes > summary.SwapTotalMaxBytes {
									summary.SwapTotalMaxBytes = *hostSample.SwapTotalBytes
								}
								summary.SwapSamples++
							}
							summary.CgroupSamplesTotal++
							if hasCgroupEvidence(hostSample.Workload) {
								summary.CgroupSamplesAvailable++
							}
							if hostSample.Workload != nil {
								if cpu := hostSample.Workload.ProcessCPUPercent; cpu != nil && hostSample.Workload.ProcessCPUCoverage == "COMPLETE" {
									if summary.ProcessCPUSamples == 0 || *cpu < summary.ProcessCPUMinPercent {
										summary.ProcessCPUMinPercent = *cpu
									}
									if summary.ProcessCPUSamples == 0 || *cpu > summary.ProcessCPUMaxPercent {
										summary.ProcessCPUMaxPercent = *cpu
									}
									summary.ProcessCPUSamples++
								}
								if rss := hostSample.Workload.ProcessRSSBytes; rss != nil {
									if summary.ProcessRSSSamples == 0 || *rss < summary.ProcessRSSMinBytes {
										summary.ProcessRSSMinBytes = *rss
									}
									if summary.ProcessRSSSamples == 0 || *rss > summary.ProcessRSSMaxBytes {
										summary.ProcessRSSMaxBytes = *rss
									}
									summary.ProcessRSSSamples++
								}
							}
						}
					}
				}
			}
		}
		restarts := appmetrics.ObserveProcessRestarts(metricObservations)
		summary.ProcessRestartCoverage = restarts.Coverage
		summary.ProcessRestartSamples = restarts.Samples
		summary.ProcessRestartSeries = restarts.Series
		summary.ExporterReportedRestarts = restarts.ObservedRestarts
		result.Campaigns = append(result.Campaigns, summary)
	}
	result.ApplicationMetrics, result.ApplicationMetricsOmitted = summarizeApplicationMetrics(campaigns)
	return result
}

func summarizeApplicationMetrics(campaigns []CampaignArtifact) ([]ApplicationMetricSummary, int) {
	rows := make([]ApplicationMetricSummary, 0)
	for ci, artifact := range campaigns {
		for _, trial := range artifact.Result.Trials {
			for _, phase := range trial.Phases {
				timeline, _, appErr := phaseApplicationTimeline(phase)
				if appErr != nil || timeline == nil {
					continue
				}
				type key struct {
					name, typ, unit string
				}
				type aggregate struct {
					row       ApplicationMetricSummary
					series    map[string]struct{}
					points    map[string][]metricPoint
					deltaSeen bool
				}
				aggs := map[key]*aggregate{}
				for _, snapshot := range timeline.Samples {
					for _, sample := range snapshot.Samples {
						k := key{sample.Name, sample.Type, sample.Unit}
						a := aggs[k]
						if a == nil {
							a = &aggregate{row: ApplicationMetricSummary{Campaign: ci + 1, Trial: trial.Number, Phase: phase.Name, Name: sample.Name, Type: sample.Type, Unit: sample.Unit, Min: sample.Value, Max: sample.Value}, series: map[string]struct{}{}, points: map[string][]metricPoint{}}
							aggs[k] = a
						}
						seriesKey := sample.SeriesID
						if seriesKey == "" {
							seriesKey = sample.Name
						}
						a.series[string(seriesKey)] = struct{}{}
						if sample.Type == "counter" {
							at, err := time.Parse(time.RFC3339Nano, snapshot.FinishedAt)
							if err == nil {
								a.points[seriesKey] = append(a.points[seriesKey], metricPoint{at: at, value: sample.Value})
							}
						}
						a.row.Observations++
						if sample.Value < a.row.Min {
							a.row.Min = sample.Value
						}
						if sample.Value > a.row.Max {
							a.row.Max = sample.Value
						}
					}
				}
				for _, a := range aggs {
					a.row.Series = len(a.series)
					for _, points := range a.points {
						sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
						if len(points) < 2 {
							continue
						}
						start, end := points[0].value, points[len(points)-1].value
						if end < start {
							a.row.CounterResets++
							continue
						}
						delta := end - start
						if !a.deltaSeen || delta < a.row.CounterDeltaMin {
							a.row.CounterDeltaMin = delta
						}
						if !a.deltaSeen || delta > a.row.CounterDeltaMax {
							a.row.CounterDeltaMax = delta
						}
						a.deltaSeen = true
						a.row.CounterDeltaSeries++
					}
					rows = append(rows, a.row)
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Campaign != b.Campaign {
			return a.Campaign < b.Campaign
		}
		if a.Trial != b.Trial {
			return a.Trial < b.Trial
		}
		if a.Phase != b.Phase {
			return a.Phase < b.Phase
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Unit != b.Unit {
			return a.Unit < b.Unit
		}
		return false
	})
	if len(rows) <= maxApplicationMetricSummaries {
		return rows, 0
	}
	return rows[:maxApplicationMetricSummaries], len(rows) - maxApplicationMetricSummaries
}

type metricPoint struct {
	at    time.Time
	value float64
}

// RenderProtocolReport emits an executive page and technical appendix from
// the same offline confirmation assessment and its saved source campaigns.
func RenderProtocolReport(p CapacityProtocol, a ConfirmationAssessment, campaigns []campaign.Result, version string, commands []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Capacity assessment: %s\n\n", md(p.Scenario))
	fmt.Fprintf(&b, "**Decision state:** `%s`  \n**Protocol:** `%s`  \n**Environment:** %s  \n**Application revision:** %s\n\n", a.State, a.ProtocolID, md(p.Environment), md(p.ApplicationRevision))
	b.WriteString("## Executive assessment\n\n")
	fmt.Fprintf(&b, "The predeclared candidate was **%d workload-equivalent virtual users**. Separate candidate campaigns recorded SLO passes: **%d**. Separate failing-level campaigns with a valid observed first breach: **%d**. Declared peak demand is **%d VUs**; after %.1f%% growth, projected demand is **%d VUs**.\n\n", a.CandidateUsers, a.ConfirmedPassingCampaigns, a.ObservedFailingCampaigns, a.DeclaredPeakDemandUsers, a.GrowthPercent, a.DemandUsers)
	if a.ConditionalPlanningUsers != nil && a.ConditionalDemandMarginUsers != nil {
		fmt.Fprintf(&b, "Applying the declared %.1f%% reserve to the candidate that passed all required campaigns gives a **conditional arithmetic planning level of %d VUs** and a conditional difference of **%s VUs** versus projected demand. This arithmetic is conditional on the recorded outcomes and does not by itself establish sustainable capacity.\n\n", a.ReservePercent, *a.ConditionalPlanningUsers, formatSignedMargin(*a.ConditionalDemandMarginUsers))
	} else {
		b.WriteString("No conditional planning level or demand margin is calculated because the candidate did not pass all required confirmation campaigns.\n\n")
	}
	if a.RecommendedOperatingUsers == nil {
		b.WriteString("No operational capacity recommendation or operational demand margin is established by this evidence.\n\n")
	} else {
		if a.OperationalDemandMarginUsers == nil {
			fmt.Fprintf(&b, "**Recommended operating level:** %d VUs; operational margin versus projected demand is unavailable.\n\n", *a.RecommendedOperatingUsers)
		} else {
			fmt.Fprintf(&b, "**Recommended operating level:** %d VUs; operational margin versus projected demand: %s VUs.\n\n", *a.RecommendedOperatingUsers, formatSignedMargin(*a.OperationalDemandMarginUsers))
		}
	}
	if p.FirstFailingUsers > 0 {
		b.WriteString("The predeclared failure level is assessed separately. Separate campaigns repeat a breach at that exact workload and stop after the first valid breach; this does not establish the lowest failing workload level. Review lower-load exploration results separately; no interpolation is made. These are not completed multi-trial campaigns or a capacity certification.\n\n")
	}
	if a.CandidateObservationCount > 0 {
		fmt.Fprintf(&b, "Candidate confirmations at **%d VUs** (%d observation samples): p95 %.2f-%.2f ms; error rate %.3f%%-%.3f%%.\n", a.CandidateUsers, a.CandidateObservationCount, a.CandidateP95MinMS, a.CandidateP95MaxMS, a.CandidateErrorRateMin*100, a.CandidateErrorRateMax*100)
	}
	if p.FirstFailingUsers > 0 && a.FailureObservationCount > 0 {
		fmt.Fprintf(&b, "Predeclared failing level at **%d VUs** (%d observed phases): p95 %.2f-%.2f ms; error rate %.3f%%-%.3f%%.\n", p.FirstFailingUsers, a.FailureObservationCount, a.FailureP95MinMS, a.FailureP95MaxMS, a.FailureErrorRateMin*100, a.FailureErrorRateMax*100)
	}
	b.WriteString("These descriptive ranges are reported separately by tested level. They do not pool different load conditions, are not confidence intervals, and do not account for temporal dependence.\n\n")
	b.WriteString("### Findings and limits\n\n")
	for _, finding := range a.Findings {
		fmt.Fprintf(&b, "- %s\n", md(finding))
	}
	for _, limitation := range a.Limitations {
		fmt.Fprintf(&b, "- Limitation: %s\n", md(limitation))
	}
	b.WriteString("\n## Technical appendix\n\n")
	fmt.Fprintf(&b, "- Protocol digest: `%s`\n- Sealed at: `%s`\n- Target origin: `%s`\n- Workload fingerprint: `%s`\n- Workload revision: %s\n- Candidate: %d VUs; predeclared failure level: %d VUs\n- Schedule: warmup %s, observation at least %s, spike %d VUs for %s, recovery %s\n- Required telemetry: application metrics=%t, Linux host sensor=%t\n- SLOs: p95 <= %.2f ms; error rate <= %.3f%%\n- Preparation: %s\n- Physical host type: NOT_RECORDED\n", a.ProtocolID, p.SealedAt, md(p.Target), p.WorkloadFingerprint, md(p.WorkloadRevision), p.CandidateUsers, p.FirstFailingUsers, p.Warmup, p.MinimumObservation, p.SpikeUsers, p.SpikeDuration, p.Recovery, p.RequireApplicationMetrics, p.RequireHostSensor, p.MaxP95MS, p.MaxErrorRate*100, md(p.Preparation))
	b.WriteString("\n### Measurement quality findings\n\n| Campaign | Evidence ID | Trial | Phase | Recomputed state | Recorded and recomputed findings |\n|---:|---|---:|---|---|---|\n")
	for campaignIndex, c := range campaigns {
		evidenceID := "unavailable"
		if campaignIndex < len(a.EvidenceIDs) {
			evidenceID = a.EvidenceIDs[campaignIndex]
		}
		for _, trial := range c.Trials {
			for _, phase := range trial.Phases {
				quality := report.AssessQuality(phase.Run)
				if quality.State == report.QualityValid && len(quality.Findings) == 0 {
					continue
				}
				findings := make([]string, 0)
				if phase.Run.Quality != nil && len(phase.Run.Quality.Findings) > 0 {
					findings = append(findings, "recorded: "+strings.Join(phase.Run.Quality.Findings, "; "))
				}
				if len(quality.Findings) > 0 {
					findings = append(findings, "recomputed: "+strings.Join(quality.Findings, "; "))
				}
				if len(findings) == 0 {
					findings = append(findings, "No specific finding.")
				}
				fmt.Fprintf(&b, "| %d | `%s` | %d | %s | %s | %s |\n", campaignIndex+1, evidenceID, trial.Number, md(phase.Name), md(quality.State), md(strings.Join(findings, " / ")))
			}
		}
	}
	b.WriteString("\n### Campaign evidence\n\nThe protocol SLO uses complete journey p95 (including configured waits); the per-phase table also reports HTTP request p95, which can be lower.\n\n| Campaign | Evidence ID | Level | Outcome | Trials | Journey p95 range | Error range | HTTP requests/s | Successful journeys/s | Generator quality | App metrics | Host sensor | Process CPU range | Exporter-reported process restarts | Process RSS range | Host swap used range | Disk I/O range | Network I/O range | Cgroup samples |\n|---:|---|---:|---|---:|---:|---:|---:|---:|---|---|---|---|---|---|---|---|---|---:|\n")
	for campaignIndex, c := range campaigns {
		evidenceID := "unavailable"
		if campaignIndex < len(a.EvidenceIDs) {
			evidenceID = a.EvidenceIDs[campaignIndex]
		}
		ioSummary := BuildProtocolReport(p, a, []CampaignArtifact{{Result: c}}).Campaigns[0]
		p95Min, p95Max, errMin, errMax := 0.0, 0.0, 0.0, 0.0
		rpsMin, rpsMax, journeyMin, journeyMax := 0.0, 0.0, 0.0, 0.0
		observationSeen := false
		appTotal, appComplete, hostTotal, hostComplete, cgroupTotal, cgroupKnown := 0, 0, 0, 0, 0, 0
		processRSSMin, processRSSMax := uint64(0), uint64(0)
		processRSSCount := 0
		swapUsedMin, swapUsedMax := uint64(0), uint64(0)
		swapSamples := 0
		qualitySet := map[string]bool{}
		for _, t := range c.Trials {
			for _, phase := range t.Phases {
				if phase.Name == "observation" {
					x, e := phase.Run.Journeys.P95MS, phase.Run.ErrorsRate
					seconds := phase.Run.WindowSeconds()
					rps, journeys := 0.0, 0.0
					if seconds > 0 {
						rps = float64(phase.Run.Metrics.Total) / seconds
						journeys = report.SuccessfulJourneysPerSecond(phase.Run)
					}
					if !observationSeen || x < p95Min {
						p95Min = x
					}
					if x > p95Max {
						p95Max = x
					}
					if !observationSeen || e < errMin {
						errMin = e
					}
					if e > errMax {
						errMax = e
					}
					if !observationSeen || rps < rpsMin {
						rpsMin = rps
					}
					if rps > rpsMax {
						rpsMax = rps
					}
					if !observationSeen || journeys < journeyMin {
						journeyMin = journeys
					}
					if journeys > journeyMax {
						journeyMax = journeys
					}
					observationSeen = true
				}
				timeline, appState, appErr := phaseApplicationTimeline(phase)
				if appErr == nil && timeline != nil {
					appTotal++
					if appState == "COLLECTED" {
						appComplete++
					}
				}
				if phase.HostTelemetry != nil {
					hostTotal++
					if phase.HostTelemetry.Coverage == "COMPLETE" {
						hostComplete++
					}
					for _, sample := range phase.HostTelemetry.Samples {
						for _, hostSample := range sample.Reading.Host.Samples {
							if hostSample.SwapUsedBytes != nil && hostSample.SwapTotalBytes != nil {
								if swapSamples == 0 || *hostSample.SwapUsedBytes < swapUsedMin {
									swapUsedMin = *hostSample.SwapUsedBytes
								}
								if swapSamples == 0 || *hostSample.SwapUsedBytes > swapUsedMax {
									swapUsedMax = *hostSample.SwapUsedBytes
								}
								swapSamples++
							}
							cgroupTotal++
							if hasCgroupEvidence(hostSample.Workload) {
								cgroupKnown++
							}
							if hostSample.Workload != nil {
								if rss := hostSample.Workload.ProcessRSSBytes; rss != nil {
									if processRSSCount == 0 || *rss < processRSSMin {
										processRSSMin = *rss
									}
									if processRSSCount == 0 || *rss > processRSSMax {
										processRSSMax = *rss
									}
									processRSSCount++
								}
							}
						}
					}
				}
				if phase.Name == "observation" && phase.Run.Quality != nil {
					qualitySet[phase.Run.Quality.State] = true
				}
			}
		}
		appState, hostState := "not configured", "not configured"
		if appTotal > 0 {
			appState = fmt.Sprintf("%s (%d/%d phases)", coverageName(appComplete, appTotal), appComplete, appTotal)
		}
		if c.HostSensorConfigured {
			hostState = fmt.Sprintf("%s (%d/%d phases)", coverageName(hostComplete, hostTotal), hostComplete, hostTotal)
		}
		quality := "unknown"
		if len(qualitySet) > 0 {
			states := make([]string, 0, len(qualitySet))
			for state := range qualitySet {
				states = append(states, state)
			}
			sort.Strings(states)
			quality = strings.Join(states, ",")
		}
		cgroupState := "not configured"
		if c.HostSensorConfigured {
			cgroupState = fmt.Sprintf("%d/%d", cgroupKnown, cgroupTotal)
		}
		processRSS := "unavailable"
		if processRSSCount > 0 {
			processRSS = fmt.Sprintf("%s-%s (%d)", bytesHuman(processRSSMin), bytesHuman(processRSSMax), processRSSCount)
		}
		swapUsed := "unavailable"
		if swapSamples > 0 {
			swapUsed = fmt.Sprintf("%s-%s (%d)", bytesHuman(swapUsedMin), bytesHuman(swapUsedMax), swapSamples)
		}
		processCPU, diskIO, networkIO := "unavailable", "unavailable", "unavailable"
		restarts := "unavailable"
		if ioSummary.ProcessRestartCoverage == "COMPLETE" {
			restarts = fmt.Sprintf("%d observed (%d series, %d samples)", ioSummary.ExporterReportedRestarts, ioSummary.ProcessRestartSeries, ioSummary.ProcessRestartSamples)
		} else if ioSummary.ProcessRestartSamples > 0 {
			restarts = fmt.Sprintf("%s (%d series, %d samples)", ioSummary.ProcessRestartCoverage, ioSummary.ProcessRestartSeries, ioSummary.ProcessRestartSamples)
		}
		if ioSummary.ProcessCPUSamples > 0 {
			processCPU = fmt.Sprintf("%.2f-%.2f%% (%d)", ioSummary.ProcessCPUMinPercent, ioSummary.ProcessCPUMaxPercent, ioSummary.ProcessCPUSamples)
		}
		if ioSummary.DiskSamples > 0 {
			diskIO = fmt.Sprintf("read %s-%s / write %s-%s (%d)", bytesHuman(ioSummary.DiskReadMinBytes), bytesHuman(ioSummary.DiskReadMaxBytes), bytesHuman(ioSummary.DiskWriteMinBytes), bytesHuman(ioSummary.DiskWriteMaxBytes), ioSummary.DiskSamples)
		}
		if ioSummary.NetworkSamples > 0 {
			networkIO = fmt.Sprintf("receive %s-%s / transmit %s-%s (%d)", bytesHuman(ioSummary.NetworkReceiveMinBytes), bytesHuman(ioSummary.NetworkReceiveMaxBytes), bytesHuman(ioSummary.NetworkTransmitMinBytes), bytesHuman(ioSummary.NetworkTransmitMaxBytes), ioSummary.NetworkSamples)
		}
		fmt.Fprintf(&b, "| %d | `%s` | %d VUs | %s | %d | %.2f-%.2f ms | %.3f%%-%.3f%% | %.2f-%.2f | %.2f-%.2f | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", campaignIndex+1, evidenceID, c.BaseUsers, c.Outcome, len(c.Trials), p95Min, p95Max, errMin*100, errMax*100, rpsMin, rpsMax, journeyMin, journeyMax, md(quality), md(appState), md(hostState), processCPU, restarts, processRSS, swapUsed, diskIO, networkIO, cgroupState)
	}
	b.WriteString("\n#### Per-phase results\n\n| Campaign | Evidence ID | Base VUs | Trial | Phase | Phase VUs | Outcome | Journey p95 | Request p95 | Error rate | HTTP requests/s | Successful journeys/s | Measurement | App telemetry | Host telemetry | Host CPU range | Process CPU range | Process RSS range | Host swap used range | Disk I/O range | Network I/O range | Cgroup samples available |\n|---:|---|---:|---:|---|---:|---|---:|---:|---:|---:|---:|---|---|---|---|---|---|---|---|---|---:|\n")
	for campaignIndex, c := range campaigns {
		evidenceID := "unavailable"
		if campaignIndex < len(a.EvidenceIDs) {
			evidenceID = a.EvidenceIDs[campaignIndex]
		}
		for _, trial := range c.Trials {
			for _, phase := range trial.Phases {
				seconds := phase.Run.WindowSeconds()
				rps := 0.0
				if seconds > 0 {
					rps = float64(phase.Run.Metrics.Total) / seconds
				}
				measurement, appCoverage, hostCoverage := "unknown", "not configured", "not configured"
				if phase.Run.Quality != nil {
					measurement = phase.Run.Quality.State
				}
				if _, appState, appErr := phaseApplicationTimeline(phase); appErr == nil && appState != "MISSING" {
					appCoverage = appState
				}
				if phase.HostTelemetry != nil {
					hostCoverage = phase.HostTelemetry.Coverage
				}
				cgroupAvailable, cgroupTotal := 0, 0
				processRSSMin, processRSSMax := uint64(0), uint64(0)
				processRSSCount := 0
				hostCPUMin, hostCPUMax, processCPUMin, processCPUMax := 0.0, 0.0, 0.0, 0.0
				hostCPUSamples, processCPUSamples := 0, 0
				swapUsedMin, swapUsedMax := uint64(0), uint64(0)
				swapSamples := 0
				diskReadMin, diskReadMax, diskWriteMin, diskWriteMax := uint64(0), uint64(0), uint64(0), uint64(0)
				diskReadSamples, diskWriteSamples := 0, 0
				networkReceiveMin, networkReceiveMax, networkTransmitMin, networkTransmitMax := uint64(0), uint64(0), uint64(0), uint64(0)
				networkReceiveSamples, networkTransmitSamples := 0, 0
				if phase.HostTelemetry != nil {
					for _, sample := range phase.HostTelemetry.Samples {
						for _, host := range sample.Reading.Host.Samples {
							if hostCPUSamples == 0 || host.CPUPercent < hostCPUMin {
								hostCPUMin = host.CPUPercent
							}
							if hostCPUSamples == 0 || host.CPUPercent > hostCPUMax {
								hostCPUMax = host.CPUPercent
							}
							hostCPUSamples++
							accumulateRange(&diskReadMin, &diskReadMax, &diskReadSamples, host.DiskReadBytes)
							accumulateRange(&diskWriteMin, &diskWriteMax, &diskWriteSamples, host.DiskWriteBytes)
							accumulateRange(&networkReceiveMin, &networkReceiveMax, &networkReceiveSamples, host.NetworkRxBytes)
							accumulateRange(&networkTransmitMin, &networkTransmitMax, &networkTransmitSamples, host.NetworkTxBytes)
							if host.SwapUsedBytes != nil && host.SwapTotalBytes != nil {
								if swapSamples == 0 || *host.SwapUsedBytes < swapUsedMin {
									swapUsedMin = *host.SwapUsedBytes
								}
								if swapSamples == 0 || *host.SwapUsedBytes > swapUsedMax {
									swapUsedMax = *host.SwapUsedBytes
								}
								swapSamples++
							}
							cgroupTotal++
							if hasCgroupEvidence(host.Workload) {
								cgroupAvailable++
							}
							if host.Workload != nil {
								if cpu := host.Workload.ProcessCPUPercent; cpu != nil && host.Workload.ProcessCPUCoverage == "COMPLETE" {
									if processCPUSamples == 0 || *cpu < processCPUMin {
										processCPUMin = *cpu
									}
									if processCPUSamples == 0 || *cpu > processCPUMax {
										processCPUMax = *cpu
									}
									processCPUSamples++
								}
								if rss := host.Workload.ProcessRSSBytes; rss != nil {
									if processRSSCount == 0 || *rss < processRSSMin {
										processRSSMin = *rss
									}
									if processRSSCount == 0 || *rss > processRSSMax {
										processRSSMax = *rss
									}
									processRSSCount++
								}
							}
						}
					}
				}
				processRSS := "unavailable"
				if processRSSCount > 0 {
					processRSS = fmt.Sprintf("%s-%s", bytesHuman(processRSSMin), bytesHuman(processRSSMax))
				}
				swapUsed := "unavailable"
				if swapSamples > 0 {
					swapUsed = fmt.Sprintf("%s-%s", bytesHuman(swapUsedMin), bytesHuman(swapUsedMax))
				}
				hostCPU, processCPU := "unavailable", "unavailable"
				if hostCPUSamples > 0 {
					hostCPU = fmt.Sprintf("%.2f-%.2f%% (%d)", hostCPUMin, hostCPUMax, hostCPUSamples)
				}
				if processCPUSamples > 0 {
					processCPU = fmt.Sprintf("%.2f-%.2f%% (%d)", processCPUMin, processCPUMax, processCPUSamples)
				}
				diskIO, networkIO := "unavailable", "unavailable"
				if diskReadSamples > 0 || diskWriteSamples > 0 {
					diskIO = fmt.Sprintf("read %s-%s / write %s-%s", bytesHuman(diskReadMin), bytesHuman(diskReadMax), bytesHuman(diskWriteMin), bytesHuman(diskWriteMax))
				}
				if networkReceiveSamples > 0 || networkTransmitSamples > 0 {
					networkIO = fmt.Sprintf("receive %s-%s / transmit %s-%s", bytesHuman(networkReceiveMin), bytesHuman(networkReceiveMax), bytesHuman(networkTransmitMin), bytesHuman(networkTransmitMax))
				}
				fmt.Fprintf(&b, "| %d | `%s` | %d | %d | %s | %d | %s | %.2f ms | %.2f ms | %.3f%% | %.2f | %.2f | %s | %s | %s | %s | %s | %s | %s | %s | %s | %d/%d |\n", campaignIndex+1, evidenceID, c.BaseUsers, trial.Number, md(phase.Name), phase.Run.Config.Users, phase.Run.Outcome, phase.Run.Journeys.P95MS, phase.Run.Metrics.P95MS, phase.Run.ErrorsRate*100, rps, report.SuccessfulJourneysPerSecond(phase.Run), md(measurement), md(appCoverage), md(hostCoverage), hostCPU, processCPU, processRSS, swapUsed, diskIO, networkIO, cgroupAvailable, cgroupTotal)
			}
		}
	}
	metricRows, metricOmitted := summarizeApplicationMetricsFromResults(campaigns)
	b.WriteString("\n#### Application metric observations\n\n| Campaign | Trial | Phase | Metric | Type | Unit | Label keys | Series | Observations | Observed range | Counter delta range (series; resets) |\n|---:|---:|---|---|---|---|---|---:|---:|---:|---|\n")
	for _, row := range metricRows {
		delta := "—"
		if row.CounterDeltaSeries > 0 {
			delta = fmt.Sprintf("%.6g-%.6g (%d; resets %d)", row.CounterDeltaMin, row.CounterDeltaMax, row.CounterDeltaSeries, row.CounterResets)
		} else if row.Type == "counter" {
			delta = fmt.Sprintf("unavailable (resets %d)", row.CounterResets)
		}
		fmt.Fprintf(&b, "| %d | %d | %s | `%s` | %s | %s | %s | %d | %d | %.6g-%.6g | %s |\n", row.Campaign, row.Trial, md(row.Phase), md(row.Name), md(row.Type), md(row.Unit), md(strings.Join(row.LabelKeys, ",")), row.Series, row.Observations, row.Min, row.Max, delta)
	}
	if len(metricRows) == 0 {
		b.WriteString("| — | — | — | No application metric samples | — | — | — | 0 | 0 | — | — |\n")
	}
	fmt.Fprintf(&b, "\nValue ranges span observations and label-distinct series, not sums or rates. Counter deltas are per stable label set between first and last timestamp-sorted sample; series with a decrease are reported as resets and excluded from delta ranges. Label values and exporter sources are omitted. At most %d rows are shown; %d additional rows omitted. Raw local campaign artifacts retain the underlying series. Metrics are correlations and do not establish bottleneck causality.\n", maxApplicationMetricSummaries, metricOmitted)
	b.WriteString("\nThe campaign JSON artifacts retain per-phase generator health, delivery counters, application series, host samples, collection gaps, and timing bounds. Host and selected-process CPU, memory, disk and network values are descriptive correlations; they do not establish bottleneck causality. Process RSS is a sampled range. A cgroup count of 0/n means host samples existed but selected-process cgroup data was unavailable; it is not zero resource use. Their evidence digests are:\n\n")
	for _, id := range a.EvidenceIDs {
		fmt.Fprintf(&b, "- `%s`\n", id)
	}
	b.WriteString("\n### Reproduction\n\n")
	fmt.Fprintf(&b, "Damocline version: `%s`\n\n", md(version))
	for _, command := range commands {
		fence := markdownFence(command)
		fmt.Fprintf(&b, "%stext\n%s\n%s\n\n", fence, command, fence)
	}
	b.WriteString("Recalculate the assessment with the sealed protocol and the listed campaign files. No traffic is generated by offline assessment. The bundle must remain local when it contains private scenario or report data.\n")
	return b.Bytes()
}

func formatSignedMargin(value int) string {
	if value > 0 {
		return fmt.Sprintf("+%d", value)
	}
	return fmt.Sprintf("%d", value)
}

func summarizeApplicationMetricsFromResults(campaigns []campaign.Result) ([]ApplicationMetricSummary, int) {
	artifacts := make([]CampaignArtifact, len(campaigns))
	for i, c := range campaigns {
		artifacts[i] = CampaignArtifact{Result: c}
	}
	return summarizeApplicationMetrics(artifacts)
}

func hasCgroupEvidence(workload *hostmon.Workload) bool {
	return workload != nil && (workload.CPUQuotaMicros != nil || workload.CPUPeriodMicros != nil || workload.MemoryLimitBytes != nil || workload.MemoryCurrentBytes != nil || len(workload.MemoryEvents) > 0)
}

func accumulateRange(minimum, maximum *uint64, count *int, value *uint64) {
	if value == nil {
		return
	}
	if count != nil {
		if *count == 0 || *value < *minimum {
			*minimum = *value
		}
		if *count == 0 || *value > *maximum {
			*maximum = *value
		}
		*count = *count + 1
		return
	}
}

func bytesHuman(value uint64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	amount, exponent := float64(value), 0
	for amount >= 1024 && exponent < 5 {
		amount /= 1024
		exponent++
	}
	prefix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.1f %s", amount, prefix[exponent-1])
}

func markdownFence(value string) string {
	fence := "```"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	return fence
}

func BuildReproductionBundle(p CapacityProtocol, a ConfirmationAssessment, campaigns []CampaignArtifact, version string, commands []string) ([]byte, error) {
	manifest := buildReproductionManifest(campaigns)
	bundle := ReproductionBundle{SchemaVersion: 1, DamoclineVersion: version, Protocol: p, Assessment: a, Campaigns: campaigns, Commands: append([]string{}, commands...), Manifest: manifest, Notes: []string{"Assessment is offline and does not generate traffic.", "The reproduction manifest restores embedded inputs to relative paths; run the command from a compatible Damocline source checkout.", "Protocol digest is not a signature or trusted timestamp.", "Campaign JSON may contain private target and environment metadata; keep this bundle local and out of public repositories."}}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func buildReproductionManifest(campaigns []CampaignArtifact) ReproductionManifest {
	m := ReproductionManifest{ProtocolPath: "reproduction/protocol.json", Campaigns: make([]ReproductionCampaignFile, 0, len(campaigns)), Requirements: []string{"Damocline source checkout at a revision matching the embedded assessment version", "Go toolchain compatible with that checkout", "No target traffic is generated during offline replay"}}
	args := []string{"go", "run", "./cmd/damocline", "protocol", "assess", "--file", m.ProtocolPath}
	for i, artifact := range campaigns {
		path := fmt.Sprintf("reproduction/campaign-%02d.json", i+1)
		m.Campaigns = append(m.Campaigns, ReproductionCampaignFile{Role: artifact.Role, Path: path, EvidenceID: artifact.EvidenceID})
		switch artifact.Role {
		case "exploration":
			args = append(args, "--exploration-campaign", path)
		case "confirmation":
			args = append(args, "--confirmation-campaign", path)
		case "failure_confirmation":
			args = append(args, "--failure-campaign", path)
		}
	}
	args = append(args, "--out", "reproduction/replayed-assessment.json", "--markdown", "reproduction/replayed-report.md")
	quoted := make([]string, len(args))
	copy(quoted, args)
	m.AssessmentCommand = strings.Join(quoted, " ")
	var restore strings.Builder
	restore.WriteString("$ErrorActionPreference = 'Stop'\n$bundle = Get-Content -Raw -LiteralPath 'reproduction-bundle.json' | ConvertFrom-Json\nNew-Item -ItemType Directory -Force -Path 'reproduction' | Out-Null\n$utf8NoBom = New-Object System.Text.UTF8Encoding($false)\n[System.IO.File]::WriteAllText('reproduction/protocol.json', ($bundle.protocol | ConvertTo-Json -Depth 100), $utf8NoBom)\n")
	for i := range campaigns {
		path := fmt.Sprintf("reproduction/campaign-%02d.json", i+1)
		fmt.Fprintf(&restore, "[System.IO.File]::WriteAllText('%s', ($bundle.campaigns[%d].result | ConvertTo-Json -Depth 100), $utf8NoBom)\n", path, i)
	}
	m.RestoreScript = restore.String()
	m.Requirements = append(m.Requirements, "Restore inputs into the reproduction/ directory at the root of the matching source checkout, then run the assessment command from that checkout root")
	return m
}

func coverageName(complete, total int) string {
	if total > 0 && complete == total {
		return "COMPLETE"
	}
	return "INCOMPLETE"
}

func md(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "`", "\\`")
	value = strings.ReplaceAll(value, "[", "\\[")
	value = strings.ReplaceAll(value, "]", "\\]")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "\r", " ")
}
