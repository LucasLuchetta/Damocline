//go:build integration && !windows && !linux

package testresource

import "fmt"

// PeakRSSMethod identifies the platform kernel high-water measurement.
const PeakRSSMethod = "unsupported"

func PeakRSS() (uint64, error) {
	return 0, fmt.Errorf("process peak RSS is unsupported on this platform")
}
