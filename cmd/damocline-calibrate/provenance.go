package main

import (
	"runtime"
	"time"
)

// Explicit provenance checks support G0-08. They do not claim host identity:
// two machines can have identical runtime versions and CPU layouts.
type localRuntime struct {
	OS string
	Arch string
	GoVersion string
	LogicalCPUs int
	GOMAXPROCS int
	RunnerVersion string
}

func currentRuntime() localRuntime {
	return localRuntime{
		OS:runtime.GOOS, Arch:runtime.GOARCH,
		GoVersion:runtime.Version(), LogicalCPUs:runtime.NumCPU(),
		GOMAXPROCS:runtime.GOMAXPROCS(0), RunnerVersion:sourceRunnerVersion(),
	}
}

// Append provenance findings to a sanitized offline comparison. A calendar
// horizon is a conservative policy warning, NOT a scientifically established
// calibration lifetime or sufficient criterion for freshness.
func (r *coverageAssessment) checkProvenance(a assessment, err error, now time.Time, local localRuntime) {
	if err != nil {
		r.add("calibration_age",coverageUnknown,"No valid calibration artifact is available to establish its age.")
		r.add("runtime_compatibility",coverageUnknown,"No valid calibration artifact is available for runtime comparison.")
		r.add("host_identity",coverageUnknown,"This format does not attest the generator host's identity.")
		r.Shape="NOT_ESTABLISHED"
		return
	}
	finished,parseErr:=time.Parse(time.RFC3339Nano,a.Preflight.FinishedAt)
	if parseErr!=nil {
		r.add("calibration_age",coverageUnknown,"Calibration completion time is unavailable.")
		r.Shape="NOT_ESTABLISHED"
	} else if age:=now.Sub(finished);age < -5*time.Minute {
		r.add("calibration_age",coverageOutside,"The calibration ends in the future beyond the allowed clock-skew window.")
		r.Shape="NOT_ESTABLISHED"
	} else if age>24*time.Hour {
		r.add("calibration_age",coverageOutside,"Calibration is older than the conservative 24-hour review threshold; repeat it on the generator.")
		r.Shape="NOT_ESTABLISHED"
	} else {
		r.add("calibration_age",coverageMatched,"Calibration is within 24 hours; time proximity alone does not establish equivalence.")
	}
	g:=a.Preflight.Generator
	if g.OS!=local.OS || g.Arch!=local.Arch || g.GoVersion!=local.GoVersion ||
		g.LogicalCPUs!=local.LogicalCPUs || g.GOMAXPROCS!=local.GOMAXPROCS ||
		(g.RunnerVersion!="source-development" && local.RunnerVersion!="source-development" && g.RunnerVersion!=local.RunnerVersion) {
		r.add("runtime_compatibility",coverageOutside,"Generator OS, architecture, Go version, CPU layout or known runner revision differs from the calibration record.")
		r.Shape="NOT_ESTABLISHED"
	} else if g.RunnerVersion=="source-development" || local.RunnerVersion=="source-development" {
		r.add("runtime_compatibility",coverageUnknown,"Runtime attributes match but the exact runner source revision cannot be attested.")
	} else {
		r.add("runtime_compatibility",coverageMatched,"Recorded runtime attributes match; this does not prove the same physical/virtual host.")
	}
	r.add("host_identity",coverageUnknown,"No stable authenticated host fingerprint is present; host equivalence is not established.")
}
