// Package engine runs bounded, closed-loop HTTP workloads. It does not infer universal capacity.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const maxResponseBytes = 2 << 20

type sample struct {
	name      string
	latency   time.Duration
	success   bool
	cancelled bool
	reason    string
}

type collector struct {
	mu        sync.Mutex
	aggregate *report.MetricSet
	steps     map[string]*report.MetricSet
}

func newCollector(v config.Validated) *collector {
	c := &collector{aggregate: report.NewMetricSet(), steps: make(map[string]*report.MetricSet, len(v.Steps))}
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
	ctx, cancel := context.WithTimeout(parent, v.Duration)
	defer cancel()
	coll := newCollector(v)
	// The ticker imposes a process-wide start rate ceiling, even with many virtual users.
	ticker := time.NewTicker(time.Second / time.Duration(v.Scenario.MaxRPS))
	defer ticker.Stop()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = v.Scenario.Users * 3
	tr.MaxIdleConnsPerHost = v.Scenario.Users * 2
	tr.MaxConnsPerHost = v.Scenario.Users + 4
	tr.ResponseHeaderTimeout = v.Timeout
	tr.IdleConnTimeout = 15 * time.Second
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	var wg sync.WaitGroup
	for i := 0; i < v.Scenario.Users; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			delay := time.Duration(index) * v.RampUp / time.Duration(v.Scenario.Users)
			if !sleep(ctx, delay) {
				return
			}
			for {
				for _, step := range v.Steps {
					if !waitToken(ctx, ticker.C) {
						return
					}
					sample := execute(ctx, client, v.Timeout, step)
					coll.add(sample)
					if sample.cancelled {
						return
					}
					if !sleep(ctx, v.ThinkTime) {
						return
					}
				}
			}
		}(i)
	}
	wg.Wait()
	coll.mu.Lock()
	aggregate := coll.aggregate.Snapshot()
	steps := make([]report.StepResult, 0, len(v.Steps))
	for _, st := range v.Steps {
		steps = append(steps, report.StepResult{Name: st.Name, Method: st.Method, Path: safePath(st.Path), Metrics: coll.steps[st.Name].Snapshot()})
	}
	coll.mu.Unlock()
	elapsed := time.Since(started)
	thresholdErrors := 0.0
	if aggregate.Total > 0 {
		thresholdErrors = float64(aggregate.Failures) / float64(aggregate.Total)
	}
	passed := aggregate.Total > 0 && aggregate.Success > 0 && thresholdErrors <= v.Scenario.MaxErrorRate && aggregate.P95MS <= v.Scenario.P95LimitMS
	result := report.Result{
		SchemaVersion: 1, Name: v.Scenario.Name, Target: v.Base.Scheme + "://" + v.Base.Host,
		StartedAt: started.UTC().Format(time.RFC3339), ElapsedSeconds: round(elapsed.Seconds()),
		Config:  report.RunConfig{Users: v.Scenario.Users, Duration: v.Scenario.Duration, RampUp: v.Scenario.RampUp, ThinkTime: v.Scenario.ThinkTime, RequestTimeout: v.Timeout.String(), MaxRPS: v.Scenario.MaxRPS, P95LimitMS: v.Scenario.P95LimitMS, MaxErrorRate: v.Scenario.MaxErrorRate},
		Metrics: aggregate, Steps: steps, Outcome: "FAIL", ErrorsRate: round(thresholdErrors),
		Note: "A single closed-loop run is not a validated sustainable-capacity limit. VUs represent modeled actors, not all connected sessions.",
	}
	if passed {
		result.Outcome = "PASS"
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Outcome = "ABORTED"
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
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
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
	for key, value := range s.ResolvedHeaders {
		req.Header.Set(key, value)
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
	duration := time.Since(start)
	if parent.Err() != nil {
		return sample{name: s.Name, latency: duration, cancelled: true, reason: "run_cancelled"}
	}
	if readErr != nil {
		if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return sample{name: s.Name, latency: duration, reason: "timeout"}
		}
		return sample{name: s.Name, latency: duration, reason: "body_read"}
	}
	if len(data) > maxResponseBytes {
		return sample{name: s.Name, latency: duration, reason: "response_too_large"}
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
		return sample{name: s.Name, latency: duration, reason: fmt.Sprintf("http_%d", res.StatusCode)}
	}
	if s.MustContain != "" && !bytes.Contains(data, []byte(s.MustContain)) {
		return sample{name: s.Name, latency: duration, reason: "assertion_missing_text"}
	}
	return sample{name: s.Name, latency: duration, success: true}
}
func safePath(p string) string {
	for i, ch := range p {
		if ch == '?' {
			return p[:i]
		}
	}
	return p
}
func round(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }
