package hostmon

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Workload attribution bounds (ADR 0172). Exceeding any bound marks the
// affected evidence TRUNCATED rather than silently sampling a subset.
const (
	AttributionSchemaVersion       = 1
	MaxAttributionScannedProcesses = 16384
	MaxAttributionTreeProcesses    = 512
	MaxAttributionThreads          = 4096
	MaxAttributionTopProcesses     = 8
	maxAttributionCommBytes        = 64
	maxAttributionCgroupPathBytes  = 512
)

// Attribution coverage states.
const (
	CoverageComplete      = "COMPLETE"
	CoverageIncomplete    = "INCOMPLETE"
	CoverageTruncated     = "TRUNCATED"
	CoverageUnavailable   = "UNAVAILABLE"
	CoverageNotConfigured = "NOT_CONFIGURED"
)

// AttributionConfig selects a workload as a process tree rooted at PID and/or
// a cgroup-v2 directory. CgroupPath is relative to the visible cgroup-v2 mount;
// when it is empty and PID is set, the PID's own cgroup is used.
type AttributionConfig struct {
	PID         int
	Descendants bool
	CgroupPath  string
}

// Enabled reports whether workload attribution was requested.
func (c AttributionConfig) Enabled() bool {
	return c.Descendants || c.CgroupPath != ""
}

// ValidateAttributionConfig rejects ambiguous or unsafe selections.
func ValidateAttributionConfig(c AttributionConfig) error {
	if c.PID < 0 {
		return fmt.Errorf("workload PID must not be negative")
	}
	if c.Descendants && c.PID == 0 {
		return fmt.Errorf("workload descendants require a workload PID")
	}
	if c.CgroupPath != "" {
		if _, err := cleanCgroupSelection(c.CgroupPath); err != nil {
			return err
		}
	}
	return nil
}

func cleanCgroupSelection(path string) (string, error) {
	if len(path) > maxAttributionCgroupPathBytes {
		return "", fmt.Errorf("workload cgroup path exceeds %d bytes", maxAttributionCgroupPathBytes)
	}
	for _, ch := range path {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("/._-:@", ch)) {
			return "", fmt.Errorf("workload cgroup path contains an unsupported character")
		}
	}
	clean := strings.Trim(path, "/")
	if clean == "" || hasTraversal(clean) || strings.Contains(clean, "//") {
		return "", fmt.Errorf("workload cgroup path must be a nonempty path below the cgroup-v2 mount without traversal")
	}
	return clean, nil
}

// ProcessUsage is one measured process within the selected tree.
type ProcessUsage struct {
	PID            int     `json:"pid"`
	StartTimeTicks uint64  `json:"process_start_time_ticks"`
	Comm           string  `json:"comm"`
	CPUCores       float64 `json:"cpu_cores"`
	RSSBytes       uint64  `json:"rss_bytes"`
	Threads        int64   `json:"threads"`
}

// ThreadUsage is the busiest comparable thread within the selected tree.
type ThreadUsage struct {
	PID      int     `json:"pid"`
	TID      int     `json:"tid"`
	Comm     string  `json:"comm"`
	CPUCores float64 `json:"cpu_cores"`
}

// Attribution is read-only evidence about a selected workload. Values that
// could not be measured remain nil; they are never reported as zero.
type Attribution struct {
	SchemaVersion int     `json:"schema_version"`
	Scope         string  `json:"scope"`
	IntervalMS    float64 `json:"interval_ms,omitempty"`

	TreeCoverage    string         `json:"process_tree_coverage"`
	TreeReason      string         `json:"process_tree_reason,omitempty"`
	RootPID         int            `json:"root_pid,omitempty"`
	ProcessCount    int            `json:"process_count,omitempty"`
	TreeCPUCores    *float64       `json:"process_tree_cpu_cores,omitempty"`
	TreeRSSBytes    *uint64        `json:"process_tree_rss_bytes,omitempty"`
	TopProcesses    []ProcessUsage `json:"top_processes,omitempty"`
	ThreadCoverage  string         `json:"thread_coverage,omitempty"`
	ThreadsCompared int            `json:"threads_compared,omitempty"`
	ThreadsChanged  int            `json:"threads_changed,omitempty"`
	BusiestThread   *ThreadUsage   `json:"busiest_thread,omitempty"`

	CgroupCoverage              string            `json:"cgroup_coverage"`
	CgroupReason                string            `json:"cgroup_reason,omitempty"`
	CgroupCPUUsageUsec          *uint64           `json:"cgroup_cpu_usage_usec,omitempty"`
	CgroupCPUCores              *float64          `json:"cgroup_cpu_cores,omitempty"`
	CgroupQuotaCores            *float64          `json:"cgroup_cpu_quota_cores,omitempty"`
	CgroupPeriodsDelta          *uint64           `json:"cgroup_cpu_periods_delta,omitempty"`
	CgroupThrottledPeriodsDelta *uint64           `json:"cgroup_cpu_throttled_periods_delta,omitempty"`
	CgroupThrottledUsecDelta    *uint64           `json:"cgroup_cpu_throttled_usec_delta,omitempty"`
	CgroupMemoryCurrentBytes    *uint64           `json:"cgroup_memory_current_bytes,omitempty"`
	CgroupMemoryEventsDelta     map[string]uint64 `json:"cgroup_memory_events_delta,omitempty"`
}

