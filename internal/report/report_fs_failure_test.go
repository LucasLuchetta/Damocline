package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These G0-09 fixtures exercise report-writing failure modes that can be
// induced deterministically on every supported OS inside a test temp dir.

func TestWriteAnyJSONFailsWhenParentIsRegularFileWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnyJSON(filepath.Join(parent, "report.json"), map[string]string{"result": "x"}); err == nil {
		t.Fatal("writing beneath a regular file unexpectedly succeeded")
	}
	data, err := os.ReadFile(parent)
	if err != nil || string(data) != "keep" {
		t.Fatalf("failed write changed the blocking file: %q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed write left files behind: %v err=%v", entries, err)
	}
}

// A writer interrupted after creating its temporary file but before the
// atomic replacement leaves only an orphan temp file. The destination must
// still hold the previous complete report, and a later write must succeed
// without adopting or depending on the orphan.
func TestInterruptedReplacementLeavesPreviousReportAndRecovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := WriteAnyJSON(path, map[string]string{"result": "previous"}); err != nil {
		t.Fatal(err)
	}
	orphan, err := os.CreateTemp(dir, ".damocline-report-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orphan.WriteString(`{"result": "trunc`); err != nil {
		t.Fatal(err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	assertReport := func(want string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]string
		if err := json.Unmarshal(data, &saved); err != nil || saved["result"] != want {
			t.Fatalf("destination = %q err=%v; want complete %q report", data, err, want)
		}
	}
	assertReport("previous")
	if err := WriteAnyJSON(path, map[string]string{"result": "recovered"}); err != nil {
		t.Fatal(err)
	}
	assertReport("recovered")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected only the report and the pre-existing orphan: %v err=%v", entries, err)
	}
	orphanData, err := os.ReadFile(orphan.Name())
	if err != nil || string(orphanData) != `{"result": "trunc` {
		t.Fatalf("writer modified an unrelated orphan temp file: %q err=%v", orphanData, err)
	}
}

func TestExclusiveReportWriteRefusesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.json")
	if err := WriteJSONBytesExclusive(path, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONBytesExclusive(path, []byte(`{"v":2}`)); err == nil {
		t.Fatal("exclusive write replaced an existing archive entry")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"v":1}` {
		t.Fatalf("exclusive failure changed the archive entry: %q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("exclusive failure left temporary files: %v err=%v", entries, err)
	}
}

// Concurrent replacements must never expose a torn or interleaved report:
// the destination is always exactly one writer's complete document.
func TestConcurrentReportReplacementsNeverExposeTornReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	const writers = 8
	payload := strings.Repeat("x", 64<<10)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// On Windows a concurrent MoveFileEx may transiently be denied;
			// such a refusal is acceptable, a torn destination is not.
			_ = WriteAnyJSON(path, map[string]string{"writer": fmt.Sprint(i), "payload": payload})
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil || saved["payload"] != payload || saved["writer"] == "" {
		t.Fatalf("concurrent replacement exposed an incomplete report (%d bytes): err=%v", len(data), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("concurrent writers left temporary files: %v err=%v", entries, err)
	}
}
