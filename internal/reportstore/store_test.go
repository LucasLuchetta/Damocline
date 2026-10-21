package reportstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveListsByteExactVersionedArtifacts(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: filepath.Join(dir, "history")}
	source := filepath.Join(dir, "run.json")
	data := []byte("{\"schema_version\":22,\"name\":\"synthetic\",\"outcome\":\"PASS\",\"started_at\":\"2026-01-01T00:00:00Z\",\"config\":{}}\n")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := store.Save(source)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(filepath.Join(store.Dir, first.ID+".json"))
	if err != nil || string(archived) != string(data) {
		t.Fatalf("archive changed artifact bytes: %q err=%v", archived, err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("history entries=%v err=%v", entries, err)
	}
	entry := entries[0]
	if entry.ID != first.ID || entry.Kind != "run" || entry.Scenario != "synthetic" || entry.Outcome != "PASS" || entry.Bytes != int64(len(data)) {
		t.Fatalf("unexpected archived metadata: %+v", entry)
	}
	if strings.Contains(fmt.Sprint(entry), "run.json") || strings.Contains(fmt.Sprint(entry), "target.example") {
		t.Fatal("history metadata exposed source paths or target hostnames")
	}
}

func TestSaveRequiresBoundedJSONArtifacts(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: filepath.Join(dir, "history")}
	for name, data := range map[string][]byte{
		"invalid.json":   []byte("not json"),
		"array.json":     []byte("[]"),
		"duplicate.json": []byte(`{"schema_version":1,"schema_\u0076ersion":2}`),
	} {
		source := filepath.Join(dir, name)
		if err := os.WriteFile(source, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Save(source); err == nil {
			t.Fatalf("invalid artifact %s was archived", name)
		}
	}
	oversized := filepath.Join(dir, "oversized.json")
	if err := os.WriteFile(oversized, []byte("{\"payload\":\""+strings.Repeat("x", maxReportSize)+"\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(oversized); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("oversized artifact was not rejected clearly: %v", err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid inputs created archive entries: %v err=%v", entries, err)
	}
}

func TestDeleteRequiresExactArchiveID(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	for _, id := range []string{"../run", "run.json", "20261019T052512.000000000Z-" + strings.Repeat("a", 63)} {
		if err := store.Delete(id); err == nil {
			t.Fatalf("invalid archive ID %q was accepted", id)
		}
	}
	outside := filepath.Join(filepath.Dir(store.Dir), "keep.json")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("../keep"); err == nil {
		t.Fatal("path traversal archive ID was accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("invalid delete changed a file outside history: %q err=%v", data, err)
	}
}

func TestPruneIsExplicitAndKeepsNewestEntries(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: filepath.Join(dir, "history")}
	ids := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		source := filepath.Join(dir, fmt.Sprintf("run-%d.json", index))
		data := fmt.Sprintf("{\"schema_version\":22,\"name\":\"run-%d\",\"outcome\":\"PASS\",\"config\":{}}\n", index)
		if err := os.WriteFile(source, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		entry, err := store.Save(source)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, entry.ID)
		time.Sleep(time.Millisecond)
	}
	removed, err := store.Prune(2)
	if err != nil || len(removed) != 1 || removed[0].ID != ids[0] {
		t.Fatalf("prune removed=%v err=%v; want oldest ID %s", removed, err, ids[0])
	}
	entries, err := store.List()
	if err != nil || len(entries) != 2 || entries[0].ID != ids[2] || entries[1].ID != ids[1] {
		t.Fatalf("retained entries=%v err=%v", entries, err)
	}
	if _, err := store.Prune(0); err == nil {
		t.Fatal("zero keep count was accepted; deleting all history requires explicit per-ID deletion")
	}
	if err := store.Delete(ids[1]); err != nil {
		t.Fatal(err)
	}
	entries, err = store.List()
	if err != nil || len(entries) != 1 || entries[0].ID != ids[2] {
		t.Fatalf("explicit delete left unexpected entries: %v err=%v", entries, err)
	}
}

func TestSaveRefusesFullHistoryWithoutDeletingExistingReports(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if err := os.MkdirAll(store.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxReports; index++ {
		data := []byte(fmt.Sprintf("{\"schema_version\":22,\"name\":\"existing-%d\",\"config\":{}}", index))
		digest := sha256.Sum256(data)
		id := fmt.Sprintf("20261019T052512.%09dZ-%s", index, hex.EncodeToString(digest[:]))
		if err := os.WriteFile(filepath.Join(store.Dir, id+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(t.TempDir(), "new.json")
	if err := os.WriteFile(source, []byte("{\"schema_version\":22,\"name\":\"new\",\"config\":{}}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(source); err == nil || !strings.Contains(err.Error(), "limit reached") {
		t.Fatalf("full archive did not stop accepting reports: %v", err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != MaxReports {
		t.Fatalf("refused save deleted existing history: count=%d err=%v", len(entries), err)
	}
}

func TestListDetectsArchiveContentTampering(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	data, err := json.Marshal(map[string]any{"schema_version": 22, "config": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(digest[:])
	if err := os.WriteFile(filepath.Join(store.Dir, id+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, id+".json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil || !strings.Contains(err.Error(), "digest check") {
		t.Fatalf("tampered archived report was listed as valid: %v", err)
	}
}

func TestListRejectsReportCountBeforeReadingBodies(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// Deliberately invalid contents: the count limit must be checked first.
	for index := 0; index <= MaxReports; index++ {
		id := fmt.Sprintf("20261020T052512.%09dZ-%064x", index, index+1)
		path := filepath.Join(store.Dir, id+".json")
		if err := os.WriteFile(path, []byte("not JSON"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.List(); err == nil || !strings.Contains(err.Error(), "100-report archive limit") {
		t.Fatalf("unbounded history was parsed before enforcing the count cap: %v", err)
	}
}

func TestListRejectsAggregateBytesBeforeReadingBodies(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// Sparse files avoid allocating hundreds of MiB during this test.
	// Invalid JSON is intentional: an oversized aggregate must fail early.
	for index := 0; index <= MaxStoreBytes/maxReportSize; index++ {
		id := fmt.Sprintf("20261020T052512.%09dZ-%064x", index, index+1)
		path := filepath.Join(store.Dir, id+".json")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxReportSize); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.List(); err == nil || !strings.Contains(err.Error(), "512 MiB archive limit") {
		t.Fatalf("oversized history was parsed before enforcing the byte cap: %v", err)
	}
}

func TestBoundedReaderRejectsOversizedArchiveContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxReportSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegularFile(path); err == nil || !strings.Contains(err.Error(), "16 MiB archive limit") {
		t.Fatalf("oversized report bypassed bounded descriptor read: %v", err)
	}
}