type procKey struct {
	pid   int
	start uint64
}

type taskKey struct {
	pid, tid int
	start    uint64
}

type procSnap struct {
	comm    string
	ticks   uint64
	rss     uint64
	threads int64
}

type threadSnap struct {
	comm  string
	ticks uint64
}

type cgroupSnap struct {
	path                              string
	usage                             uint64
	periods, throttled, throttledUsec uint64
	hasThrottle                       bool
	events                            map[string]uint64
	current                           *uint64
	quotaCores                        *float64
}

type attributionSnapshot struct {
	at               time.Time
	treeErr          string
	treeTruncated    bool
	rootStart        uint64
	procs            map[procKey]procSnap
	threads          map[taskKey]threadSnap
	threadsTruncated bool
	cgroup           *cgroupSnap
	cgroupErr        string
	cgroupState      string
}

// AttributionSampler computes interval deltas between successive bounded
// snapshots. It only reads files below procRoot and cgroupRoot.
type AttributionSampler struct {
	cfg        AttributionConfig
	procRoot   string
	cgroupRoot string
	clockTicks uint64
	pageSize   uint64
	prev       *attributionSnapshot
}

// NewAttributionSampler builds a sampler over explicit roots (tests use
// synthetic fixtures). clockTicks and pageSize must be positive.
func NewAttributionSampler(cfg AttributionConfig, procRoot, cgroupRoot string, clockTicks, pageSize uint64) *AttributionSampler {
	if cfg.CgroupPath != "" {
		if clean, err := cleanCgroupSelection(cfg.CgroupPath); err == nil {
			cfg.CgroupPath = clean
		}
	}
	return &AttributionSampler{cfg: cfg, procRoot: procRoot, cgroupRoot: cgroupRoot, clockTicks: clockTicks, pageSize: pageSize}
}

// NativeAttributionSampler reads the live /proc and /sys/fs/cgroup mounts.
func NativeAttributionSampler(cfg AttributionConfig) *AttributionSampler {
	ticks, _ := readClockTicks()
	return NewAttributionSampler(cfg, "/proc", "/sys/fs/cgroup", ticks, uint64(syscall.Getpagesize()))
}

func (s *AttributionSampler) scope() string {
	switch {
	case s.cfg.Descendants && s.cgroupConfigured():
		return "process_tree_and_cgroup"
	case s.cfg.Descendants:
		return "process_tree"
	default:
		return "cgroup"
	}
}

func (s *AttributionSampler) cgroupConfigured() bool {
	return s.cfg.CgroupPath != "" || s.cfg.PID > 0
}

// Prime records a baseline snapshot without producing evidence.
func (s *AttributionSampler) Prime(at time.Time) {
	s.prev = s.snapshot(at)
}

// Sample takes a snapshot at the given time and returns interval evidence
// relative to the previous snapshot.
func (s *AttributionSampler) Sample(at time.Time) *Attribution {
	current := s.snapshot(at)
	previous := s.prev
	s.prev = current
	result := &Attribution{SchemaVersion: AttributionSchemaVersion, Scope: s.scope(), TreeCoverage: CoverageNotConfigured, CgroupCoverage: CoverageNotConfigured}
	var elapsed time.Duration
	if previous != nil {
		elapsed = current.at.Sub(previous.at)
		if elapsed > 0 {
			result.IntervalMS = float64(elapsed) / float64(time.Millisecond)
		}
	}
	if s.cfg.Descendants {
		s.fillTree(result, previous, current, elapsed)
	}
	if s.cgroupConfigured() {
		s.fillCgroup(result, previous, current, elapsed)
	}
	return result
}

