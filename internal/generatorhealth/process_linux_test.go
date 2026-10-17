//go:build linux

package generatorhealth

import "testing"

func TestResidentBytesRejectsZeroAndOverflow(t *testing.T) {
	if _, ok := residentBytesFromPages(0, 4096); ok {
		t.Fatal("zero resident pages must remain unavailable")
	}
	if _, ok := residentBytesFromPages(^uint64(0), 4096); ok {
		t.Fatal("page multiplication overflow must be rejected")
	}
	if got, ok := residentBytesFromPages(2, 4096); !ok || got != 8192 {
		t.Fatalf("valid resident page count not converted: got=%d ok=%v", got, ok)
	}
}

func TestProcessCPUTimeRejectsOverflowAndInvalidMicroseconds(t *testing.T) {
	maxInt64 := int64(^uint64(0) >> 1)
	if _, ok := processCPUTime(maxInt64, 0, 1, 0); ok {
		t.Fatal("seconds addition overflow must be rejected")
	}
	if _, ok := processCPUTime(1, 1_000_000, 0, 0); ok {
		t.Fatal("out-of-range microseconds must be rejected")
	}
	if got, ok := processCPUTime(1, 250_000, 2, 250_000); !ok || got != 3.5 {
		t.Fatalf("valid process CPU duration not converted: got=%v ok=%v", got, ok)
	}
}
