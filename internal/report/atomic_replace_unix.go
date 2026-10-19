//go:build !windows

package report

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceReportFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("open report directory for sync: %w", err)
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return fmt.Errorf("report replacement succeeded, but syncing its directory failed: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close report directory: %w", err)
	}
	return nil
}

func syncReportDirectory(destination string) error {
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("open report directory for sync: %w", err)
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return fmt.Errorf("report archive succeeded, but syncing its directory failed: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close report directory: %w", err)
	}
	return nil
}