func (s *AttributionSampler) fillTree(result *Attribution, previous, current *attributionSnapshot, elapsed time.Duration) {
	result.RootPID = s.cfg.PID
	if current.treeErr != "" {
		result.TreeCoverage, result.TreeReason = CoverageUnavailable, current.treeErr
		return
	}
	result.ProcessCount = len(current.procs)
	if !current.treeTruncated {
		var sum uint64
		overflow := false
		for _, p := range current.procs {
			if sum > math.MaxUint64-p.rss {
				overflow = true
				break
			}
			sum += p.rss
		}
		if !overflow {
			result.TreeRSSBytes = &sum
		}
	}
	switch {
	case current.treeTruncated:
		result.TreeCoverage, result.TreeReason = CoverageTruncated, fmt.Sprintf("process tree exceeds %d processes or the process scan exceeds %d entries", MaxAttributionTreeProcesses, MaxAttributionScannedProcesses)
	case previous == nil || elapsed <= 0:
		result.TreeCoverage, result.TreeReason = CoverageIncomplete, "no earlier comparable snapshot"
	case previous.treeErr != "" || previous.treeTruncated:
		result.TreeCoverage, result.TreeReason = CoverageIncomplete, "earlier snapshot was not comparable"
	case previous.rootStart != current.rootStart:
		result.TreeCoverage, result.TreeReason = CoverageIncomplete, "workload root process restarted or its PID was reused"
	case !sameProcessSet(previous.procs, current.procs):
		result.TreeCoverage, result.TreeReason = CoverageIncomplete, "process tree membership changed during the interval"
	case s.clockTicks == 0:
		result.TreeCoverage, result.TreeReason = CoverageIncomplete, "kernel clock tick rate is unavailable"
	default:
		result.TreeCoverage = CoverageComplete
	}
	if previous == nil || elapsed <= 0 || s.clockTicks == 0 || previous.treeErr != "" {
		return
	}
	seconds := elapsed.Seconds()
	tick := float64(s.clockTicks)
	usages := make([]ProcessUsage, 0, len(current.procs))
	total := 0.0
	for key, now := range current.procs {
		before, ok := previous.procs[key]
		if !ok || now.ticks < before.ticks {
			continue
		}
		cores := float64(now.ticks-before.ticks) / tick / seconds
		total += cores
		usages = append(usages, ProcessUsage{PID: key.pid, StartTimeTicks: key.start, Comm: now.comm, CPUCores: cores, RSSBytes: now.rss, Threads: now.threads})
	}
	sort.Slice(usages, func(i, j int) bool {
		if usages[i].CPUCores != usages[j].CPUCores {
			return usages[i].CPUCores > usages[j].CPUCores
		}
		return usages[i].PID < usages[j].PID
	})
	if len(usages) > MaxAttributionTopProcesses {
		usages = usages[:MaxAttributionTopProcesses]
	}
	result.TopProcesses = usages
	if result.TreeCoverage == CoverageComplete {
		result.TreeCPUCores = &total
	}
	var busiest *ThreadUsage
	compared, changed := 0, 0
	for key, now := range current.threads {
		before, ok := previous.threads[key]
		if !ok || now.ticks < before.ticks {
			changed++
			continue
		}
		compared++
		cores := float64(now.ticks-before.ticks) / tick / seconds
		if busiest == nil || cores > busiest.CPUCores || cores == busiest.CPUCores && (key.pid < busiest.PID || key.pid == busiest.PID && key.tid < busiest.TID) {
			busiest = &ThreadUsage{PID: key.pid, TID: key.tid, Comm: now.comm, CPUCores: cores}
		}
	}
	for key := range previous.threads {
		if _, ok := current.threads[key]; !ok {
			changed++
		}
	}
	result.ThreadsCompared, result.ThreadsChanged, result.BusiestThread = compared, changed, busiest
	switch {
	case current.threadsTruncated || previous.threadsTruncated:
		result.ThreadCoverage = CoverageTruncated
	case compared == 0:
		result.ThreadCoverage = CoverageUnavailable
	case changed > 0:
		// Threads that started or exited within the interval are not compared,
		// so the busiest thread is a lower bound.
		result.ThreadCoverage = CoverageIncomplete
	default:
		result.ThreadCoverage = CoverageComplete
	}
}

