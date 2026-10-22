package hostmon

import (
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseCounters(t *testing.T) {
	got := parseCounters("low 2\nhigh 1\nmax 0\ninvalid nope\n")
	if got["low"] != 2 || got["high"] != 1 || got["max"] != 0 || len(got) != 3 {
		t.Fatalf("unexpected counters: %#v", got)
	}
}

func TestProcessCgroupPath(t *testing.T) {
	for _, raw := range []string{"2:cpu:/worker", "0::/../../etc", "0:cpu:/worker"} {
		if _, err := parseProcessCgroup(raw); err == nil {
			t.Errorf("accepted invalid cgroup record %q", raw)
		}
	}
	path, err := parseProcessCgroup("0::/system.slice/app.service")
	if err != nil || path != "system.slice/app.service" {
		t.Fatalf("valid v2 path: %q %v", path, err)
	}
}

func TestNamespaceCgroupRejectsPathsOutsideVisibleMount(t *testing.T) {
	for _, raw := range []string{"../../etc/passwd", "../c314cdf69952a1fee52ab52420084f613ed595c2eff2dbb105c7fe2f69e2bafc", "../not-an-id", ".."} {
		if _, err := namespaceCgroup(raw); err == nil {
			t.Errorf("accepted unsafe namespace path %q", raw)
		}
	}
	if got, err := namespaceCgroup("system.slice/app.service"); err != nil || got != "system.slice/app.service" {
		t.Fatalf("valid visible cgroup path: %q %v", got, err)
	}
}

func TestParseProcessStatHandlesParenthesesAndExtractsIdentityAndRSS(t *testing.T) {
	fields := make([]string, 22)
	fields[0] = "R"
	for i := 1; i < len(fields); i++ {
		fields[i] = "0"
	}
	fields[19] = "987654"
	fields[21] = "12"
	fields[11], fields[12] = "120", "30"
	parsed, err := parseProcessStat("4321 (worker ) with spaces) " + joinFields(fields))
	if err != nil || parsed.StartTimeTicks != 987654 || parsed.RSSPages != 12 || parsed.CPUUserTicks != 120 || parsed.CPUSystemTicks != 30 {
		t.Fatalf("unexpected process stat parse: %+v err=%v", parsed, err)
	}
	if _, err := parseProcessStat("4321 (truncated) R 1"); err == nil {
		t.Fatal("accepted truncated process stat")
	}
}

func TestParseAuxvClockTicks(t *testing.T) {
	word := strconv.IntSize / 8
	data := make([]byte, 4*word)
	putWord := func(index int, value uint64) {
		if word == 8 {
			binary.NativeEndian.PutUint64(data[index*word:], value)
		} else {
			binary.NativeEndian.PutUint32(data[index*word:], uint32(value))
		}
	}
	putWord(0, 3)
	putWord(1, 4096)
	putWord(2, 17)
	putWord(3, 250)
	got, err := parseAuxvClockTicks(data)
	if err != nil || got != 250 {
		t.Fatalf("clock ticks=%d err=%v", got, err)
	}
	if _, err := parseAuxvClockTicks(data[:word]); err == nil {
		t.Fatal("accepted truncated auxv")
	}
}

func TestApplyProcessCPUDeltaUsesClockRateAndRejectsPIDReuse(t *testing.T) {
	previousTicks, currentTicks := uint64(100), uint64(150)
	previous := &Workload{PID: 5, StartTimeTicks: 99, ProcessCPUTimeTicks: &previousTicks, ProcessCPUClockTicksPerSecond: 100}
	current := &Workload{PID: 5, StartTimeTicks: 99, ProcessCPUTimeTicks: &currentTicks, ProcessCPUClockTicksPerSecond: 100}
	applyProcessCPUDelta(previous, current, 500*time.Millisecond)
	if current.ProcessCPUCoverage != "COMPLETE" || current.ProcessCPUDeltaTicks == nil || *current.ProcessCPUDeltaTicks != 50 || current.ProcessCPUPercent == nil || *current.ProcessCPUPercent != 100 || current.ProcessCPUIntervalMS == nil || *current.ProcessCPUIntervalMS != 500 {
		t.Fatalf("unexpected process CPU delta: %+v", current)
	}
	reused := &Workload{PID: 5, StartTimeTicks: 100, ProcessCPUTimeTicks: &currentTicks, ProcessCPUClockTicksPerSecond: 100}
	applyProcessCPUDelta(previous, reused, 500*time.Millisecond)
	if reused.ProcessCPUCoverage != "INCOMPLETE" || reused.ProcessCPUPercent != nil {
		t.Fatalf("PID reuse was treated as comparable: %+v", reused)
	}
}

func joinFields(fields []string) string {
	return strings.Join(fields, " ")
}
