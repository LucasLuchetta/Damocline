// Package reportstore manages an explicitly requested, bounded local history
// of JSON evidence artifacts.
package reportstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

const (
	MaxReports    = 100
	MaxStoreBytes = 512 << 20
	maxReportSize = 16 << 20
)

var (
	idPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{64}$`)
	storeMu   sync.Mutex
)

type Entry struct {
	ID        string
	Kind      string
	Scenario  string
	Outcome   string
	StartedAt string
	Bytes     int64
	SavedAt   time.Time
}

type Store struct {
	Dir string
}

func (s Store) Save(source string) (Entry, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := readArtifact(source)
	if err != nil {
		return Entry{}, err
	}
	metadata, err := inspectArtifact(data)
	if err != nil {
		return Entry{}, err
	}
	digest := sha256.Sum256(data)
	entries, err := s.listUnlocked()
	if err != nil {
		return Entry{}, err
	}
	var usedBytes int64
	for _, entry := range entries {
		usedBytes += entry.Bytes
	}
	if len(entries) >= MaxReports || usedBytes+int64(len(data)) > MaxStoreBytes {
		return Entry{}, fmt.Errorf("local report history limit reached (%d reports or %d MiB); prune older reports explicitly", MaxReports, MaxStoreBytes>>20)
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return Entry{}, fmt.Errorf("create report history directory: %w", err)
	}
	for attempts := 0; attempts < 16; attempts++ {
		savedAt := time.Now().UTC()
		id := savedAt.Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(digest[:])
		destination := filepath.Join(s.Dir, id+".json")
		if err := report.WriteJSONBytesExclusive(destination, data); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return Entry{}, fmt.Errorf("archive report: %w", err)
		}
		return Entry{ID: id, Kind: metadata.Kind, Scenario: metadata.Scenario, Outcome: metadata.Outcome,
			StartedAt: metadata.StartedAt, Bytes: int64(len(data)), SavedAt: savedAt}, nil
	}
	return Entry{}, errors.New("could not allocate a unique report archive ID after repeated timestamp collisions")
}

func (s Store) List() ([]Entry, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return s.listUnlocked()
}

func (s Store) Delete(id string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if !idPattern.MatchString(id) {
		return errors.New("report ID has an invalid format")
	}
	path := filepath.Join(s.Dir, id+".json")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete archived report: %w", err)
	}
	return nil
}

func (s Store) Prune(keep int) ([]Entry, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if keep < 1 || keep > MaxReports {
		return nil, fmt.Errorf("keep must be between 1 and %d; delete individual reports explicitly to empty history", MaxReports)
	}
	entries, err := s.listUnlocked()
	if err != nil {
		return nil, err
	}
	if len(entries) <= keep {
		return []Entry{}, nil
	}
	removed := make([]Entry, 0, len(entries)-keep)
	for _, entry := range entries[keep:] {
		if err := os.Remove(filepath.Join(s.Dir, entry.ID+".json")); err != nil {
			return removed, fmt.Errorf("prune archived report %s: %w", entry.ID, err)
		}
		removed = append(removed, entry)
	}
	return removed, nil
}

func (s Store) listUnlocked() ([]Entry, error) {
	dirEntries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read report history: %w", err)
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !idPattern.MatchString(id) {
			continue
		}
		info, err := dirEntry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect archived report %s: %w", id, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("archived report %s is not a regular file", id)
		}
		if info.Size() > maxReportSize {
			return nil, fmt.Errorf("archived report %s exceeds the 16 MiB archive limit", id)
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("read archived report %s: %w", id, err)
		}
		metadata, err := inspectArtifact(data)
		if err != nil {
			return nil, fmt.Errorf("archived report %s is invalid: %w", id, err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != id[27:] {
			return nil, fmt.Errorf("archived report %s failed its content digest check", id)
		}
		stamp, err := time.Parse("20060102T150405.000000000Z", id[:26])
		if err != nil {
			return nil, fmt.Errorf("archived report %s has an invalid timestamp", id)
		}
		entries = append(entries, Entry{ID: id, Kind: metadata.Kind, Scenario: metadata.Scenario,
			Outcome: metadata.Outcome, StartedAt: metadata.StartedAt, Bytes: info.Size(), SavedAt: stamp})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SavedAt.Equal(entries[j].SavedAt) {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].SavedAt.After(entries[j].SavedAt)
	})
	return entries, nil
}

func readArtifact(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open report artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect report artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("report artifact must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReportSize+1))
	if err != nil {
		return nil, fmt.Errorf("read report artifact: %w", err)
	}
	if len(data) > maxReportSize {
		return nil, errors.New("report artifact exceeds the 16 MiB archive limit")
	}
	if _, err := inspectArtifact(data); err != nil {
		return nil, err
	}
	return data, nil
}

type artifactMetadata struct {
	Kind      string
	Scenario  string
	Outcome   string
	StartedAt string
}

func inspectArtifact(data []byte) (artifactMetadata, error) {
	var object map[string]json.RawMessage
	if !json.Valid(data) || json.Unmarshal(data, &object) != nil || object == nil {
		return artifactMetadata{}, errors.New("report artifact must be a valid JSON object")
	}
	metadata := artifactMetadata{Kind: "json-artifact"}
	switch {
	case object["arrival_campaign_schema_version"] != nil:
		metadata.Kind = "arrival-campaign"
	case object["campaign_schema_version"] != nil:
		metadata.Kind = "campaign"
	case object["arrival_explorer_schema_version"] != nil:
		metadata.Kind = "arrival-exploration"
	case object["schema_version"] != nil && object["config"] != nil:
		metadata.Kind = "run"
	case object["schema_version"] != nil && object["probes"] != nil:
		metadata.Kind = "exploration"
	case object["schema_version"] != nil && object["campaigns"] != nil:
		metadata.Kind = "campaign-audit"
	}
	metadata.Scenario = jsonString(object, "scenario")
	if metadata.Scenario == "" {
		metadata.Scenario = jsonString(object, "name")
	}
	metadata.Outcome = jsonString(object, "outcome")
	metadata.StartedAt = jsonString(object, "started_at")
	return metadata, nil
}

func jsonString(object map[string]json.RawMessage, key string) string {
	var value string
	if json.Unmarshal(object[key], &value) != nil {
		return ""
	}
	return value
}