func (s *AttributionSampler) fillCgroup(result *Attribution, previous, current *attributionSnapshot, elapsed time.Duration) {
	if current.cgroup == nil {
		result.CgroupCoverage, result.CgroupReason = current.cgroupState, current.cgroupErr
		return
	}
	now := current.cgroup
	usage := now.usage
	result.CgroupCPUUsageUsec = &usage
	result.CgroupQuotaCores = now.quotaCores
	result.CgroupMemoryCurrentBytes = now.current
	if previous == nil || previous.cgroup == nil || elapsed <= 0 {
		result.CgroupCoverage, result.CgroupReason = CoverageIncomplete, "no earlier comparable cgroup snapshot"
		return
	}
	before := previous.cgroup
	if before.path != now.path || now.usage < before.usage {
		result.CgroupCoverage, result.CgroupReason = CoverageIncomplete, "cgroup counters changed identity or reset"
		return
	}
	cores := float64(now.usage-before.usage) / 1e6 / elapsed.Seconds()
	result.CgroupCPUCores = &cores
	if now.hasThrottle && before.hasThrottle && now.periods >= before.periods && now.throttled >= before.throttled && now.throttledUsec >= before.throttledUsec {
		periods, throttled, throttledUsec := now.periods-before.periods, now.throttled-before.throttled, now.throttledUsec-before.throttledUsec
		if throttled <= periods {
			result.CgroupPeriodsDelta, result.CgroupThrottledPeriodsDelta, result.CgroupThrottledUsecDelta = &periods, &throttled, &throttledUsec
		}
	}
	if len(now.events) > 0 && len(before.events) > 0 {
		deltas := make(map[string]uint64)
		for name, value := range now.events {
			if old, ok := before.events[name]; ok && value >= old {
				deltas[name] = value - old
			}
		}
		if len(deltas) > 0 {
			result.CgroupMemoryEventsDelta = deltas
		}
	}
	result.CgroupCoverage = CoverageComplete
}

func sameProcessSet(a, b map[procKey]procSnap) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func (s *AttributionSampler) snapshot(at time.Time) *attributionSnapshot {
	snap := &attributionSnapshot{at: at}
	if s.cfg.Descendants {
		s.readTree(snap)
	}
	if s.cgroupConfigured() {
		s.readCgroup(snap)
	}
	return snap
}

func (s *AttributionSampler) readTree(snap *attributionSnapshot) {
	dir, err := os.Open(s.procRoot)
	if err != nil {
		snap.treeErr = "process table is not readable"
		return
	}
	entries, err := dir.ReadDir(MaxAttributionScannedProcesses + 1)
	dir.Close()
	if err != nil && len(entries) == 0 {
		snap.treeErr = "process table is not readable"
		return
	}
	if len(entries) > MaxAttributionScannedProcesses {
		snap.treeTruncated = true
		entries = entries[:MaxAttributionScannedProcesses]
	}
	all := make(map[int]processStat)
	children := make(map[int][]int)
	for _, item := range entries {
		pid, convErr := strconv.Atoi(item.Name())
		if convErr != nil || pid < 1 {
			continue
		}
		text, readErr := readText(filepath.Join(s.procRoot, item.Name(), "stat"))
		if readErr != nil {
			continue // Process exited between listing and reading.
		}
		parsed, parseErr := parseProcessStat(text)
		if parseErr != nil {
			continue
		}
		all[pid] = parsed
		children[parsed.PPID] = append(children[parsed.PPID], pid)
	}
	root, ok := all[s.cfg.PID]
	if !ok {
		snap.treeErr = "workload root process is unavailable"
		return
	}
	snap.rootStart = root.StartTimeTicks
	snap.procs = make(map[procKey]procSnap)
	snap.threads = make(map[taskKey]threadSnap)
	queue := []int{s.cfg.PID}
	seen := map[int]bool{s.cfg.PID: true}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if len(snap.procs) >= MaxAttributionTreeProcesses {
			snap.treeTruncated = true
			break
		}
		stat := all[pid]
		rss := uint64(0)
		if stat.RSSPages > 0 && uint64(stat.RSSPages) <= math.MaxUint64/maxUint64(s.pageSize, 1) {
			rss = uint64(stat.RSSPages) * s.pageSize
		}
		ticks := stat.CPUUserTicks + stat.CPUSystemTicks
		if stat.CPUUserTicks > math.MaxUint64-stat.CPUSystemTicks {
			ticks = 0
		}
		snap.procs[procKey{pid: pid, start: stat.StartTimeTicks}] = procSnap{comm: sanitizeComm(stat.Comm), ticks: ticks, rss: rss, threads: stat.Threads}
		s.readThreads(snap, pid)
		kids := children[pid]
		sort.Ints(kids)
		for _, child := range kids {
			if !seen[child] {
				seen[child] = true
				queue = append(queue, child)
			}
		}
	}
}

