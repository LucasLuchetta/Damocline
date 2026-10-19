// Package engine implements Damocline's own bounded HTTP scheduling engine.
package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/scheduler"
)

const maxResponseBytes = 2 << 20

var variablePattern = regexp.MustCompile(`\{\{([a-z][a-z0-9_]{0,63})\}\}`)

type sample struct {
	name               string
	latency            time.Duration
	trace              report.HTTPTrace
	responseBodyState  report.ResponseBodyState
	responseBodyBytes  int64
	statusCode         int
	assertionFailure   bool
	tlsFailure         bool
	success, cancelled bool
	reason             string
}
type collector struct {
	mu           sync.Mutex
	aggregate    *report.MetricSet
	steps        map[string]*report.MetricSet
	stepTraces   map[string]*report.HTTPTrace
	thinkTime    time.Duration
	thinkSamples int
	profileThink map[string]thinkTimeTotal
	journeys     *report.MetricSet
	profiles     map[string]*report.MetricSet
	started      time.Time
	window       time.Duration
	observation  time.Duration
	windows      []*windowMetrics
}

type windowMetrics struct {
	aggregate *report.MetricSet
	journeys  *report.MetricSet
	steps     map[string]*report.MetricSet
	profiles  map[string]*report.MetricSet
}

type thinkTimeTotal struct {
	duration time.Duration
	samples  int
}

func newCollector(v config.Validated, started time.Time) *collector {
	c := &collector{aggregate: report.NewMetricSet(),
		journeys: report.NewMetricSet(), steps: make(map[string]*report.MetricSet, len(v.Steps)), stepTraces: make(map[string]*report.HTTPTrace, len(v.Steps)), profiles: make(map[string]*report.MetricSet, len(v.Profiles)), profileThink: make(map[string]thinkTimeTotal, len(v.Profiles)), started: started, window: v.Window, observation: v.Duration}
	for _, s := range v.Steps {
		c.steps[s.Name] = report.NewMetricSet()
		c.stepTraces[s.Name] = &report.HTTPTrace{}
	}
	for _, profile := range v.Profiles {
		c.profiles[profile.Name] = report.NewMetricSet()
	}
	if c.window > 0 {
		windowCount := int((c.observation + c.window - 1) / c.window)
		for index := 0; index < windowCount; index++ {
			c.windowAtIndex(index)
		}
	}
	return c
}

func (c *collector) addThinkTime(profile string, elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thinkTime += elapsed
	c.thinkSamples++
	v := c.profileThink[profile]
	v.duration += elapsed
	v.samples++
	c.profileThink[profile] = v
}

func (c *collector) addJourney(profile string, latency time.Duration, success, cancelled bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.journeys.Record(latency, success, cancelled, reason)
	c.profiles[profile].Record(latency, success, cancelled, reason)
	if c.window > 0 {
		elapsed := time.Since(c.started)
		if elapsed < c.observation {
			window := c.windowAt(elapsed)
			window.journeys.Record(latency, success, cancelled, reason)
			window.profiles[profile].Record(latency, success, cancelled, reason)
		}
	}
}

type profileSelector struct {
	weights []int
	current []int
	total   int
}

func newProfileSelector(profiles []config.PreparedProfile) *profileSelector {
	s := &profileSelector{weights: make([]int, len(profiles)), current: make([]int, len(profiles))}
	for i, profile := range profiles {
		s.weights[i] = profile.Weight
		s.total += profile.Weight
	}
	return s
}

// next uses smooth weighted round-robin. For every prefix of selections, each
// profile remains close to its requested weight without random sampling noise.
func (s *profileSelector) next() int {
	selected := 0
	for i := range s.weights {
		s.current[i] += s.weights[i]
		if s.current[i] > s.current[selected] {
			selected = i
		}
	}
	s.current[selected] -= s.total
	return selected
}
func (c *collector) add(s sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aggregate.Record(s.latency, s.success, s.cancelled, s.reason)
	c.steps[s.name].Record(s.latency, s.success, s.cancelled, s.reason)
	c.aggregate.RecordHTTPOutcome(s.statusCode, s.reason, s.assertionFailure, s.tlsFailure)
	c.steps[s.name].RecordHTTPOutcome(s.statusCode, s.reason, s.assertionFailure, s.tlsFailure)
	if s.responseBodyState != "" {
		c.aggregate.RecordResponseBody(s.responseBodyState, s.responseBodyBytes)
		c.steps[s.name].RecordResponseBody(s.responseBodyState, s.responseBodyBytes)
	}
	c.stepTraces[s.name].Record(s.trace)
	if c.window <= 0 {
		return
	}
	elapsed := time.Since(c.started)
	if elapsed >= c.observation {
		return
	}
	index := int(elapsed / c.window)
	if index < 0 {
		return
	}
	window := c.windowAtIndex(index)
	window.aggregate.Record(s.latency, s.success, s.cancelled, s.reason)
	window.steps[s.name].Record(s.latency, s.success, s.cancelled, s.reason)
	window.aggregate.RecordHTTPOutcome(s.statusCode, s.reason, s.assertionFailure, s.tlsFailure)
	window.steps[s.name].RecordHTTPOutcome(s.statusCode, s.reason, s.assertionFailure, s.tlsFailure)
	if s.responseBodyState != "" {
		window.aggregate.RecordResponseBody(s.responseBodyState, s.responseBodyBytes)
		window.steps[s.name].RecordResponseBody(s.responseBodyState, s.responseBodyBytes)
	}
}

