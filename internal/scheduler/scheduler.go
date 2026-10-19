// Package scheduler accounts for offered workload independently of timer wakeups.
package scheduler

import (
	"context"
	"sort"
	"time"
)

// Stats reconciles all planned arrivals, including generator delays and VU shortage.
type Stats struct {
	Planned                  int64
	Admitted                 int64
	DroppedBusy              int64
	DroppedLate              int64
	CancelledBeforeAdmission int64
	LagSamples               int64
	P50Lag                   time.Duration
	P95Lag                   time.Duration
	MaxLag                   time.Duration
	lags                     []time.Duration
}

func (s Stats) Dropped() int64 {
	return s.DroppedBusy + s.DroppedLate + s.CancelledBeforeAdmission
}
func (s Stats) Consistent() bool { return s.Planned == s.Admitted+s.Dropped() }

// DueCount computes the planned arrival count from monotonic elapsed time.
// Arrival slots are centered within each period, excluding the test boundary.
func DueCount(elapsed, interval, duration time.Duration) int64 {
	if interval <= 0 || duration <= 0 {
		return 0
	}
	first := interval / 2
	if elapsed < first {
		return 0
	}
	max := int64((duration-first-time.Nanosecond)/interval) + 1
	due := int64((elapsed-first)/interval) + 1
	if due > max {
		return max
	}
	return due
}
func (s *Stats) observe(lag time.Duration) {
	if lag < 0 {
		lag = 0
	}
	if lag > s.MaxLag {
		s.MaxLag = lag
	}
	s.lags = append(s.lags, lag)
}
func (s *Stats) finish() Stats {
	if len(s.lags) > 0 {
		sort.Slice(s.lags, func(i, j int) bool { return s.lags[i] < s.lags[j] })
		s.LagSamples = int64(len(s.lags))
		p50Index := (50*len(s.lags)+99)/100 - 1
		p95Index := (95*len(s.lags)+99)/100 - 1
		s.P50Lag = s.lags[p50Index]
		s.P95Lag = s.lags[p95Index]
	}
	s.lags = nil
	return *s
}

// Drive is a bounded, single-scheduler workload generator. The callback must be
// non-blocking. Missed deadlines are reported, not silently omitted by Go timers.
// The validated scenario limits bound retained samples to at most 100 arrivals/s * 3 min.
func Drive(ctx context.Context, duration time.Duration, rate int, admit func() bool) Stats {
	var stats Stats
	if duration <= 0 || rate <= 0 || admit == nil {
		return stats
	}
	interval := time.Second / time.Duration(rate)
	if interval <= 0 {
		return stats
	}
	start := time.Now()
	first := interval / 2
	maximum := DueCount(duration, interval, duration)
	next := int64(0)
	timer := time.NewTimer(first)
	defer timer.Stop()
	account := func(now time.Time, stopped bool) {
		due := DueCount(now.Sub(start), interval, duration)
		for next < due {
			intended := start.Add(first + time.Duration(next)*interval)
			lag := now.Sub(intended)
			stats.Planned++
			stats.observe(lag)
			elapsed := now.Sub(start)
			if stopped && elapsed < duration {
				stats.CancelledBeforeAdmission++
			} else if elapsed >= duration || lag >= interval {
				stats.DroppedLate++
			} else if admit() {
				stats.Admitted++
			} else {
				stats.DroppedBusy++
			}
			next++
		}
	}
	for {
		select {
		case <-ctx.Done():
			account(time.Now(), true)
			return stats.finish()
		case <-timer.C:
			account(time.Now(), ctx.Err() != nil)
		}
		if next >= maximum {
			// Preserve the full observation interval before closing the workers.
			remaining := time.Until(start.Add(duration))
			if remaining > 0 {
				drain := time.NewTimer(remaining)
				select {
				case <-ctx.Done():
					drain.Stop()
				case <-drain.C:
				}
			}
			return stats.finish()
		}
		wait := time.Until(start.Add(first + time.Duration(next)*interval))
		if wait < 0 {
			wait = 0
		}
		timer.Reset(wait)
	}
}
