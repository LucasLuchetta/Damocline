package calibration

import "github.com/LucasLuchetta/Damocline/internal/config"

// ReferenceWorkloadFingerprint identifies the HTTP workload shape executed by
// every healthy preflight stage. It deliberately excludes target identity and
// rate/duration settings, which are validated as separate calibration fields.
func ReferenceWorkloadFingerprint() (string, error) {
	scenario := config.Scenario{
		Name:         "Synthetic reference workload",
		BaseURL:      "http://127.0.0.1",
		Users:        10,
		Duration:     "5s",
		Timeout:      "1s",
		MaxRPS:       50,
		Mode:         "arrival",
		ArrivalRate:  2,
		P95LimitMS:   500,
		MaxErrorRate: 0,
		Steps: []config.Step{{
			Name:   "work",
			Method: "GET",
			Path:   "/work",
		}},
	}
	validated, err := config.Parse(scenario, config.Options{Authorized: true})
	if err != nil {
		return "", err
	}
	return validated.WorkloadFingerprint, nil
}
