package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestDueCountReconcilesCoalescedWakeups(t *testing.T) {
	period, duration := 50*time.Millisecond, time.Second
	cases := []struct {
		time time.Duration
		want int64
	}{
		{0, 0}, {24 * time.Millisecond, 0}, {25 * time.Millisecond, 1},
		{74 * time.Millisecond, 1}, {75 * time.Millisecond, 2},
		{510 * time.Millisecond, 10}, {975 * time.Millisecond, 20},
		{time.Second, 20}, {2 * time.Second, 20},
	}
	for _, x := range cases {
		if got := DueCount(x.time, period, duration); got != x.want {
			t.Fatalf("at %v planned %d; want %d", x.time, got, x.want)
		}
	}
}
func TestDriveEveryArrivalHasAnOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 320*time.Millisecond)
	defer cancel()
	s := Drive(ctx, 250*time.Millisecond, 20, func() bool { return false })
	if !s.Consistent() || s.Planned != 5 || s.Dropped() != 5 || s.Admitted != 0 {
		t.Fatalf("scheduled events vanished: %+v", s)
	}
}
func TestDriveAdmitsAllOnAvailableGenerator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 320*time.Millisecond)
	defer cancel()
	s := Drive(ctx, 250*time.Millisecond, 20, func() bool { return true })
	if !s.Consistent() || s.Planned != 5 || s.Admitted != 5 || s.Dropped() != 0 {
		t.Fatalf("unexpected generator underdelivery: %+v", s)
	}
}
func TestDriveAbortBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Drive(ctx, time.Second, 20, func() bool { return true })
	if s.Planned != 0 || !s.Consistent() {
		t.Fatalf("unexpected traffic: %+v", s)
	}
}

func TestDriveAccountsForSchedulerStall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	const injectedPause = 130 * time.Millisecond
	s := Drive(ctx, 500*time.Millisecond, 20, func() bool {
		calls++
		if calls == 1 {
			// Fault-inject a delayed callback to verify that late arrivals are
			// counted rather than silently erased after a scheduler stall.
			time.Sleep(injectedPause)
		}
		return true
	})
	if !s.Consistent() || s.Planned != 10 || s.DroppedLate == 0 || s.LagSamples != s.Planned || s.P95Lag <= 0 || s.MaxLag < injectedPause/3 {
		t.Fatalf("scheduler stall was not reconciled as late arrivals: %+v", s)
	}
}

func TestDriveDoesNotAdmitAfterObservationBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	const duration = 250 * time.Millisecond
	started := time.Now()
	var admitted []time.Time
	s := Drive(ctx, duration, 20, func() bool {
		admitted = append(admitted, time.Now())
		return true
	})
	if !s.Consistent() || s.Planned != 5 || s.Admitted+s.DroppedLate != s.Planned || s.DroppedBusy != 0 {
		t.Fatalf("boundary arrivals did not reconcile: %+v", s)
	}
	for _, at := range admitted {
		if at.Sub(started) >= duration {
			t.Fatalf("admission occurred after observation boundary: %v", at.Sub(started))
		}
	}
}

func TestDriveSeparatesExternalCancellationFromSchedulerLateness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstAdmission := make(chan struct{})
	done := make(chan Stats, 1)
	go func() {
		done <- Drive(ctx, time.Second, 20, func() bool {
			select {
			case <-firstAdmission:
			default:
				close(firstAdmission)
				time.Sleep(140 * time.Millisecond)
			}
			return true
		})
	}()
	<-firstAdmission
	time.Sleep(80 * time.Millisecond)
	cancel()
	stats := <-done
	if !stats.Consistent() || stats.Planned != 3 || stats.Admitted != 1 ||
		stats.CancelledBeforeAdmission != 2 || stats.DroppedLate != 0 || stats.DroppedBusy != 0 {
		t.Fatalf("external termination was conflated with scheduler lateness: %+v", stats)
	}
}

func TestFinishSummarizesEveryPlannedLagSample(t *testing.T) {
	var stats Stats
	for _, lag := range []time.Duration{40, 10, 30, 20} {
		stats.Planned++
		stats.observe(lag * time.Millisecond)
	}
	got := stats.finish()
	if got.LagSamples != 4 || got.P50Lag != 20*time.Millisecond ||
		got.P95Lag != 40*time.Millisecond || got.MaxLag != 40*time.Millisecond {
		t.Fatalf("unexpected lag summary: %+v", got)
	}
}
