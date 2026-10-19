package report

import "math"

// SuccessfulJourneysPerSecond returns the rate of complete, successful
// configured journeys during the observation window. Failed complete journeys
// and cancelled incomplete journeys remain separate in the underlying ledger.
func SuccessfulJourneysPerSecond(r Result) float64 {
	seconds := r.ObservationSeconds
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || r.Journeys.Success <= 0 {
		return 0
	}
	return float64(r.Journeys.Success) / seconds
}

// CompletedJourneysPerSecond includes successful and failed journeys that
// completed their configured workflow; cancelled journeys are excluded.
func CompletedJourneysPerSecond(r Result) float64 {
	seconds := r.ObservationSeconds
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || r.Journeys.Total <= 0 {
		return 0
	}
	return float64(r.Journeys.Total) / seconds
}
