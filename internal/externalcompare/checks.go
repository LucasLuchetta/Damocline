package externalcompare

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/LucasLuchetta/Damocline/internal/report"
)

type checkCounts struct {
	Name   string   `json:"name"`
	Passes *float64 `json:"passes"`
	Fails  *float64 `json:"fails"`
}

func checkName(fingerprint string, step int) string {
	return "damocline:v1:" + fingerprint + ":" + strconv.Itoa(step)
}

func namedChecks(raw json.RawMessage) []checkCounts {
	var list []checkCounts
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var keyed map[string]checkCounts
	if json.Unmarshal(raw, &keyed) != nil {
		return nil
	}
	list = make([]checkCounts, 0, len(keyed))
	for name, entry := range keyed {
		if entry.Name == "" {
			entry.Name = name
		}
		list = append(list, entry)
	}
	return list
}

func validCount(value float64) bool {
	return value >= 0 && value < 1<<53 && value == math.Trunc(value)
}

// businessCheckEvidence treats observed failed checks as a veto. Approval
// requires the generated workload contract and one passing check per request.
func businessCheckEvidence(run report.Result, k K6Summary) (failed, complete bool) {
	failed = k.checkFailures > 0
	for _, check := range k.namedChecks {
		if check.Fails != nil && *check.Fails > 0 {
			failed = true
		}
	}
	fingerprint := run.Config.WorkloadFingerprint
	if fingerprint == "" || len(run.Steps) == 0 || len(k.namedChecks) != len(run.Steps) ||
		!k.hasCheckTotals || !validCount(k.HTTPReqs) || k.HTTPReqs <= 0 || k.checkPasses+k.checkFailures != k.HTTPReqs {
		return failed, false
	}
	expected := make(map[string]bool, len(run.Steps))
	for index := range run.Steps {
		expected[checkName(fingerprint, index)] = true
	}
	var passed, failures float64
	minSamples := float64(max(1, run.Config.MinSamplesPerStep))
	for _, check := range k.namedChecks {
		if !expected[check.Name] || check.Passes == nil || check.Fails == nil ||
			!validCount(*check.Passes) || !validCount(*check.Fails) || *check.Passes+*check.Fails < minSamples {
			return failed, false
		}
		delete(expected, check.Name)
		passed += *check.Passes
		failures += *check.Fails
	}
	return failed, passed == k.checkPasses && failures == k.checkFailures
}
