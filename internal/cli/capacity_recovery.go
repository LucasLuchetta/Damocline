package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// Recovery gate bounds (ADR 0179). Before every step-down campaign the
// protocol waits a fixed cooldown and then sends the scenario's bounded,
// credential-free network-path probe until its median returns near the
// pre-load reference recorded by explore, or the bounded wait elapses.
const (
	capacityRecoveryCooldown = 30 * time.Second
	capacityRecoveryRetry    = 15 * time.Second
	capacityRecoveryMaxWait  = 3 * time.Minute
	capacityRecoveryRatio    = 2.0
	capacityRecoveryMarginMS = 25.0
)

const (
	capacityRecoveryRecovered    = "RECOVERED"
	capacityRecoveryNotRecovered = "NOT_RECOVERED"
	capacityRecoveryUnverified   = "UNVERIFIED"
	capacityOutcomeNotRecovered  = "NOT_RUN_TARGET_NOT_RECOVERED"
)

// capacityRecoveryCheck is the recorded outcome of one recovery gate.
type capacityRecoveryCheck struct {
	Status         string    `json:"status"`
	Cooldown       string    `json:"cooldown"`
	MaxWait        string    `json:"max_wait"`
	Waited         string    `json:"waited"`
	ReferenceP50MS float64   `json:"reference_p50_ms,omitempty"`
	LimitP50MS     float64   `json:"limit_p50_ms,omitempty"`
	Tolerance      string    `json:"tolerance"`
	ProbeP50MS     []float64 `json:"probe_p50_ms,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// Test hooks: production probes the real network path and sleeps on the wall
// clock; unit tests substitute deterministic fakes.
var (
	capacityRecoveryProbe = engine.ProbeNetworkPath
	capacityRecoverySleep = func(ctx context.Context, d time.Duration) bool {
		if d <= 0 {
			return ctx.Err() == nil
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}
)

// capacityRecoveryReference returns the median pre-load network-path p50
// recorded by the explore runs at the lowest probed level, falling back to
// all explore runs. Zero means no measured reference exists.
func capacityRecoveryReference(result explorer.Result) float64 {
	collect := func(match func(explorer.Probe) bool) []float64 {
		var values []float64
		for _, probe := range result.Probes {
			if !match(probe) {
				continue
			}
			for _, run := range probe.Runs {
				if b := run.NetworkBaseline; b != nil && b.Status == report.NetworkBaselineMeasured && b.Completed > 0 && b.P50MS > 0 {
					values = append(values, b.P50MS)
				}
			}
		}
		return values
	}
	lowest := 0
	for _, probe := range result.Probes {
		if probe.Users > 0 && (lowest == 0 || probe.Users < lowest) {
			lowest = probe.Users
		}
	}
	values := collect(func(p explorer.Probe) bool { return p.Users == lowest })
	if len(values) == 0 {
		values = collect(func(explorer.Probe) bool { return true })
	}
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return values[(len(values)-1)/2]
}

func capacityRecoveryLimit(reference float64) float64 {
	return math.Round(1000*(capacityRecoveryRatio*reference+capacityRecoveryMarginMS)) / 1000
}

// capacityRecoveryGate waits for the target to return near its pre-load
// network-path latency. It is bounded by capacityRecoveryMaxWait.
func capacityRecoveryGate(ctx context.Context, plan *config.NetworkBaselinePlan, reference float64) capacityRecoveryCheck {
	check := capacityRecoveryCheck{
		Cooldown: capacityRecoveryCooldown.String(), MaxWait: capacityRecoveryMaxWait.String(),
		Tolerance: fmt.Sprintf("probe p50 <= %.0fx reference p50 + %.0f ms", capacityRecoveryRatio, capacityRecoveryMarginMS),
	}
	waited := time.Duration(0)
	finish := func(status, reason string) capacityRecoveryCheck {
		check.Status, check.Reason, check.Waited = status, reason, waited.String()
		return check
	}
	if !capacityRecoverySleep(ctx, capacityRecoveryCooldown) {
		return finish(capacityRecoveryNotRecovered, "cooldown interrupted")
	}
	waited += capacityRecoveryCooldown
	if plan == nil || plan.SkipReason != "" || reference <= 0 {
		return finish(capacityRecoveryUnverified, "no measured network-path reference; only the fixed cooldown was applied, so carry-over from the previous campaign cannot be excluded")
	}
	check.ReferenceP50MS, check.LimitP50MS = reference, capacityRecoveryLimit(reference)
	for {
		start := time.Now()
		probe := capacityRecoveryProbe(ctx, plan)
		waited += min(time.Since(start), plan.WallBudget)
		if probe != nil && probe.Completed > 0 && probe.Failures == 0 && probe.P50MS > 0 {
			check.ProbeP50MS = append(check.ProbeP50MS, probe.P50MS)
			if probe.P50MS <= check.LimitP50MS {
				return finish(capacityRecoveryRecovered, "")
			}
		} else {
			check.ProbeP50MS = append(check.ProbeP50MS, 0)
		}
		if waited+capacityRecoveryRetry+plan.WallBudget > capacityRecoveryMaxWait {
			return finish(capacityRecoveryNotRecovered, fmt.Sprintf("target did not recover to within %s of its pre-load network-path latency in %s; later campaigns would be contaminated by carry-over", check.Tolerance, capacityRecoveryMaxWait))
		}
		if !capacityRecoverySleep(ctx, capacityRecoveryRetry) {
			return finish(capacityRecoveryNotRecovered, "recovery wait interrupted")
		}
		waited += capacityRecoveryRetry
	}
}
