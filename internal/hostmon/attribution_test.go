package hostmon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeStat writes a synthetic /proc stat record with the given parent,
// cumulative user+system ticks, thread count, start time and RSS pages.
func writeStat(t *testing.T, path, comm string, ppid int, utime, stime uint64, threads int, start uint64, rssPages int) {
	t.Helper()
	fields := make([]string, 22)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[1] = fmt.Sprint(ppid)
	fields[11], fields[12] = fmt.Sprint(utime), fmt.Sprint(stime)
	fields[17] = fmt.Sprint(threads)
	fields[19] = fmt.Sprint(start)
	fields[21] = fmt.Sprint(rssPages)
	writeFile(t, path, fmt.Sprintf("1 (%s) %s\n", comm, strings.Join(fields, " ")))
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fixtureProc struct {
	pid, ppid int
	comm      string
	start     uint64
	threads   map[int]uint64 // tid -> cumulative ticks
}

func writeTree(t *testing.T, root string, procs []fixtureProc) {
	t.Helper()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		total := uint64(0)
		for tid, ticks := range p.threads {
			total += ticks
			writeStat(t, filepath.Join(root, fmt.Sprint(p.pid), "task", fmt.Sprint(tid), "stat"), p.comm, p.ppid, ticks, 0, 1, p.start, 0)
		}
		writeStat(t, filepath.Join(root, fmt.Sprint(p.pid), "stat"), p.comm, p.ppid, total, 0, len(p.threads), p.start, 10)
	}
}

func cgroupFixture(t *testing.T, root, group string, usage, periods, throttled, throttledUsec, oom uint64) {
	t.Helper()
	writeFile(t, filepath.Join(root, "cgroup.controllers"), "cpu memory\n")
	dir := filepath.Join(root, filepath.FromSlash(group))
	writeFile(t, filepath.Join(dir, "cpu.stat"), fmt.Sprintf("usage_usec %d\nuser_usec 0\nsystem_usec 0\nnr_periods %d\nnr_throttled %d\nthrottled_usec %d\n", usage, periods, throttled, throttledUsec))
	writeFile(t, filepath.Join(dir, "cpu.max"), "200000 100000\n")
	writeFile(t, filepath.Join(dir, "memory.current"), "4096\n")
	writeFile(t, filepath.Join(dir, "memory.events"), fmt.Sprintf("low 0\nhigh 0\nmax 0\noom %d\noom_kill 0\n", oom))
}

func TestAttributionMeasuresChildWorkWhenInitProcessIsIdle(t *testing.T) {
	dir := t.TempDir()
	procRoot, cgroupRoot := filepath.Join(dir, "proc"), filepath.Join(dir, "cgroup")
	// An idle init process (PID 40) supervises an application child (41)
	// whose single busy thread does almost all the work; PID 99 is unrelated.
	writeTree(t, procRoot, []fixtureProc{
		{pid: 40, ppid: 1, comm: "init", start: 100, threads: map[int]uint64{40: 5}},
		{pid: 41, ppid: 40, comm: "app", start: 110, threads: map[int]uint64{41: 1000, 42: 10}},
		{pid: 99, ppid: 1, comm: "other", start: 50, threads: map[int]uint64{99: 0}},
	})
	writeFile(t, filepath.Join(procRoot, "40", "cgroup"), "0::/workload.slice/unit\n")
	cgroupFixture(t, cgroupRoot, "workload.slice/unit", 1_000_000, 10, 0, 0, 0)
	sampler := NewAttributionSampler(AttributionConfig{PID: 40, Descendants: true}, procRoot, cgroupRoot, 100, 4096)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sampler.Prime(at)

	writeTree(t, procRoot, []fixtureProc{
		{pid: 40, ppid: 1, comm: "init", start: 100, threads: map[int]uint64{40: 5}},
		{pid: 41, ppid: 40, comm: "app", start: 110, threads: map[int]uint64{41: 1098, 42: 12}},
		{pid: 99, ppid: 1, comm: "other", start: 50, threads: map[int]uint64{99: 400}},
	})
	writeFile(t, filepath.Join(procRoot, "40", "cgroup"), "0::/workload.slice/unit\n")
	cgroupFixture(t, cgroupRoot, "workload.slice/unit", 2_000_000, 20, 4, 50_000, 1)
	got := sampler.Sample(at.Add(time.Second))
	if err := ValidateAttribution(got); err != nil {
		t.Fatalf("sampled attribution failed validation: %v (%+v)", err, got)
	}
	if got.Scope != "process_tree_and_cgroup" || got.TreeCoverage != CoverageComplete || got.ProcessCount != 2 {
		t.Fatalf("unexpected tree coverage: %+v", got)
	}
	if got.TreeCPUCores == nil || abs(*got.TreeCPUCores-1.0) > 1e-9 {
		t.Fatalf("tree cores = %v, want 1.0 (unrelated process excluded)", got.TreeCPUCores)
	}
	if len(got.TopProcesses) != 2 || got.TopProcesses[0].PID != 41 || abs(got.TopProcesses[0].CPUCores-1.0) > 1e-9 || got.TopProcesses[1].CPUCores != 0 {
		t.Fatalf("unexpected top processes: %+v", got.TopProcesses)
	}
	if got.BusiestThread == nil || got.BusiestThread.TID != 41 || abs(got.BusiestThread.CPUCores-0.98) > 1e-9 || got.ThreadCoverage != CoverageComplete {
		t.Fatalf("unexpected busiest thread: %+v coverage=%s", got.BusiestThread, got.ThreadCoverage)
	}
	if got.TreeRSSBytes == nil || *got.TreeRSSBytes != 2*10*4096 {
		t.Fatalf("unexpected tree RSS: %v", got.TreeRSSBytes)
	}
	if got.CgroupCoverage != CoverageComplete || got.CgroupCPUCores == nil || abs(*got.CgroupCPUCores-1.0) > 1e-9 || got.CgroupQuotaCores == nil || *got.CgroupQuotaCores != 2 {
		t.Fatalf("unexpected cgroup CPU: %+v", got)
	}
	if *got.CgroupPeriodsDelta != 10 || *got.CgroupThrottledPeriodsDelta != 4 || *got.CgroupThrottledUsecDelta != 50_000 || got.CgroupMemoryEventsDelta["oom"] != 1 {
		t.Fatalf("unexpected cgroup throttling or memory events: %+v", got)
	}
}

