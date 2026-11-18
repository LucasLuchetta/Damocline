//go:build integration

package report

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/testresource"
)

func TestRepeatedBoundedReportWritesReleaseHeapAndTemporaryFiles(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OS process peak RSS observation is supported only on Windows and Linux")
	}
	const childResultEnv = "DAMOCLINE_REPORT_RESOURCE_CHILD_RESULT"
	if resultPath := os.Getenv(childResultEnv); resultPath != "" {
		result := exerciseBoundedReportWrites(t)
		peak, err := testresource.PeakRSS()
		if err != nil || peak == 0 {
			t.Fatalf("process peak RSS is unavailable: bytes=%d error=%v", peak, err)
		}
		result.PeakRSSBytes = peak
		result.PeakRSSMethod = testresource.PeakRSSMethod
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resultPath, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	resultPath := filepath.Join(t.TempDir(), "child-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRepeatedBoundedReportWritesReleaseHeapAndTemporaryFiles$", "-test.v")
	scratch := t.TempDir()
	command.Env = append(os.Environ(), childResultEnv+"="+resultPath, "TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated report resource child failed: %v context=%v output=%s", err, ctx.Err(), output)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result reportResourceObservation
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	const maximumPeakRSS = 256 << 20
	if result.AcceptedWrites != 32 || result.RejectedWrites != 32 || result.PeakRSSBytes == 0 || result.PeakRSSBytes > maximumPeakRSS || result.PeakRSSMethod != testresource.PeakRSSMethod {
		t.Fatalf("isolated resource observation did not meet its declared fixture bound: %+v peak_limit_bytes=%d", result, maximumPeakRSS)
	}
	t.Logf("accepted_writes=%d rejected_writes=%d before_heap_bytes=%d after_heap_bytes=%d total_allocated_bytes=%d process_peak_rss_bytes=%d peak_limit_bytes=%d peak_rss_method=%s",
		result.AcceptedWrites, result.RejectedWrites, result.BeforeHeapBytes, result.AfterHeapBytes,
		result.TotalAllocatedBytes, result.PeakRSSBytes, maximumPeakRSS, result.PeakRSSMethod)
}

type reportResourceObservation struct {
	AcceptedWrites      int    `json:"accepted_writes"`
	RejectedWrites      int    `json:"rejected_writes"`
	BeforeHeapBytes     uint64 `json:"before_heap_bytes"`
	AfterHeapBytes      uint64 `json:"after_heap_bytes"`
	TotalAllocatedBytes uint64 `json:"total_allocated_bytes"`
	PeakRSSBytes        uint64 `json:"process_peak_rss_bytes"`
	PeakRSSMethod       string `json:"peak_rss_method"`
}

func exerciseBoundedReportWrites(t *testing.T) reportResourceObservation {
	t.Helper()
	const repeats = 32
	const maximumRetainedGrowth = 8 << 20
	directory := t.TempDir()
	path := filepath.Join(directory, "report.json")
	accepted := map[string]string{"payload": strings.Repeat("x", 1<<20)}
	rejected := map[string]string{"payload": strings.Repeat("y", 64<<20)}
	if err := WriteAnyJSON(path, accepted); err != nil {
		t.Fatal(err)
	}
	wantDigest := resourceReportDigest(t, path)
	// Two collections also discard buffers held in sync.Pool's victim cache.
	// This observes retained Go heap, not peak RSS or allocation-free operation.
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for iteration := 0; iteration < repeats; iteration++ {
		if err := WriteAnyJSON(path, accepted); err != nil {
			t.Fatalf("accepted write %d: %v", iteration, err)
		}
		if err := WriteAnyJSON(path, rejected); !errors.Is(err, errReportResourceBudget) {
			t.Fatalf("oversized write %d: %v", iteration, err)
		}
		if digest := resourceReportDigest(t, path); digest != wantDigest {
			t.Fatalf("write %d changed the committed report digest", iteration)
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "report.json" {
			t.Fatalf("write %d left unexpected temporary files: %v", iteration, entries)
		}
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(accepted)
	runtime.KeepAlive(rejected)
	if after.HeapAlloc > before.HeapAlloc && after.HeapAlloc-before.HeapAlloc > maximumRetainedGrowth {
		t.Fatalf("retained heap grew by %d bytes after %d accepted and %d rejected writes; limit %d",
			after.HeapAlloc-before.HeapAlloc, repeats, repeats, maximumRetainedGrowth)
	}
	return reportResourceObservation{
		AcceptedWrites: repeats, RejectedWrites: repeats, BeforeHeapBytes: before.HeapAlloc,
		AfterHeapBytes: after.HeapAlloc, TotalAllocatedBytes: after.TotalAlloc - before.TotalAlloc,
	}
}

func resourceReportDigest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	bytes, err := io.Copy(hash, io.LimitReader(file, maxReportBytes+1))
	if err != nil || bytes == 0 || bytes > maxReportBytes {
		t.Fatalf("committed report size=%d read error=%v", bytes, err)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}
