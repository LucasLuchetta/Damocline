package main

import (
	"testing"
	"time"
)

func runtimeForCalibration(a assessment) localRuntime {
	g := a.Preflight.Generator
	return localRuntime{OS: g.OS, Arch: g.Arch, GoVersion: g.GoVersion,
		LogicalCPUs: g.LogicalCPUs, GOMAXPROCS: g.GOMAXPROCS, RunnerVersion: g.RunnerVersion}
}

func TestProvenanceMarksFreshnessAndHostUncertaintySeparately(t *testing.T) {
	a := validCoverageCalibration(t, true)
	now := time.Date(2026, 10, 17, 12, 2, 0, 0, time.UTC)
	r := compareScenarioToReference(referenceShapedScenario(), a, nil)
	r.checkProvenance(a, nil, now, runtimeForCalibration(a))
	findDimension(t, r, "calibration_age", coverageMatched)
	findDimension(t, r, "runtime_compatibility", coverageMatched)
	findDimension(t, r, "host_identity", coverageUnknown)
	if r.Status != "INCONCLUSIVE" {
		t.Fatal("runtime similarity must never imply PASS")
	}
}

func TestProvenanceRejectsStaleClockSkewAndDifferentRuntimes(t *testing.T) {
	a := validCoverageCalibration(t, true)
	for _, now := range []time.Time{
		time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 17, 11, 0, 0, 0, time.UTC),
	} {
		r := compareScenarioToReference(referenceShapedScenario(), a, nil)
		r.checkProvenance(a, nil, now, runtimeForCalibration(a))
		findDimension(t, r, "calibration_age", coverageOutside)
		if r.Shape != "NOT_ESTABLISHED" {
			t.Fatal("stale or future evidence must invalidate apparent fixture resemblance")
		}
	}
	local := runtimeForCalibration(a)
	local.GOMAXPROCS++
	r := compareScenarioToReference(referenceShapedScenario(), a, nil)
	r.checkProvenance(a, nil, time.Date(2026, 10, 17, 12, 2, 0, 0, time.UTC), local)
	findDimension(t, r, "runtime_compatibility", coverageOutside)
	if r.Shape != "NOT_ESTABLISHED" {
		t.Fatal("mismatched runtime must invalidate apparent fixture resemblance")
	}
}

func TestProvenanceMissingAndUnattestedVersionRemainUnknown(t *testing.T) {
	a := validCoverageCalibration(t, false)
	r := compareScenarioToReference(referenceShapedScenario(), a, nil)
	local := runtimeForCalibration(a)
	local.RunnerVersion = "source-development"
	r.checkProvenance(a, nil, time.Date(2026, 10, 17, 12, 2, 0, 0, time.UTC), local)
	findDimension(t, r, "runtime_compatibility", coverageUnknown)
	missing := compareScenarioToReference(referenceShapedScenario(), assessment{}, nil)
	missing.checkProvenance(assessment{}, errInconclusive, time.Now(), local)
	findDimension(t, missing, "calibration_age", coverageUnknown)
	findDimension(t, missing, "runtime_compatibility", coverageUnknown)
	findDimension(t, missing, "host_identity", coverageUnknown)
	if missing.Shape != "NOT_ESTABLISHED" {
		t.Fatal("missing provenance cannot establish equivalence")
	}
}
