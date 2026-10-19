package campaignprofile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MaxProfiles     = 100
	MaxProfileBytes = 1 << 20
)

type Store struct{ Dir string }

func (s Store) Save(profile Profile, replace bool) error {
	if err := Validate(profile); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return fmt.Errorf("create private campaign profile directory: %w", err)
	}
	path := s.path(profile.Name)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("existing campaign profile must be a regular file")
		}
		if !replace {
			return fmt.Errorf("campaign profile %q already exists; use --replace to update it", profile.Name)
		}
		old, _, loadErr := s.Load(profile.Name)
		if loadErr != nil {
			return fmt.Errorf("cannot replace invalid campaign profile: %w", loadErr)
		}
		profile.CreatedAt = old.CreatedAt
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect campaign profile: %w", err)
	}
	profiles, err := s.List()
	if err != nil {
		return err
	}
	if len(profiles) >= MaxProfiles {
		found := false
		for _, item := range profiles {
			if item.Name == profile.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("campaign profile limit reached (%d); delete profiles explicitly", MaxProfiles)
		}
	}
	profile.SchemaVersion = CurrentSchemaVersion
	if profile.CreatedAt == "" {
		profile.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	profile.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writePrivateAtomic(path, data, replace)
}

func (s Store) Load(name string) (Profile, bool, error) {
	if !ValidName(name) {
		return Profile{}, false, errors.New("invalid campaign profile name")
	}
	path := s.path(name)
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, false, fmt.Errorf("open campaign profile: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Profile{}, false, err
	}
	if !info.Mode().IsRegular() {
		return Profile{}, false, errors.New("campaign profile must be a regular file")
	}
	if info.Size() > MaxProfileBytes {
		return Profile{}, false, errors.New("campaign profile exceeds the 1 MiB safety limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxProfileBytes+1))
	if err != nil {
		return Profile{}, false, err
	}
	if len(data) > MaxProfileBytes {
		return Profile{}, false, errors.New("campaign profile exceeds the 1 MiB safety limit")
	}
	profile, migrated, err := Decode(data)
	if err != nil {
		return Profile{}, false, err
	}
	if profile.Name != name {
		return Profile{}, false, errors.New("campaign profile name does not match its file name")
	}
	return profile, migrated, nil
}

func (s Store) Import(path string) (Profile, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, false, fmt.Errorf("open campaign profile source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Profile{}, false, err
	}
	if !info.Mode().IsRegular() {
		return Profile{}, false, errors.New("campaign profile source must be a regular file")
	}
	if info.Size() > MaxProfileBytes {
		return Profile{}, false, errors.New("campaign profile source exceeds the 1 MiB safety limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxProfileBytes+1))
	if err != nil {
		return Profile{}, false, err
	}
	if len(data) > MaxProfileBytes {
		return Profile{}, false, errors.New("campaign profile source exceeds the 1 MiB safety limit")
	}
	profile, migrated, err := Decode(data)
	return profile, migrated, err
}

func (s Store) Migrate(name string) (bool, error) {
	profile, migrated, err := s.Load(name)
	if err != nil {
		return false, err
	}
	if !migrated {
		return false, nil
	}
	profile.SchemaVersion = CurrentSchemaVersion
	if err := s.Save(profile, true); err != nil {
		return false, fmt.Errorf("write migrated campaign profile: %w", err)
	}
	return true, nil
}

func (s Store) List() ([]Profile, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read campaign profile directory: %w", err)
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("campaign profile %q must not be a symbolic link", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect campaign profile %q: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("campaign profile %q is not a regular file", entry.Name())
		}
		if info.Size() > MaxProfileBytes {
			return nil, fmt.Errorf("campaign profile %q exceeds the 1 MiB safety limit", entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		profile, _, err := s.Load(name)
		if err != nil {
			return nil, fmt.Errorf("invalid campaign profile %q: %w", name, err)
		}
		profiles = append(profiles, profile)
	}
	if len(profiles) > MaxProfiles {
		return nil, fmt.Errorf("campaign profile directory exceeds the %d-profile limit", MaxProfiles)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func (s Store) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("invalid campaign profile name")
	}
	if err := os.Remove(s.path(name)); err != nil {
		return fmt.Errorf("delete campaign profile: %w", err)
	}
	return nil
}

func (s Store) path(name string) string { return filepath.Join(s.Dir, name+".json") }

func (s Store) Path(name string) string { return s.path(name) }

func writePrivateAtomic(path string, data []byte, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".campaign-profile-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary campaign profile: %w", err)
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if !replace {
		if _, err := os.Lstat(path); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install campaign profile: %w", err)
	}
	return os.Chmod(path, 0600)
}
