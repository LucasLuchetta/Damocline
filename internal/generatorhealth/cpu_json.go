package generatorhealth

import "encoding/json"

// UnmarshalJSON retains an absent historical window mean as unknown. Process
// CPU validity can still describe supported adjacent samples and their peak.
// The optional diagnostic survives re-encoding without rewriting source files.
// Each artifact is decoded independently, without inheriting a prior value.
func (s *Summary) UnmarshalJSON(data []byte) error {
	type plain Summary
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var presence struct {
		Mean *float64 `json:"mean_process_cpu_utilization_ratio"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	decoded.MeanProcessCPUUnavailable = decoded.MeanProcessCPUUnavailable || (decoded.ProcessCPUValid && presence.Mean == nil)
	*s = Summary(decoded)
	return nil
}

// MeanProcessCPUAvailable checks source availability, not sampler coverage,
// entitlement stability or headroom. Native summaries have explicit Go values.
func (s *Summary) MeanProcessCPUAvailable() bool {
	return s != nil && s.ProcessCPUValid && !s.MeanProcessCPUUnavailable
}
