package engine

import (
	"context"
	"io"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// maxNetworkBaselineBodyBytes bounds the response body drained per baseline
// request so a large page cannot turn the probe into a download test.
const maxNetworkBaselineBodyBytes = 64 << 10

// runNetworkBaseline sends the plan's fixed number of sequential GET/HEAD
// requests at a low fixed rate before load (ADR 0153). Requests carry no
// cookies, custom headers, or body. Latency is time to response headers on a
// dedicated keep-alive transport, so the first sample includes connection
// setup. Any HTTP response completes a round trip; transport errors and
// timeouts are failures. It never runs concurrently with the load phase.
func runNetworkBaseline(ctx context.Context, plan *config.NetworkBaselinePlan, transport *http.Transport) *report.NetworkBaseline {
	if plan == nil {
		return nil
	}
	if plan.SkipReason != "" {
		return &report.NetworkBaseline{Status: report.NetworkBaselineSkipped, SkipReason: plan.SkipReason}
	}
	out := &report.NetworkBaseline{Status: report.NetworkBaselineMeasured, Phase: "before_load", Source: plan.Source,
		Method: plan.Method, Path: safePath(plan.Path), Planned: plan.Requests,
		IntervalMS: ms(plan.Interval), TimeoutMS: ms(plan.Timeout)}
	tr := transport
	if tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		tr = tr.Clone()
	}
	tr.MaxConnsPerHost = 1
	tr.ResponseHeaderTimeout = plan.Timeout
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: plan.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	budget, cancel := context.WithTimeout(ctx, plan.WallBudget)
	defer cancel()
	latencies := make([]time.Duration, 0, plan.Requests)
	next := time.Now()
	for i := 0; i < plan.Requests; i++ {
		if i > 0 {
			if !sleep(budget, time.Until(next)) {
				out.NotAttempted = plan.Requests - i
				break
			}
		}
		next = time.Now().Add(plan.Interval)
		req, err := http.NewRequestWithContext(budget, plan.Method, plan.URL, nil)
		if err != nil {
			out.Failures++
			continue
		}
		begin := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			if budget.Err() != nil {
				out.NotAttempted = plan.Requests - i
				break
			}
			out.Failures++
			continue
		}
		latencies = append(latencies, time.Since(begin))
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			out.UnexpectedHTTPStatus++
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxNetworkBaselineBodyBytes))
		_ = resp.Body.Close()
	}
	out.Completed = len(latencies)
	if len(latencies) == 0 {
		return out
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	rank := func(p float64) time.Duration {
		index := int(math.Ceil(p*float64(len(latencies)))) - 1
		if index < 0 {
			index = 0
		}
		return latencies[index]
	}
	out.MinMS = round(ms(latencies[0]))
	out.P50MS = round(ms(rank(0.50)))
	out.P95MS = round(ms(rank(0.95)))
	out.MaxMS = round(ms(latencies[len(latencies)-1]))
	if out.P50MS > 0 {
		out.MaxToP50Ratio = math.Round(1000*out.MaxMS/out.P50MS) / 1000
	}
	return out
}