func (s *AttributionSampler) readThreads(snap *attributionSnapshot, pid int) {
	if snap.threadsTruncated {
		return
	}
	taskRoot := filepath.Join(s.procRoot, strconv.Itoa(pid), "task")
	dir, err := os.Open(taskRoot)
	if err != nil {
		return
	}
	remaining := MaxAttributionThreads - len(snap.threads)
	entries, _ := dir.ReadDir(remaining + 1)
	dir.Close()
	if len(entries) > remaining {
		snap.threadsTruncated = true
		entries = entries[:remaining]
	}
	for _, item := range entries {
		tid, convErr := strconv.Atoi(item.Name())
		if convErr != nil || tid < 1 {
			continue
		}
		text, readErr := readText(filepath.Join(taskRoot, item.Name(), "stat"))
		if readErr != nil {
			continue
		}
		parsed, parseErr := parseProcessStat(text)
		if parseErr != nil || parsed.CPUUserTicks > math.MaxUint64-parsed.CPUSystemTicks {
			continue
		}
		snap.threads[taskKey{pid: pid, tid: tid, start: parsed.StartTimeTicks}] = threadSnap{comm: sanitizeComm(parsed.Comm), ticks: parsed.CPUUserTicks + parsed.CPUSystemTicks}
	}
}

func (s *AttributionSampler) readCgroup(snap *attributionSnapshot) {
	if _, err := os.Stat(filepath.Join(s.cgroupRoot, "cgroup.controllers")); err != nil {
		snap.cgroupState, snap.cgroupErr = CoverageUnavailable, "cgroup-v2 unified hierarchy is not visible"
		return
	}
	group := s.cfg.CgroupPath
	if group == "" {
		derived, err := processCgroup(filepath.Join(s.procRoot, strconv.Itoa(s.cfg.PID)))
		if err == nil {
			derived, err = namespaceCgroup(derived)
		}
		if err != nil {
			snap.cgroupState, snap.cgroupErr = CoverageUnavailable, "workload process cgroup-v2 path is not visible"
			return
		}
		group = derived
	}
	path := filepath.Join(s.cgroupRoot, filepath.FromSlash(group))
	root := filepath.Clean(s.cgroupRoot)
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		snap.cgroupState, snap.cgroupErr = CoverageUnavailable, "workload cgroup path is outside the visible mount"
		return
	}
	text, err := readText(filepath.Join(path, "cpu.stat"))
	if err != nil {
		snap.cgroupState, snap.cgroupErr = CoverageUnavailable, "workload cgroup cpu.stat is not readable"
		return
	}
	counters := parseCounters(text)
	usage, ok := counters["usage_usec"]
	if !ok {
		snap.cgroupState, snap.cgroupErr = CoverageUnavailable, "workload cgroup cpu.stat lacks usage_usec"
		return
	}
	c := &cgroupSnap{path: group, usage: usage}
	periods, hasPeriods := counters["nr_periods"]
	throttled, hasThrottled := counters["nr_throttled"]
	throttledUsec, hasThrottledUsec := counters["throttled_usec"]
	if hasPeriods && hasThrottled && hasThrottledUsec {
		c.periods, c.throttled, c.throttledUsec, c.hasThrottle = periods, throttled, throttledUsec, true
	}
	if value, err := readText(filepath.Join(path, "cpu.max")); err == nil {
		fields := strings.Fields(value)
		if len(fields) == 2 && fields[0] != "max" {
			quota, quotaErr := strconv.ParseUint(fields[0], 10, 64)
			period, periodErr := strconv.ParseUint(fields[1], 10, 64)
			if quotaErr == nil && periodErr == nil && period > 0 {
				cores := float64(quota) / float64(period)
				c.quotaCores = &cores
			}
		}
	}
	if value, err := readText(filepath.Join(path, "memory.current")); err == nil {
		if n, parseErr := strconv.ParseUint(value, 10, 64); parseErr == nil {
			c.current = &n
		}
	}
	if value, err := readText(filepath.Join(path, "memory.events")); err == nil {
		c.events = parseCounters(value)
	}
	snap.cgroup = c
}