func (c *collector) windowAt(elapsed time.Duration) *windowMetrics {
	return c.windowAtIndex(int(elapsed / c.window))
}

func (c *collector) windowAtIndex(index int) *windowMetrics {
	for len(c.windows) <= index {
		window := &windowMetrics{aggregate: report.NewMetricSet(), journeys: report.NewMetricSet(), steps: make(map[string]*report.MetricSet, len(c.steps)), profiles: make(map[string]*report.MetricSet, len(c.profiles))}
		for name := range c.steps {
			window.steps[name] = report.NewMetricSet()
		}
		for name := range c.profiles {
			window.profiles[name] = report.NewMetricSet()
		}
		c.windows = append(c.windows, window)
	}
	return c.windows[index]
}

func Run(parent context.Context, v config.Validated) report.Result {
	return runWithTransport(parent, v, nil)
}

func runWithTransport(parent context.Context, v config.Validated, transport *http.Transport) report.Result {
	started := time.Now()
	// Stop admitting new work at the configured boundary, then allow any
	// already-started journey a bounded time to finish.
	drainAllowance := drainDuration(v)
	ctx, cancel := context.WithTimeout(parent, v.Duration+drainAllowance)
	defer cancel()
	generatorMonitor := generatorhealth.StartWithGuardrails(250*time.Millisecond, v.GeneratorGuardrails, func(generatorhealth.GuardrailTrigger) {
		cancel()
	})
	scheduleCtx, cancelSchedule := context.WithTimeout(ctx, v.Duration)
	defer cancelSchedule()
	coll := newCollector(v, started)
	ticker := time.NewTicker(time.Second / time.Duration(v.Scenario.MaxRPS))
	defer ticker.Stop()
	tr := transport
	if tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		tr = tr.Clone()
	}
	tr.MaxIdleConns = v.Scenario.Users * 3
	tr.MaxIdleConnsPerHost = v.Scenario.Users * 2
	tr.MaxConnsPerHost = v.Scenario.Users + 4
	tr.DisableKeepAlives = v.Scenario.ConnectionPolicy == "new_per_request"
	tr.ResponseHeaderTimeout = v.Timeout
	tr.IdleConnTimeout = 15 * time.Second
	defer tr.CloseIdleConnections()
	var startedIterations, completedIterations, scheduledIterations, droppedIterations atomic.Int64
	var inFlight, inFlightTotal, inFlightSamples atomic.Int64
	var wg sync.WaitGroup
	var arrivalStats scheduler.Stats
	clients := make([]*http.Client, v.Scenario.Users)
	sampleDone := make(chan struct{})
	go func() {
		<-scheduleCtx.Done()
		close(sampleDone)
	}()
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sampleDone:
				return
			case <-ticker.C:
				inFlightTotal.Add(inFlight.Load())
				inFlightSamples.Add(1)
			}
		}
	}()
	for i := range clients {
		jar, _ := cookiejar.New(nil)
		if len(v.Cookies) > 0 {
			for _, part := range strings.Split(v.Cookies[i], ";") {
				part = strings.TrimSpace(part)
				pair := strings.SplitN(part, "=", 2)
				if len(pair) == 2 && strings.TrimSpace(pair[0]) != "" {
					jar.SetCookies(v.Base, []*http.Cookie{{Name: strings.TrimSpace(pair[0]), Value: strings.TrimSpace(pair[1]), Path: "/"}})
				}
			}
		}
		clients[i] = &http.Client{Transport: tr, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	var syntheticSequence atomic.Int64
	iteration := func(client *http.Client, profile config.PreparedProfile, actor int, journeyOrdinal uint64) {
		startedIterations.Add(1)
		journeyStart := time.Now()
		journeyOk := true
		fullyCompleted := true
		variables := map[string]string{}
		if v.SyntheticDataPrefix != "" {
			sequence := syntheticSequence.Add(1)
			variables["synthetic_id"] = fmt.Sprintf("%s-%x-%d-%d", v.SyntheticDataPrefix, started.UnixNano(), actor+1, sequence)
		}
		for stepIndex, step := range profile.Steps {
			waitStarted := time.Now()
			if !waitToken(ctx, ticker.C) {
				coll.add(sample{name: step.Name, cancelled: true, reason: "journey_cancelled", trace: report.HTTPTrace{RateWaitCancelledCount: 1, RateWaitMS: ms(time.Since(waitStarted))}})
				fullyCompleted = false
				break
			}
			s, extracted := execute(ctx, client, v.Base, v.Timeout, step, variables, &inFlight)
			s.trace.RateWaitCount = 1
			s.trace.RateWaitMS = ms(time.Since(waitStarted))
			coll.add(s)
			for name, value := range extracted {
				variables[name] = value
			}
			if !s.success {
				journeyOk = false
			}
			if s.cancelled {
				fullyCompleted = false
				break
			}
			pause := profile.ThinkTime
			if profile.ThinkTimeMax > 0 || len(profile.ThinkTimeSamples) > 0 {
				timingActor := actor
				if v.Scenario.Mode == "arrival" {
					timingActor = -1
				}
				pause = sampledThinkTime(v.Scenario.WorkloadSeed, timingActor, journeyOrdinal, stepIndex, profile)
			}
			if stepIndex < len(profile.Steps)-1 && pause > 0 {
				thinkStarted := time.Now()
				continued := sleep(ctx, pause)
				coll.addThinkTime(profile.Name, time.Since(thinkStarted))
				if !continued {
					fullyCompleted = false
					break
				}
			}
		}
		if fullyCompleted {
			completedIterations.Add(1)
			coll.addJourney(profile.Name, time.Since(journeyStart), journeyOk, false, func() string {
				if journeyOk {
					return ""
				}
				return "journey_step_failed"
			}())
		} else {
			coll.addJourney(profile.Name, time.Since(journeyStart), false, true, "journey_cancelled")
		}
	}
	selector := newProfileSelector(v.Profiles)
	assignedVUs := make([]int, len(v.Profiles))
	if v.Scenario.Mode == "arrival" {
		type selectedArrival struct {
			profile int
			ordinal uint64
		}
		arrivals := make(chan selectedArrival) // unbuffered: busy workers cannot hide dropped arrivals
		for i := 0; i < v.Scenario.Users; i++ {
			wg.Add(1)
			go func(client *http.Client, actor int) {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case arrival, ok := <-arrivals:
						if !ok {
							return
						}
						iteration(client, v.Profiles[arrival.profile], actor, arrival.ordinal)
					}
				}
			}(clients[i], i)
		}
		var arrivalOrdinal uint64
		arrivalStats = scheduler.Drive(scheduleCtx, v.Duration, v.Scenario.ArrivalRate, func() bool {
			profileIndex := selector.next()
			arrivalOrdinal++
			select {
			case arrivals <- selectedArrival{profile: profileIndex, ordinal: arrivalOrdinal}:
				return true
			default:
				return false
			}
		})
		scheduledIterations.Store(arrivalStats.Planned)
		droppedIterations.Store(arrivalStats.Dropped())
		close(arrivals)
	} else {
		closedProfiles := make([]int, v.Scenario.Users)
		for i := range closedProfiles {
			closedProfiles[i] = selector.next()
			assignedVUs[closedProfiles[i]]++
		}
		for i := 0; i < v.Scenario.Users; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				delay := time.Duration(index) * v.RampUp / time.Duration(v.Scenario.Users)
				if !sleep(scheduleCtx, delay) {
					return
				}
				profileIndex := closedProfiles[index]
				var journeyOrdinal uint64
				for scheduleCtx.Err() == nil {
					scheduledIterations.Add(1)
					journeyOrdinal++
					iteration(clients[index], v.Profiles[profileIndex], index, journeyOrdinal)
				}
			}(i)
		}
	}
	cancelledBeforeAdmission := arrivalStats.CancelledBeforeAdmission
	arrivalsAdmitted := arrivalStats.Admitted
	wg.Wait()
	generatorSnapshot := generatorMonitor.Stop()
	coll.mu.Lock()
	agg := coll.aggregate.Snapshot()
	journeyMetrics := coll.journeys.Snapshot()
	steps := make([]report.StepResult, 0, len(v.Steps))
	for _, st := range v.Steps {
		profileName := ""
		if len(v.Scenario.Profiles) > 0 {
			profileName = st.Profile
		}
		steps = append(steps, report.StepResult{Profile: profileName, Name: st.Name, Method: st.Method, Path: safePath(st.Path), Metrics: coll.steps[st.Name].Snapshot(), HTTPTrace: *coll.stepTraces[st.Name]})
	}
	windows := make([]report.WindowResult, 0, len(coll.windows))
	for index, metrics := range coll.windows {
		startSeconds := float64(index) * v.Window.Seconds()
		endSeconds := math.Min(startSeconds+v.Window.Seconds(), v.Duration.Seconds())
		window := report.WindowResult{StartSeconds: round(startSeconds), EndSeconds: round(endSeconds), Metrics: metrics.aggregate.Snapshot(), Journeys: metrics.journeys.Snapshot()}
		for _, step := range v.Steps {
			window.Steps = append(window.Steps, report.WindowMetric{Name: step.Name, Metrics: metrics.steps[step.Name].Snapshot()})
		}
		if len(v.Scenario.Profiles) > 0 {
			for _, profile := range v.Profiles {
				window.Profiles = append(window.Profiles, report.WindowMetric{Name: profile.Name, Metrics: metrics.profiles[profile.Name].Snapshot()})
			}
		}
		windows = append(windows, window)
	}
	var profileResults []report.ProfileResult
	var profileConfig []report.ProfileConfig
	if len(v.Scenario.Profiles) > 0 {
		profileResults = make([]report.ProfileResult, 0, len(v.Profiles))
		profileConfig = make([]report.ProfileConfig, 0, len(v.Profiles))
		for i, profile := range v.Profiles {
			think := coll.profileThink[profile.Name]
			profileResults = append(profileResults, report.ProfileResult{Name: profile.Name, Weight: profile.Weight, AssignedVUs: assignedVUs[i], Journeys: coll.profiles[profile.Name].Snapshot(), ObservedThinkTimeMS: ms(think.duration), ThinkTimeObservations: think.samples})
			entry := report.ProfileConfig{Name: profile.Name, Weight: profile.Weight, ThinkTime: profile.ThinkTime.String()}
			if profile.ThinkTimeMax > 0 {
				entry.ThinkTime = ""
				entry.ThinkTimeRange = &report.ThinkTimeRange{Min: profile.ThinkTimeMin.String(), Max: profile.ThinkTimeMax.String()}
			}
			if len(profile.ThinkTimeSamples) > 0 {
				entry.ThinkTime = ""
				for _, sample := range profile.ThinkTimeSamples {
					entry.ThinkTimeSamples = append(entry.ThinkTimeSamples, sample.String())
				}
			}
			profileConfig = append(profileConfig, entry)
		}
	}
	coll.mu.Unlock()
	elapsed := time.Since(started)
	errorsRate := 0.0
	if agg.Total > 0 {
		errorsRate = float64(agg.Failures) / float64(agg.Total)
	}
	averageInFlight := 0.0
	if samples := inFlightSamples.Load(); samples > 0 {
		averageInFlight = float64(inFlightTotal.Load()) / float64(samples)
	}
	littleLawEstimate := 0.0
	if v.Duration > 0 {
		littleLawEstimate = float64(agg.Total) / v.Duration.Seconds() * agg.MeanMS / 1000
	}
	result := report.Result{SchemaVersion: report.CurrentSchemaVersion, Name: v.Scenario.Name, Target: v.Base.Scheme + "://" + v.Base.Host, StartedAt: started.UTC().Format(time.RFC3339), ElapsedSeconds: round(elapsed.Seconds()), ObservationSeconds: round(v.Duration.Seconds()),
		Config:  report.RunConfig{WorkloadRevision: v.Scenario.WorkloadRevision, ApplicationRevision: v.Scenario.ApplicationRevision, WorkloadFingerprint: v.WorkloadFingerprint, Environment: v.Scenario.Environment, ProfileMixBasis: v.Scenario.ProfileMixBasis, ProfileMixReference: v.Scenario.ProfileMixReference, Users: v.Scenario.Users, Duration: v.Scenario.Duration, RampUp: v.Scenario.RampUp, ThinkTime: v.Scenario.ThinkTime, WorkloadSeed: v.Scenario.WorkloadSeed, RequestTimeout: v.Timeout.String(), ConnectionPolicy: v.Scenario.ConnectionPolicy, MaxRPS: v.Scenario.MaxRPS, P95LimitMS: v.Scenario.P95LimitMS, MaxErrorRate: v.Scenario.MaxErrorRate, Mode: v.Scenario.Mode, ArrivalRate: v.Scenario.ArrivalRate, SeparateSessions: len(v.Cookies) > 0, MinSamplesPerStep: v.Scenario.MinSamplesPerStep, Profiles: profileConfig, WindowDuration: v.Window.String()},
		Metrics: agg, Journeys: journeyMetrics, Steps: steps, Profiles: profileResults, Windows: windows, Outcome: "PASS", ErrorsRate: round(errorsRate), IterationsStarted: startedIterations.Load(), IterationsCompleted: completedIterations.Load(), ArrivalsScheduled: scheduledIterations.Load(), ArrivalsAdmitted: &arrivalsAdmitted, ArrivalsDropped: droppedIterations.Load(), ArrivalsDroppedLate: arrivalStats.DroppedLate, ArrivalsDroppedBusy: arrivalStats.DroppedBusy, ArrivalsCancelledBeforeAdmission: &cancelledBeforeAdmission, SchedulerLagSamples: arrivalStats.LagSamples, SchedulerLagP50MS: float64(arrivalStats.P50Lag.Microseconds()) / 1000, SchedulerLagP95MS: float64(arrivalStats.P95Lag.Microseconds()) / 1000, SchedulerLagMaxMS: float64(arrivalStats.MaxLag.Microseconds()) / 1000, SchedulerLagMethod: calibration.SchedulerDispatchLagMethod, AverageInFlight: round(averageInFlight), LittleLawEstimate: round(littleLawEstimate), ObservedThinkTimeMS: ms(coll.thinkTime), ThinkTimeObservations: coll.thinkSamples,
		Note: "Measured synthetic HTTP actors under the configured workload; not a universal server capacity or a browser-user count.", Generator: &generatorSnapshot}
	if v.Scenario.ThinkTimeRange != nil {
		result.Config.ThinkTimeRange = &report.ThinkTimeRange{Min: v.Scenario.ThinkTimeRange.Min, Max: v.Scenario.ThinkTimeRange.Max}
	}
	result.Config.ThinkTimeSamples = append([]string(nil), v.Scenario.ThinkTimeSamples...)
	if agg.Total == 0 || agg.Success == 0 || errorsRate > v.Scenario.MaxErrorRate || agg.P95MS > v.Scenario.P95LimitMS || result.ArrivalsDropped > 0 {
		result.Outcome = "FAIL"
	}
	if journeyMetrics.Total == 0 {
		if result.Outcome == "PASS" {
			result.Outcome = "INCONCLUSIVE"
		}
		result.Warnings = append(result.Warnings, "No completed journeys were observed; full-journey SLO evidence is unavailable.")
	} else {
		journeyErrorRate := float64(journeyMetrics.Failures) / float64(journeyMetrics.Total)
		if journeyErrorRate > v.Scenario.MaxErrorRate || journeyMetrics.P95MS > v.Scenario.P95LimitMS {
			result.Outcome = "FAIL"
			result.Warnings = append(result.Warnings, "Completed journeys breached the configured latency or error objective.")
		}
	}
	for _, st := range steps {
		if st.Metrics.Total < v.Scenario.MinSamplesPerStep || st.Metrics.Success == 0 || st.Metrics.P95MS > v.Scenario.P95LimitMS || (st.Metrics.Total > 0 && float64(st.Metrics.Failures)/float64(st.Metrics.Total) > v.Scenario.MaxErrorRate) {
			result.Outcome = "FAIL"
		}
	}
	for _, window := range windows {
		if window.Journeys.Total == 0 {
			if result.Outcome == "PASS" {
				result.Outcome = "INCONCLUSIVE"
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("Observation window %.1f-%.1fs has no completed journeys; full-journey evidence is insufficient.", window.StartSeconds, window.EndSeconds))
		} else {
			journeyErrorRate := float64(window.Journeys.Failures) / float64(window.Journeys.Total)
			if window.Journeys.P95MS > v.Scenario.P95LimitMS || journeyErrorRate > v.Scenario.MaxErrorRate {
				result.Outcome = "FAIL"
				result.Warnings = append(result.Warnings, fmt.Sprintf("Completed journeys in observation window %.1f-%.1fs breached an SLO.", window.StartSeconds, window.EndSeconds))
			}
		}
		if window.Metrics.Total < v.Scenario.MinSamplesPerStep {
			if result.Outcome == "PASS" {
				result.Outcome = "INCONCLUSIVE"
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("Observation window %.1f-%.1fs has only %d completed requests; interval evidence is insufficient.", window.StartSeconds, window.EndSeconds, window.Metrics.Total))
			continue
		}
		windowErrorRate := float64(window.Metrics.Failures) / float64(window.Metrics.Total)
		if window.Metrics.P95MS > v.Scenario.P95LimitMS || windowErrorRate > v.Scenario.MaxErrorRate {
			result.Outcome = "FAIL"
			result.Warnings = append(result.Warnings, fmt.Sprintf("Observation window %.1f-%.1fs breached an SLO.", window.StartSeconds, window.EndSeconds))
		}
		for _, step := range window.Steps {
			if step.Metrics.Total < v.Scenario.MinSamplesPerStep || step.Metrics.Success == 0 {
				if result.Outcome == "PASS" {
					result.Outcome = "INCONCLUSIVE"
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("A named step in observation window %.1f-%.1fs has insufficient completed samples.", window.StartSeconds, window.EndSeconds))
				continue
			}
			stepErrorRate := float64(step.Metrics.Failures) / float64(step.Metrics.Total)
			if step.Metrics.P95MS > v.Scenario.P95LimitMS || stepErrorRate > v.Scenario.MaxErrorRate {
				result.Outcome = "FAIL"
				result.Warnings = append(result.Warnings, fmt.Sprintf("A named step in observation window %.1f-%.1fs breached an SLO.", window.StartSeconds, window.EndSeconds))
			}
		}
		for _, profile := range window.Profiles {
			if profile.Metrics.Total < v.Scenario.MinSamplesPerStep || profile.Metrics.Success == 0 {
				if result.Outcome == "PASS" {
					result.Outcome = "INCONCLUSIVE"
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("A workflow profile in observation window %.1f-%.1fs has insufficient completed journeys.", window.StartSeconds, window.EndSeconds))
				continue
			}
			profileErrorRate := float64(profile.Metrics.Failures) / float64(profile.Metrics.Total)
			if profile.Metrics.P95MS > v.Scenario.P95LimitMS || profileErrorRate > v.Scenario.MaxErrorRate {
				result.Outcome = "FAIL"
				result.Warnings = append(result.Warnings, fmt.Sprintf("A workflow profile in observation window %.1f-%.1fs breached an SLO.", window.StartSeconds, window.EndSeconds))
			}
		}
	}
	if arrivalStats.DroppedLate > 0 {
		result.Outcome = "INCONCLUSIVE"
	}
	if errors.Is(parent.Err(), context.Canceled) || errors.Is(parent.Err(), context.DeadlineExceeded) {
		result.Outcome = "ABORTED"
	}
	if generatorSnapshot.GuardrailTrigger != nil {
		result.Outcome = "ABORTED"
		trigger := generatorSnapshot.GuardrailTrigger
		if trigger.Resource == "generator_process_cpu" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Generator process CPU guardrail triggered at %.1f%% (limit %.1f%%) for %d consecutive samples; this run is aborted and cannot be attributed to the target application.", trigger.ObservedCPUPercent, trigger.LimitCPUPercent, trigger.ConsecutiveSamples))
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Generator process RSS guardrail triggered at %d bytes (limit %d bytes) for %d consecutive samples; this run is aborted and cannot be attributed to the target application.", trigger.ObservedRSSBytes, trigger.LimitRSSBytes, trigger.ConsecutiveSamples))
		}
	} else if v.GeneratorGuardrails.Enabled() &&
		((v.GeneratorGuardrails.MaxProcessRSSBytes > 0 && generatorSnapshot.ProcessSamples < v.GeneratorGuardrails.ConsecutiveSamples) ||
			(v.GeneratorGuardrails.MaxProcessCPUPercent > 0 && generatorSnapshot.ProcessCPUSamples < v.GeneratorGuardrails.ConsecutiveSamples)) {
		if result.Outcome == "PASS" {
			result.Outcome = "INCONCLUSIVE"
		}
		result.Warnings = append(result.Warnings, "Configured generator resource guardrails lacked enough valid process samples to establish that the limits were not exceeded.")
	}
	if len(v.Cookies) == 0 && v.Scenario.Users > 1 {
		result.Warnings = append(result.Warnings, "Multiple virtual users may share one login identity; configure per_vu_cookie_env_prefix to test independent authenticated sessions.")
	}
	// A saturated request-rate governor masks the offered capacity of added VUs.
	if agg.Total > 0 && v.Duration.Seconds() > 0 && float64(agg.Total)/v.Duration.Seconds() > 0.9*float64(v.Scenario.MaxRPS) {
		result.Warnings = append(result.Warnings, "Request throughput is near max_rps: the configured client-side rate ceiling may hide server capacity. This run alone cannot establish a user-count limit.")
	}
	if arrivalStats.DroppedBusy > 0 {
		result.Warnings = append(result.Warnings, "Planned arrivals found no idle synthetic workers. This is not proof of server saturation.")
	}
	if arrivalStats.DroppedLate > 0 {
		result.Warnings = append(result.Warnings, "The load generator missed planned arrival deadlines; this trial cannot demonstrate a server capacity ceiling.")
	}
	if arrivalStats.CancelledBeforeAdmission > 0 {
		result.Warnings = append(result.Warnings, "Planned arrivals not admitted before external test cancellation are counted separately from scheduler-late and busy-worker drops.")
	}
	if v.Scenario.Mode == "arrival" && !arrivalStats.Consistent() {
		result.Outcome = "INCONCLUSIVE"
		result.Warnings = append(result.Warnings, "Arrival counts did not reconcile: treat measurements as invalid.")
	}
	if agg.Cancelled > 0 {
		result.Warnings = append(result.Warnings, "In-flight requests were cancelled at the test boundary; completion statistics exclude them.")
	}
	if result.Journeys.Cancelled > 0 {
		result.Warnings = append(result.Warnings, "One or more journeys were incomplete at the run boundary; journey latency percentiles exclude them.")
		if result.Outcome == "PASS" {
			result.Outcome = "INCONCLUSIVE"
		}
	}

	result.Quality = report.AssessQuality(result)
	result.Signals = report.DetectSignals(result)
	if result.Quality.State == report.QualityInvalid && result.Outcome == "PASS" {
		result.Outcome = "INCONCLUSIVE"
		result.Warnings = append(result.Warnings, "Inconsistent delivery/accounting evidence prevents a passing observation.")
	}
	return result
}

