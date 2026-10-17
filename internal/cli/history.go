package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

const historyDir = ".damocline/history"

var errHistoryCancelled = errors.New("history selection cancelled")
var credentialReference = regexp.MustCompile(`^\$\{[A-Z_][A-Z0-9_]*\}$`)

// historyEntry stores only reusable scenario configuration. Session cookies and
// passwords remain external to the profile and must never enter the archive.
type historyEntry struct {
	SavedAt     time.Time        `json:"saved_at"`
	Scenario    config.Scenario  `json:"scenario"`
	Preferences startPreferences `json:"preferences"`
}

type historyOption struct {
	historyEntry
	Current bool
}

// preventSecretArchiving avoids copying manually edited credential headers with
// literal secret values into historical snapshots.
func preventSecretArchiving(s config.Scenario) error {
	for _, step := range s.Steps {
		for name, value := range step.Headers {
			if !credentialReference.MatchString(value) {
				return fmt.Errorf("cannot archive literal %s header; use an environment variable reference", name)
			}
		}
	}
	return nil
}

func sameProfile(a historyEntry, s config.Scenario, p startPreferences) bool {
	return reflect.DeepEqual(a.Scenario, s) && reflect.DeepEqual(a.Preferences, p)
}

func savedHistory() ([]historyOption, error) {
	var options []historyOption
	files, err := os.ReadDir(historyDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		for _, file := range files {
			if !file.Type().IsRegular() || filepath.Ext(file.Name()) != ".json" {
				continue
			}
			raw, readErr := os.ReadFile(filepath.Join(historyDir, file.Name()))
			if readErr != nil {
				return nil, fmt.Errorf("read history %s: %w", file.Name(), readErr)
			}
			var entry historyEntry
			if decodeErr := json.Unmarshal(raw, &entry); decodeErr != nil {
				return nil, fmt.Errorf("invalid history %s: %w", file.Name(), decodeErr)
			}
			if entry.Scenario.Name == "" || entry.Scenario.BaseURL == "" ||
				(entry.Preferences.Kind != "run" && entry.Preferences.Kind != "explore") {
				return nil, fmt.Errorf("invalid saved scenario in %s", file.Name())
			}
			options = append(options, historyOption{historyEntry: entry})
		}
	}
	if stat, statErr := os.Stat(scenarioPath); statErr == nil {
		s, p, loadErr := loadStartProfile()
		if loadErr != nil {
			return nil, fmt.Errorf("read current scenario: %w", loadErr)
		}
		current := historyEntry{SavedAt: stat.ModTime(), Scenario: s, Preferences: p}
		options = append(options, historyOption{historyEntry: current, Current: true})
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].SavedAt.Equal(options[j].SavedAt) {
			return options[i].Current && !options[j].Current
		}
		return options[i].SavedAt.After(options[j].SavedAt)
	})
	return options, nil
}

func archiveCurrentProfile() error {
	if _, err := os.Stat(scenarioPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	s, p, err := loadStartProfile()
	if err != nil {
		return err
	}
	if err := preventSecretArchiving(s); err != nil {
		return err
	}
	// Avoid storing duplicate snapshots when switching between older scenarios.
	options, err := savedHistory()
	if err != nil {
		return err
	}
	for _, option := range options {
		if !option.Current && sameProfile(option.historyEntry, s, p) {
			return nil
		}
	}
	entry := historyEntry{SavedAt: time.Now().UTC(), Scenario: s, Preferences: p}
	name := entry.SavedAt.Format("20060102T150405.000000000Z") + ".json"
	return writePrivateJSON(filepath.Join(historyDir, name), entry)
}

func saveActiveProfile(s config.Scenario, p startPreferences) error {
	if _, err := os.Stat(scenarioPath); err == nil {
		old, oldPreferences, loadErr := loadStartProfile()
		if loadErr != nil {
			return loadErr
		}
		if !reflect.DeepEqual(old, s) || !reflect.DeepEqual(oldPreferences, p) {
			if err := archiveCurrentProfile(); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writePrivateJSON(scenarioPath, s); err != nil {
		return err
	}
	return writePrivateJSON(preferencesPath, p)
}

func (w *wizard) selectHistory() (config.Scenario, startPreferences, error) {
	var empty config.Scenario
	var preferences startPreferences
	options, err := savedHistory()
	if err != nil {
		return empty, preferences, err
	}
	if len(options) == 0 {
		fmt.Fprintln(w.out, "No saved scenarios yet. Run damocline /start to create one.")
		return empty, preferences, errHistoryCancelled
	}
	fmt.Fprintln(w.out, "\nSaved scenarios (local configuration only; no credentials):")
	for i, option := range options {
		marker := ""
		if option.Current {
			marker = " [current]"
		}
		fmt.Fprintf(w.out, "  %d) %s | %s | %s | %d VUs | %s%s\n",
			i+1, option.Scenario.Name, option.Scenario.BaseURL,
			option.Preferences.Kind, option.Scenario.Users,
			option.SavedAt.Local().Format("2006-01-02 15:04"), marker)
	}
	for {
		choice, err := w.ask("Select a scenario number (0 to cancel)", "0")
		if err != nil {
			return empty, preferences, err
		}
		if choice == "0" {
			return empty, preferences, errHistoryCancelled
		}
		index, convErr := strconv.Atoi(choice)
		if convErr == nil && index >= 1 && index <= len(options) {
			item := options[index-1]
			return item.Scenario, item.Preferences, nil
		}
		fmt.Fprintf(w.out, "Enter a number from 0 to %d.\n", len(options))
	}
}
