package hostmon

import (
	"strings"
	"testing"
)

func TestParseCPU(t *testing.T) {
	c, err := ParseCPU(strings.NewReader("cpu  10 0 20 70 0 0 0 0 0 0\ncpu0 1 2 3 4\n"))
	if err != nil || c.Total != 100 || c.Idle != 70 {
		t.Fatalf("cpu=%+v error=%v", c, err)
	}
}
func TestParseMemory(t *testing.T) {
	total, avail, err := ParseMemory(strings.NewReader("MemTotal:       2000 kB\nMemAvailable:   300 kB\n"))
	if err != nil || total != 2000*1024 || avail != 300*1024 {
		t.Fatalf("%d %d %v", total, avail, err)
	}
}
func TestCPUUtilization(t *testing.T) {
	got := utilization(Counters{Total: 100, Idle: 40}, Counters{Total: 200, Idle: 60})
	if got != 80 {
		t.Fatalf("got=%f", got)
	}
}