func sanitizeComm(comm string) string {
	var b strings.Builder
	for _, ch := range comm {
		if b.Len() >= maxAttributionCommBytes {
			break
		}
		if ch < 0x20 || ch == 0x7f {
			ch = '?'
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// ValidateAttribution checks persisted attribution invariants so that a
// reader fails closed on impossible or inconsistent evidence.
func ValidateAttribution(a *Attribution) error {
	if a == nil {
		return nil
	}
	if a.SchemaVersion != AttributionSchemaVersion {
		return fmt.Errorf("unsupported workload attribution schema")
	}
	switch a.Scope {
	case "process_tree", "cgroup", "process_tree_and_cgroup":
	default:
		return fmt.Errorf("invalid workload attribution scope")
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
	if !finite(a.IntervalMS) {
		return fmt.Errorf("invalid workload attribution interval")
	}
	switch a.TreeCoverage {
	case CoverageComplete:
		if a.TreeCPUCores == nil || a.IntervalMS <= 0 {
			return fmt.Errorf("complete process tree evidence lacks CPU cores")
		}
	case CoverageIncomplete, CoverageTruncated, CoverageUnavailable, CoverageNotConfigured:
		if a.TreeCPUCores != nil {
			return fmt.Errorf("incomplete process tree evidence includes total CPU cores")
		}
	default:
		return fmt.Errorf("invalid process tree coverage")
	}
	if (a.TreeCoverage == CoverageNotConfigured) != !strings.HasPrefix(a.Scope, "process_tree") {
		return fmt.Errorf("process tree coverage does not match scope")
	}
	if a.TreeCPUCores != nil && !finite(*a.TreeCPUCores) || len(a.TopProcesses) > MaxAttributionTopProcesses || a.ProcessCount < 0 || a.ProcessCount > MaxAttributionTreeProcesses || a.ThreadsCompared < 0 || a.ThreadsChanged < 0 || a.ThreadsCompared > MaxAttributionThreads {
		return fmt.Errorf("invalid process tree values")
	}
	for i, p := range a.TopProcesses {
		if p.PID < 1 || !finite(p.CPUCores) || len(p.Comm) > maxAttributionCommBytes+4 || i > 0 && p.CPUCores > a.TopProcesses[i-1].CPUCores {
			return fmt.Errorf("invalid top process evidence")
		}
	}
	switch a.ThreadCoverage {
	case "", CoverageComplete, CoverageIncomplete, CoverageTruncated, CoverageUnavailable:
	default:
		return fmt.Errorf("invalid thread coverage")
	}
	if t := a.BusiestThread; t != nil && (t.PID < 1 || t.TID < 1 || !finite(t.CPUCores)) {
		return fmt.Errorf("invalid busiest thread evidence")
	}
	switch a.CgroupCoverage {
	case CoverageComplete:
		if a.CgroupCPUCores == nil || a.CgroupCPUUsageUsec == nil || a.IntervalMS <= 0 {
			return fmt.Errorf("complete cgroup evidence lacks CPU usage")
		}
	case CoverageIncomplete, CoverageUnavailable, CoverageNotConfigured:
		if a.CgroupCPUCores != nil || a.CgroupThrottledPeriodsDelta != nil {
			return fmt.Errorf("incomplete cgroup evidence includes deltas")
		}
	default:
		return fmt.Errorf("invalid cgroup coverage")
	}
	if (a.CgroupCoverage == CoverageNotConfigured) != !strings.HasSuffix(a.Scope, "cgroup") {
		return fmt.Errorf("cgroup coverage does not match scope")
	}
	if a.CgroupCPUCores != nil && !finite(*a.CgroupCPUCores) || a.CgroupQuotaCores != nil && !finite(*a.CgroupQuotaCores) {
		return fmt.Errorf("invalid cgroup CPU values")
	}
	if (a.CgroupPeriodsDelta == nil) != (a.CgroupThrottledPeriodsDelta == nil) || (a.CgroupPeriodsDelta == nil) != (a.CgroupThrottledUsecDelta == nil) {
		return fmt.Errorf("partial cgroup throttling evidence")
	}
	if a.CgroupPeriodsDelta != nil && *a.CgroupThrottledPeriodsDelta > *a.CgroupPeriodsDelta {
		return fmt.Errorf("cgroup throttled periods exceed periods")
	}
	return nil
}
