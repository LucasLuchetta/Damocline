// Package engine implements Damocline's own bounded HTTP scheduling engine.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/scheduler"
)

const maxResponseBytes = 2 << 20

type sample struct {
	name               string
	latency            time.Duration
	success, cancelled bool
	reason             string
}
type collector struct {
	mu        sync.Mutex
	aggregate *report.MetricSet
	steps     map[string]*report.MetricSet
	journeys  *report.MetricSet
}

func newCollector(v config.Validated) *collector {
	c := &collector{aggregate: report.NewMetricSet(),
		journeys: report.NewMetricSet(), steps: make(map[string]*report.MetricSet, len(v.Steps))}
	for _, s := range v.Steps {
		c.steps[s.Name] = report.NewMetricSet()
	}
	return c
}
func (c *collector) add(s sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aggregate.Record(s.latency, s.success, s.cancelled, s.reason)
	c.steps[s.name].Record(s.latency, s.success, s.cancelled, s.reason)
}

func Run(parent context.Context, v config.Validated) report.Result {
	started := time.Now()
	generatorMonitor := generatorhealth.Start(250 * time.Millisecond)
	// Stop admitting new work at the configured boundary, then allow any
	// already-started journey a bounded time to finish.
	drainAllowance := min(30*time.Second, v.Timeout*time.Duration(len(v.Steps))+v.ThinkTime*time.Duration(len(v.Steps)))
	ctx, cancel := context.WithTimeout(parent, v.Duration+drainAllowance)
	defer cancel()
	scheduleCtx, cancelSchedule := context.WithTimeout(parent, v.Duration)
	defer cancelSchedule()
	coll := newCollector(v)
	ticker := time.NewTicker(time.Second / time.Duration(v.Scenario.MaxRPS))
	defer ticker.Stop()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = v.Scenario.Users * 3
	tr.MaxIdleConnsPerHost = v.Scenario.Users * 2
	tr.MaxConnsPerHost = v.Scenario.Users + 4
	tr.ResponseHeaderTimeout = v.Timeout
	tr.IdleConnTimeout = 15 * time.Second
	defer tr.CloseIdleConnections()
	var startedIterations, completedIterations, scheduledIterations, droppedIterations atomic.Int64
	var wg sync.WaitGroup
	var arrivalStats scheduler.Stats
	clients := make([]*http.Client, v.Scenario.Users)
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
	iteration := func(client *http.Client) {
		startedIterations.Add(1)
		journeyStart := time.Now()
		journeyOk := true
		fullyCompleted := true
		for stepIndex, step := range v.Steps {
			if !waitToken(ctx, ticker.C) {
				fullyCompleted = false
				break
			}
			s := execute(ctx, client, v.Timeout, step)
			coll.add(s)
			if !s.success {
				journeyOk = false
			}
			if s.cancelled {
				fullyCompleted = false
				break
			}
			if stepIndex < len(v.Steps)-1 && !sleep(ctx, v.ThinkTime) {
				fullyCompleted = false
				break
			}
		}
		if fullyCompleted {
			completedIterations.Add(1)
			coll.mu.Lock()
			coll.journeys.Record(time.Since(journeyStart), journeyOk, false, func() string {
				if journeyOk {
					return ""
				}
				return "journey_step_failed"
			}())
			coll.mu.Unlock()
		} else {
			coll.mu.Lock()
			coll.journeys.Record(time.Since(journeyStart), false, true, "journey_cancelled")
			coll.mu.Unlock()
		}
	}
	if v.Scenario.Mode == "arrival" {
		arrivals := make(chan struct{}) // unbuffered: busy workers cannot hide dropped arrivals
		for i := 0; i < v.Scenario.Users; i++ {
			wg.Add(1)
			go func(client *http.Client) {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case _, ok := <-arrivals:
						if !ok {
							return
						}
						iteration(client)
					}
				}
			}(clients[i])
		}
		arrivalStats = scheduler.Drive(scheduleCtx, v.Duration, v.Scenario.ArrivalRate, func() bool {
			select {
			case arrivals <- struct{}{}:
				return true
			default:
				return false
			}
		})
		scheduledIterations.Store(arrivalStats.Planned)
		droppedIterations.Store(arrivalStats.Dropped())
		close(arrivals)
	} else {
		for i := 0; i < v.Scenario.Users; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				delay := time.Duration(index) * v.RampUp / time.Duration(v.Scenario.Users)
				if !sleep(scheduleCtx, delay) {
					return
				}
				for scheduleCtx.Err() == nil {
					scheduledIterations.Add(1)
					iteration(clients[index])
				}
			}(i)
		}
	}
	wg.Wait()
	generatorSnapshot := generatorMonitor.Stop()
	coll.mu.Lock()
	agg := coll.aggregate.Snapshot()
	journeyMetrics := coll.journeys.Snapshot()
	steps := make([]report.StepResult, 0, len(v.Steps))
	for _, st := range v.Steps {
		steps = append(steps, report.StepResult{Name: st.Name, Method: st.Method, Path: safePath(st.Path), Metrics: coll.steps[st.Name].Snapshot()})
	}
	coll.mu.Unlock()
	elapsed := time.Since(started)
	errorsRate := 0.0
	if agg.Total > 0 {
		errorsRate = float64(agg.Failures) / float64(agg.Total)
	}
	result := report.Result{SchemaVersion: 4, Name: v.Scenario.Name, Target: v.Base.Scheme + "://" + v.Base.Host, StartedAt: started.UTC().Format(time.RFC3339), ElapsedSeconds: round(elapsed.Seconds()), ObservationSeconds: round(v.Duration.Seconds()),
		Config:  report.RunConfig{Users: v.Scenario.Users, Duration: v.Scenario.Duration, RampUp: v.Scenario.RampUp, ThinkTime: v.Scenario.ThinkTime, RequestTimeout: v.Timeout.String(), MaxRPS: v.Scenario.MaxRPS, P95LimitMS: v.Scenario.P95LimitMS, MaxErrorRate: v.Scenario.MaxErrorRate, Mode: v.Scenario.Mode, ArrivalRate: v.Scenario.ArrivalRate, SeparateSessions: len(v.Cookies) > 0, MinSamplesPerStep: v.Scenario.MinSamplesPerStep},
		Metrics: agg, Journeys: journeyMetrics, Steps: steps, Outcome: "PASS", ErrorsRate: round(errorsRate), IterationsStarted: startedIterations.Load(), IterationsCompleted: completedIterations.Load(), ArrivalsScheduled: scheduledIterations.Load(), ArrivalsDropped: droppedIterations.Load(), ArrivalsDroppedLate: arrivalStats.DroppedLate, ArrivalsDroppedBusy: arrivalStats.DroppedBusy, SchedulerLagP95MS: float64(arrivalStats.P95Lag.Microseconds()) / 1000, SchedulerLagMaxMS: float64(arrivalStats.MaxLag.Microseconds()) / 1000,
		Note: "Measured synthetic HTTP actors under the configured workload; not a universal server capacity or a browser-user count.", Generator: &generatorSnapshot}
	if agg.Total == 0 || agg.Success == 0 || errorsRate > v.Scenario.MaxErrorRate || agg.P95MS > v.Scenario.P95LimitMS || result.ArrivalsDropped > 0 {
		result.Outcome = "FAIL"
	}
	for _, st := range steps {
		if st.Metrics.Total < v.Scenario.MinSamplesPerStep || st.Metrics.Success == 0 || st.Metrics.P95MS > v.Scenario.P95LimitMS || (st.Metrics.Total > 0 && float64(st.Metrics.Failures)/float64(st.Metrics.Total) > v.Scenario.MaxErrorRate) {
			result.Outcome = "FAIL"
		}
	}
	if arrivalStats.DroppedLate > 0 {
		result.Outcome = "INCONCLUSIVE"
	}
	if errors.Is(parent.Err(), context.Canceled) || errors.Is(parent.Err(), context.DeadlineExceeded) {
		result.Outcome = "ABORTED"
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
func execute(parent context.Context, client *http.Client, timeout time.Duration, s config.PreparedStep) sample {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var body io.Reader
	if len(s.Body) > 0 {
		body = bytes.NewReader(s.Body)
	}
	req, err := http.NewRequestWithContext(ctx, s.Method, s.URL, body)
	if err != nil {
		return sample{name: s.Name, reason: "request_build"}
	}
	for k, v := range s.ResolvedHeaders {
		req.Header.Set(k, v)
	}
	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		d := time.Since(start)
		if parent.Err() != nil {
			return sample{name: s.Name, latency: d, cancelled: true, reason: "run_cancelled"}
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return sample{name: s.Name, latency: d, reason: "timeout"}
		}
		return sample{name: s.Name, latency: d, reason: "transport"}
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	d := time.Since(start)
	if parent.Err() != nil {
		return sample{name: s.Name, latency: d, cancelled: true, reason: "run_cancelled"}
	}
	if readErr != nil {
		if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return sample{name: s.Name, latency: d, reason: "timeout"}
		}
		return sample{name: s.Name, latency: d, reason: "body_read"}
	}
	if len(data) > maxResponseBytes {
		return sample{name: s.Name, latency: d, reason: "response_too_large"}
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
		return sample{name: s.Name, latency: d, reason: fmt.Sprintf("http_%d", res.StatusCode)}
	}
	if s.MustContain != "" && !bytes.Contains(data, []byte(s.MustContain)) {
		return sample{name: s.Name, latency: d, reason: "assertion_missing_text"}
	}
	return sample{name: s.Name, latency: d, success: true}
}
func safePath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}
func round(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }
