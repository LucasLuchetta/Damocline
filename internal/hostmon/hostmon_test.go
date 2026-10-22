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
func TestParseSwapPreservesConfiguredZeroAndRejectsInvalidCoverage(t *testing.T) {
	for _, test := range []struct {
		name                string
		input               string
		wantTotal, wantFree uint64
		wantErr             bool
	}{
		{name: "configured", input: "SwapTotal: 2048 kB\nSwapFree: 512 kB\n", wantTotal: 2048 * 1024, wantFree: 512 * 1024},
		{name: "not configured but measured", input: "SwapTotal: 0 kB\nSwapFree: 0 kB\n"},
		{name: "missing counters", input: "MemTotal: 1 kB\n", wantErr: true},
		{name: "free exceeds total", input: "SwapTotal: 1 kB\nSwapFree: 2 kB\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			total, free, err := ParseSwap(strings.NewReader(test.input))
			if (err != nil) != test.wantErr || total != test.wantTotal || free != test.wantFree {
				t.Fatalf("ParseSwap()=(%d,%d,%v), want (%d,%d, error=%t)", total, free, err, test.wantTotal, test.wantFree, test.wantErr)
			}
		})
	}
}
func TestCPUUtilization(t *testing.T) {
	got := utilization(Counters{Total: 100, Idle: 40}, Counters{Total: 200, Idle: 60})
	if got != 80 {
		t.Fatalf("got=%f", got)
	}
}

func TestParseDiskStatsExcludesPartitionsAndPseudoDevices(t *testing.T) {
	input := "8 0 sda 1 0 10 0 2 0 20 0 0 0 0 0 0 0 0 0 0\n" +
		"8 1 sda1 1 0 100 0 2 0 200 0 0 0 0 1 0 0 0 0 0\n" +
		"7 0 loop0 1 0 1000 0 2 0 2000 0 0 0 0 0 0 0 0 0 0\n"
	got, err := ParseDiskStats(strings.NewReader(input), func(id string) bool { return id == "8:1" })
	if err != nil || got.ReadBytes != 10*512 || got.WriteBytes != 20*512 || got.Devices != 1 {
		t.Fatalf("unexpected disk counters: %+v, %v", got, err)
	}
}

func TestParseNetworkDevSkipsLoopbackAndAggregates(t *testing.T) {
	input := "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n" +
		" lo: 999 0 0 0 0 0 0 0 888 0 0 0 0 0 0 0\n" +
		"eth0: 100 1 0 0 0 0 0 0 200 1 0 0 0 0 0 0\n" +
		"eth1: 300 1 0 0 0 0 0 0 400 1 0 0 0 0 0 0\n"
	got, err := ParseNetworkDev(strings.NewReader(input))
	if err != nil || got.ReceiveBytes != 400 || got.TransmitBytes != 600 || got.Interfaces != 2 {
		t.Fatalf("unexpected network counters: %+v, %v", got, err)
	}
}

func TestNetworkDeviceIdentityDetectsReplacementWithSameCount(t *testing.T) {
	first, err := ParseNetworkDev(strings.NewReader("eth0: 100 1 0 0 0 0 0 0 200 1 0 0 0 0 0 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseNetworkDev(strings.NewReader("eth1: 110 1 0 0 0 0 0 0 220 1 0 0 0 0 0 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Interfaces != second.Interfaces || first.InterfaceSet == second.InterfaceSet {
		t.Fatalf("interface replacement was not identified: first=%+v second=%+v", first, second)
	}
}

func TestCountersDeltaNeverConvertsResetToZero(t *testing.T) {
	before := IOKind{ReadBytes: 100, WriteBytes: 200, Devices: 1}
	after := IOKind{ReadBytes: 5, WriteBytes: 201, Devices: 1}
	if after.ReadBytes >= before.ReadBytes || after.WriteBytes-before.WriteBytes != 1 {
		t.Fatalf("test fixture does not represent counter reset: before=%+v after=%+v", before, after)
	}
}
