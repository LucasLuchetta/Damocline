package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportHistoryCommandsArchiveListPruneAndDelete(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "history")
	var savedIDs []string
	for index := 0; index < 3; index++ {
		source := filepath.Join(dir, fmt.Sprintf("run-%d.json", index))
		data := fmt.Sprintf("{\"schema_version\":22,\"name\":\"synthetic-%d\",\"target\":\"https://private.example\",\"outcome\":\"PASS\",\"started_at\":\"2026-01-01T00:00:00Z\",\"config\":{}}", index)
		if err := os.WriteFile(source, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		code := Run([]string{"report", "save", "--file", source, "--dir", archive}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("archive command failed: code=%d stderr=%s", code, stderr.String())
		}
		fields := strings.Fields(stdout.String())
		savedIDs = append(savedIDs, fields[1])
		time.Sleep(time.Millisecond)
	}

	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "history", "--dir", archive}, &stdout, &stderr); code != 0 {
		t.Fatalf("history command failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "synthetic-2") || strings.Contains(stdout.String(), "private.example") {
		t.Fatalf("history output omitted safe metadata or exposed target host: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"report", "prune", "--keep", "2", "--dir", archive}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Pruned 1") {
		t.Fatalf("explicit retention failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"report", "delete", "--id", savedIDs[1], "--dir", archive}, &stdout, &stderr); code != 0 {
		t.Fatalf("explicit deletion failed: code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"report", "history", "--dir", archive}, &stdout, &stderr); code != 0 || strings.Contains(stdout.String(), savedIDs[0]) || strings.Contains(stdout.String(), savedIDs[1]) || !strings.Contains(stdout.String(), savedIDs[2]) {
		t.Fatalf("history after prune/delete is inconsistent: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestReportPruneRequiresExplicitPositiveKeepCount(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := Run([]string{"report", "prune"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "deletion is never automatic") {
		t.Fatalf("prune without an explicit policy was accepted: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