func waitToken(ctx context.Context, ch <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ch:
		return ctx.Err() == nil
	}
}

func drainDuration(v config.Validated) time.Duration {
	maximum := time.Duration(0)
	for _, profile := range v.Profiles {
		candidate := v.Timeout*time.Duration(len(profile.Steps)) + profile.ThinkTime*time.Duration(len(profile.Steps)-1)
		if candidate > maximum {
			maximum = candidate
		}
	}
	return min(30*time.Second, maximum)
}

func sampledThinkTime(seed uint64, actor int, journeyOrdinal uint64, stepIndex int, profile config.PreparedProfile) time.Duration {
	key := seed ^ (uint64(actor+1) * 0x9e3779b97f4a7c15)
	key ^= journeyOrdinal * 0xbf58476d1ce4e5b9
	key ^= uint64(stepIndex+1) * 0x94d049bb133111eb
	key += 0x9e3779b97f4a7c15
	key = (key ^ (key >> 30)) * 0xbf58476d1ce4e5b9
	key = (key ^ (key >> 27)) * 0x94d049bb133111eb
	key ^= key >> 31
	if len(profile.ThinkTimeSamples) > 0 {
		return profile.ThinkTimeSamples[key%uint64(len(profile.ThinkTimeSamples))]
	}
	minimum := uint64(profile.ThinkTimeMin)
	span := uint64(profile.ThinkTimeMax - profile.ThinkTimeMin)
	return time.Duration(minimum + key%(span+1))
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return ctx.Err() == nil
	}
}
func execute(parent context.Context, client *http.Client, base *url.URL, timeout time.Duration, s config.PreparedStep, variables map[string]string, inFlight *atomic.Int64) (sample, map[string]string) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	requestURL, err := interpolateURL(base, s.PathTemplate, variables)
	if err != nil {
		return sample{name: s.Name, reason: "variable_interpolation"}, nil
	}
	var body io.Reader
	if len(s.Body) > 0 {
		body = bytes.NewReader([]byte(interpolate(string(s.Body), variables)))
	}
	req, err := http.NewRequestWithContext(ctx, s.Method, requestURL, body)
	if err != nil {
		return sample{name: s.Name, reason: "request_build"}, nil
	}
	for k, v := range s.ResolvedHeaders {
		value := interpolate(v, variables)
		if strings.ContainsAny(value, "\r\n") {
			return sample{name: s.Name, reason: "variable_interpolation"}, nil
		}
		req.Header.Set(k, value)
	}
	start := time.Now()
	var traceMu sync.Mutex
	trace := report.HTTPTrace{}
	trace.RequestAttemptCount = 1
	var dnsStart, tlsStart, firstByte time.Time
	connectStarts := make(map[string][]time.Time)
	clientTrace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			traceMu.Lock()
			dnsStart = time.Now()
			trace.DNSStartCount++
			traceMu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			traceMu.Lock()
			if !dnsStart.IsZero() {
				trace.DNSCount++
				trace.DNSMS += ms(time.Since(dnsStart))
				dnsStart = time.Time{}
			}
			traceMu.Unlock()
		},
		ConnectStart: func(network, address string) {
			traceMu.Lock()
			key := network + "\x00" + address
			connectStarts[key] = append(connectStarts[key], time.Now())
			trace.ConnectStartCount++
			traceMu.Unlock()
		},
		ConnectDone: func(network, address string, _ error) {
			traceMu.Lock()
			key := network + "\x00" + address
			starts := connectStarts[key]
			if len(starts) > 0 {
				trace.ConnectCount++
				trace.ConnectMS += ms(time.Since(starts[0]))
				if len(starts) == 1 {
					delete(connectStarts, key)
				} else {
					connectStarts[key] = starts[1:]
				}
			}
			traceMu.Unlock()
		},
		TLSHandshakeStart: func() {
			traceMu.Lock()
			tlsStart = time.Now()
			trace.TLSStartCount++
			traceMu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			traceMu.Lock()
			if !tlsStart.IsZero() {
				trace.TLSCount++
				trace.TLSMS += ms(time.Since(tlsStart))
				tlsStart = time.Time{}
			}
			traceMu.Unlock()
		},
		GotFirstResponseByte: func() {
			traceMu.Lock()
			firstByte = time.Now()
			trace.FirstByteCount++
			trace.FirstByteMS += ms(time.Since(start))
			traceMu.Unlock()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			traceMu.Lock()
			if info.Err == nil {
				trace.WriteCount++
				trace.WriteMS += ms(time.Since(start))
			} else {
				trace.WriteFailedCount++
			}
			traceMu.Unlock()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), clientTrace))
	inFlight.Add(1)
	defer inFlight.Add(-1)
	res, err := client.Do(req)
	if err != nil {
		d := time.Since(start)
		traceMu.Lock()
		trace.CompleteCount++
		trace.CompleteMS += ms(d)
		observedTrace := trace
		traceMu.Unlock()
		if parent.Err() != nil {
			return sample{name: s.Name, latency: d, trace: observedTrace, cancelled: true, reason: "run_cancelled"}, nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return sample{name: s.Name, latency: d, trace: observedTrace, reason: "timeout"}, nil
		}
		if isDNSFailure(err) {
			return sample{name: s.Name, latency: d, trace: observedTrace, reason: "dns_failure"}, nil
		}
		if isTLSFailure(err) {
			return sample{name: s.Name, latency: d, trace: observedTrace, reason: "transport", tlsFailure: true}, nil
		}
		if isConnectionEstablishmentFailure(err) {
			return sample{name: s.Name, latency: d, trace: observedTrace, reason: "connection_failure"}, nil
		}
		return sample{name: s.Name, latency: d, trace: observedTrace, reason: "transport"}, nil
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	responseBodyState := report.ResponseBodyIncomplete
	if readErr == nil {
		responseBodyState = report.ResponseBodyComplete
		if len(data) > maxResponseBytes {
			responseBodyState = report.ResponseBodyOversized
		}
	}
	responseBodyBytes := int64(len(data))
	if responseBodyState != report.ResponseBodyComplete {
		responseBodyBytes = 0
	}
	d := time.Since(start)
	traceMu.Lock()
	if !firstByte.IsZero() {
		trace.BodyMS += ms(time.Since(firstByte))
		if readErr == nil {
			trace.BodyCount++
		} else {
			trace.BodyIncompleteCount++
		}
	}
	trace.CompleteCount++
	trace.CompleteMS += ms(d)
	observedTrace := trace
	traceMu.Unlock()
	responseSample := func(reason string) sample {
		return sample{
			name:              s.Name,
			latency:           d,
			trace:             observedTrace,
			responseBodyState: responseBodyState,
			responseBodyBytes: responseBodyBytes,
			statusCode:        res.StatusCode,
			reason:            reason,
		}
	}
	if parent.Err() != nil {
		observed := responseSample("run_cancelled")
		observed.cancelled = true
		return observed, nil
	}
	if readErr != nil {
		if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return responseSample("timeout"), nil
		}
		return responseSample("body_read"), nil
	}
	if len(data) > maxResponseBytes {
		return responseSample("response_too_large"), nil
	}
	ok := res.StatusCode >= 200 && res.StatusCode <= 299
	if len(s.ExpectedStatus) > 0 {
		ok = false
		for _, code := range s.ExpectedStatus {
			if code == res.StatusCode {
				ok = true
				break
			}
		}
	}
	if !ok {
		observed := responseSample(fmt.Sprintf("http_%d", res.StatusCode))
		observed.assertionFailure = len(s.ExpectedStatus) > 0
		return observed, nil
	}
	if s.MustContain != "" && !bytes.Contains(data, []byte(s.MustContain)) {
		observed := responseSample("assertion_missing_text")
		observed.assertionFailure = true
		return observed, nil
	}
	if len(s.JSONAssertions) > 0 && !jsonAssertionsMatch(s.JSONAssertions, data) {
		observed := responseSample("assertion_json_mismatch")
		observed.assertionFailure = true
		return observed, nil
	}
	extracted, err := extractValues(s.Extract, res.Header, data)
	if err != nil {
		return responseSample("extraction_failed"), nil
	}
	observed := responseSample("")
	observed.success = true
	return observed, extracted
}