func TestAttributionFailsClosedOnRestartMembershipChangeAndMissingCgroup(t *testing.T) {
	dir := t.TempDir()
	procRoot, cgroupRoot := filepath.Join(dir, "proc"), filepath.Join(dir, "cgroup")
	writeTree(t, procRoot, []fixtureProc{{pid: 40, ppid: 1, comm: "init", start: 100, threads: map[int]uint64{40: 5}}})
	sampler := NewAttributionSampler(AttributionConfig{PID: 40, Descendants: true, CgroupPath: "missing"}, procRoot, cgroupRoot, 100, 4096)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := sampler.Sample(at)
	if first.TreeCoverage != CoverageIncomplete || first.TreeCPUCores != nil || first.CgroupCoverage != CoverageUnavailable {
		t.Fatalf("first sample must be incomplete and cgroup unavailable: %+v", first)
	}
	writeTree(t, procRoot, []fixtureProc{
		{pid: 40, ppid: 1, comm: "init", start: 100, threads: map[int]uint64{40: 5}},
		{pid: 41, ppid: 40, comm: "app", start: 120, threads: map[int]uint64{41: 50}},
	})
	changed := sampler.Sample(at.Add(time.Second))
	if changed.TreeCoverage != CoverageIncomplete || changed.TreeCPUCores != nil || !strings.Contains(changed.TreeReason, "membership") {
		t.Fatalf("membership change accepted: %+v", changed)
	}
	writeTree(t, procRoot, []fixtureProc{{pid: 40, ppid: 1, comm: "init", start: 200, threads: map[int]uint64{40: 5}}})
	restarted := sampler.Sample(at.Add(2 * time.Second))
	if restarted.TreeCoverage != CoverageIncomplete || !strings.Contains(restarted.TreeReason, "restarted") {
		t.Fatalf("restart accepted: %+v", restarted)
	}
	if err := ValidateAttribution(restarted); err != nil {
		t.Fatal(err)
	}
	gone := NewAttributionSampler(AttributionConfig{PID: 77, Descendants: true}, procRoot, cgroupRoot, 100, 4096).Sample(at)
	if gone.TreeCoverage != CoverageUnavailable || gone.CgroupCoverage != CoverageUnavailable {
		t.Fatalf("missing root must be unavailable: %+v", gone)
	}
}

func TestValidateAttributionConfigAndEvidence(t *testing.T) {
	for _, bad := range []AttributionConfig{{Descendants: true}, {PID: -1}, {CgroupPath: "../etc"}, {CgroupPath: "a/../b"}, {CgroupPath: "a b"}, {CgroupPath: "/"}} {
		if ValidateAttributionConfig(bad) == nil {
			t.Errorf("accepted unsafe config %+v", bad)
		}
	}
	if err := ValidateAttributionConfig(AttributionConfig{CgroupPath: "/system.slice/app.service"}); err != nil {
		t.Fatal(err)
	}
	one := 1.0
	periods, throttled := uint64(1), uint64(2)
	for _, bad := range []*Attribution{
		{SchemaVersion: 2, Scope: "cgroup", TreeCoverage: CoverageNotConfigured, CgroupCoverage: CoverageNotConfigured},
		{SchemaVersion: 1, Scope: "process_tree", TreeCoverage: CoverageIncomplete, TreeCPUCores: &one, CgroupCoverage: CoverageNotConfigured},
		{SchemaVersion: 1, Scope: "process_tree", TreeCoverage: CoverageComplete, CgroupCoverage: CoverageNotConfigured, IntervalMS: 1000},
		{SchemaVersion: 1, Scope: "cgroup", TreeCoverage: CoverageNotConfigured, CgroupCoverage: CoverageComplete, CgroupCPUCores: &one, CgroupCPUUsageUsec: &periods, IntervalMS: 1000, CgroupPeriodsDelta: &periods, CgroupThrottledPeriodsDelta: &throttled, CgroupThrottledUsecDelta: &periods},
		{SchemaVersion: 1, Scope: "cgroup", TreeCoverage: CoverageComplete, TreeCPUCores: &one, CgroupCoverage: CoverageUnavailable, IntervalMS: 1000},
	} {
		if ValidateAttribution(bad) == nil {
			t.Errorf("accepted invalid attribution %+v", bad)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
