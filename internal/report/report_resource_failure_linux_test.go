//go:build integration && linux

package report

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReportWriteSurvivesOSFileSizeLimit(t *testing.T) {
	const childEnv = "DAMOCLINE_REPORT_FILE_LIMIT_CHILD"
	if os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReportWriteSurvivesOSFileSizeLimit$", "-test.v")
		scratch := t.TempDir()
		command.Env = append(os.Environ(), childEnv+"=1", "TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated file-size-limit child failed: %v context=%v output=%s", err, ctx.Err(), output)
		}
		t.Log("isolated child observed EFBIG, preserved the destination, removed the failed temporary file and committed a subsequent small report")
		return
	}
	// This limit and signal disposition belong only to the isolated child.
	// The kernel permits a partial temporary-file write, then returns EFBIG.
	directory := t.TempDir()
	path := filepath.Join(directory, "report.json")
	previous := []byte("previous committed report")
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 1024, Max: 1024}); err != nil {
		t.Fatalf("set child-only file-size limit: %v", err)
	}
	err := WriteAnyJSON(path, map[string]string{"payload": strings.Repeat("x", 4096)})
	if !errors.Is(err, syscall.EFBIG) || !strings.Contains(err.Error(), "write temporary report") {
		t.Fatalf("expected a kernel-rejected partial temporary write, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(previous) {
		t.Fatalf("failed write changed the committed destination: size=%d error=%v", len(data), err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "report.json" {
		t.Fatalf("failed temporary write was not cleaned up: entries=%v error=%v", entries, err)
	}
	if err := WriteAnyJSON(path, map[string]string{"status": "recovered"}); err != nil {
		t.Fatalf("subsequent bounded write failed: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recovered map[string]string
	if err := json.Unmarshal(data, &recovered); err != nil || recovered["status"] != "recovered" {
		t.Fatalf("subsequent bounded report was not committed: error=%v", err)
	}
}
