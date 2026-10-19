package report

import "os"

func linkReportFile(source, destination string) error {
	if err := os.Link(source, destination); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return syncReportDirectory(destination)
}
