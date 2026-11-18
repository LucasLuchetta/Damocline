//go:build integration && linux

package testresource

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPeakRSSStatusRequiresBoundedPositiveKilobytes(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		want         uint64
	}{
		{"valid", "Name:\tfixture\nVmHWM:\t1234 kB\nVmRSS:\t500 kB\n", 1234 * 1024},
		{"missing", "VmRSS: 1234 kB\n", 0},
		{"zero", "VmHWM: 0 kB\n", 0},
		{"negative", "VmHWM: -1 kB\n", 0},
		{"unit", "VmHWM: 12 MB\n", 0},
		{"duplicate", "VmHWM: 12 kB\nVmHWM: 13 kB\n", 0},
		{"overflow", "VmHWM: 18446744073709551615 kB\n", 0},
		{"malformed", "VmHWM: 12\n", 0},
		{"oversized", strings.Repeat("x", 65537), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := peakRSSFromStatus(strings.NewReader(tc.status))
			if (err != nil) != (tc.want == 0) || got != tc.want {
				t.Fatalf("peak=%d error=%v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestPeakRSSMeasuresCurrentExecutableImage(t *testing.T) {
	const childMarker = "DAMOCLINE_PEAK_IMAGE_CHILD"
	if os.Getenv(childMarker) == "1" {
		before, err := PeakRSS()
		if err != nil {
			t.Fatal(err)
		}
		memory := make([]byte, 16<<20)
		for i := 0; i < len(memory); i += 4096 {
			memory[i] = 1
		}
		peak, err := PeakRSS()
		runtime.KeepAlive(memory)
		if err != nil || peak < 16<<20 || peak < before || peak >= 64<<20 {
			t.Fatalf("current image peak includes unrelated launch memory or misses its allocation: before=%d after=%d error=%v", before, peak, err)
		}
		var lifetime syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &lifetime); err != nil {
			t.Fatal(err)
		}
		t.Logf("image_peak_bytes=%d process_lifetime_peak_kib=%d", peak, lifetime.Maxrss)
		return
	}
	// Keep a larger parent resident while launching a fresh executable image.
	// Linux getrusage retains pre-exec history; this child must measure its own mm.
	memory := make([]byte, 128<<20)
	for i := 0; i < len(memory); i += 4096 {
		memory[i] = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPeakRSSMeasuresCurrentExecutableImage$", "-test.v")
	scratch := t.TempDir()
	command.Env = append(os.Environ(), childMarker+"=1", "TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch)
	output, err := command.CombinedOutput()
	runtime.KeepAlive(memory)
	if err != nil {
		t.Fatalf("fresh image peak regression failed: %v context=%v output=%s", err, ctx.Err(), output)
	}
	t.Logf("fresh image result: %s", output)
}
