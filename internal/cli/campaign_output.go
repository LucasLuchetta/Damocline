package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func rejectCampaignOutputAlias(configPath, outputPath string) error {
	if outputPath == "" {
		return nil
	}
	configInfo, err := os.Stat(configPath)
	if err != nil {
		return err
	}
	if outputInfo, statErr := os.Stat(outputPath); statErr == nil {
		if os.SameFile(configInfo, outputInfo) {
			return errors.New("--out must not refer to the scenario configuration file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if strings.EqualFold(filepath.Clean(configAbs), filepath.Clean(outputAbs)) {
			return errors.New("--out must not refer to the scenario configuration file")
		}
	} else if filepath.Clean(configAbs) == filepath.Clean(outputAbs) {
		return errors.New("--out must not refer to the scenario configuration file")
	}
	return nil
}