func jsonAssertionsMatch(assertions map[string]string, body []byte) bool {
	document, err := decodeJSONDocument(body)
	if err != nil {
		return false
	}
	for pointer, expected := range assertions {
		actual, ok := jsonPointer(document, pointer)
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

func decodeJSONDocument(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return document, nil
}

func isDNSFailure(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

func isConnectionEstablishmentFailure(err error) bool {
	if isDNSFailure(err) {
		return false
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	return opErr.Op == "dial" || opErr.Op == "connect"
}

func isTLSFailure(err error) bool {
	if err == nil {
		return false
	}
	var recordHeader tls.RecordHeaderError
	var certificateVerification *tls.CertificateVerificationError
	if errors.As(err, &recordHeader) || errors.As(err, &certificateVerification) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "tls") || strings.Contains(message, "x509") || strings.Contains(message, "certificate")
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func interpolateURL(base *url.URL, pathTemplate string, variables map[string]string) (string, error) {
	pathOnly, query, hasQuery := strings.Cut(pathTemplate, "?")
	segments := strings.Split(pathOnly, "/")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "{{") || !strings.HasSuffix(segment, "}}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{{"), "}}")
		value, ok := variables[name]
		if !ok {
			return "", fmt.Errorf("missing variable %q", name)
		}
		segments[i] = url.PathEscape(value)
	}
	resolvedPath := strings.Join(segments, "/")
	if hasQuery {
		if strings.Contains(query, "{{") {
			values, err := url.ParseQuery(query)
			if err != nil {
				return "", fmt.Errorf("parse query template: %w", err)
			}
			for key, items := range values {
				if strings.Contains(key, "{{") || strings.Contains(key, "}}") {
					return "", fmt.Errorf("query templates are supported only in parameter values")
				}
				for i, item := range items {
					values[key][i], err = interpolateChecked(item, variables)
					if err != nil {
						return "", err
					}
				}
			}
			query = values.Encode()
		}
		resolvedPath += "?" + query
	}
	parsed, err := url.Parse(resolvedPath)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func interpolateChecked(value string, variables map[string]string) (string, error) {
	var missing string
	result := variablePattern.ReplaceAllStringFunc(value, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		resolved, ok := variables[name]
		if !ok {
			missing = name
			return match
		}
		return resolved
	})
	if missing != "" {
		return "", fmt.Errorf("missing variable %q", missing)
	}
	return result, nil
}

func interpolate(value string, variables map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(value, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		return variables[name]
	})
}

func extractValues(extracts map[string]config.Extract, headers http.Header, body []byte) (map[string]string, error) {
	values := map[string]string{}
	if len(extracts) == 0 {
		return values, nil
	}
	var document any
	for name, extract := range extracts {
		if extract.Header != "" {
			value := headers.Get(extract.Header)
			if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\r\n") {
				return nil, fmt.Errorf("header extraction %q failed", name)
			}
			values[name] = value
			continue
		}
		if document == nil {
			var err error
			document, err = decodeJSONDocument(body)
			if err != nil {
				return nil, fmt.Errorf("decode JSON response: %w", err)
			}
		}
		value, ok := jsonPointer(document, extract.JSONPointer)
		if !ok || value == "" || len(value) > 8192 {
			return nil, fmt.Errorf("JSON pointer extraction %q failed", name)
		}
		values[name] = value
	}
	return values, nil
}

func jsonPointer(document any, pointer string) (string, bool) {
	current := document
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = node[part]
			if !ok {
				return "", false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return "", false
			}
			current = node[index]
		default:
			return "", false
		}
	}
	switch value := current.(type) {
	case string:
		return value, true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case json.Number:
		return value.String(), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return "", false
	}
}
func safePath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}
func round(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }
