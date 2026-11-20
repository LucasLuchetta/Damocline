# ADR 0164: List actionable audit decision blockers

Keep the existing thresholds and conservative decision states. For readable
campaign artifacts, collect independent decision failures instead of returning
after the first failed gate. Group distinct findings under configuration and
scenario, insufficient evidence, and Damocline limitations. Pair each finding
with an action. Retain the existing findings field and add structured blockers
without changing the interpretation of earlier artifacts.

An unreadable artifact must first pass the bounded reader. Missing counters
cannot support statistical calculation. Repeated failures of the same gate
are listed once per series type. The phase and window ledgers retain the
individual observations.

## Calibration and load-envelope blockers

Required phases whose recomputed quality is not valid are also screened for
their generator calibration state (none attached, malformed, failed,
older than the 24-hour review threshold, recorded for a different generator,
controls or workload, or not covering the phase level) and for invalid or
`GENERATOR_LIMITED` extended-envelope evidence. Each state is reported once
with every affected load level and a remediation, for example running
`damocline calibrate` for those levels. Phases with valid recomputed quality
are not blocked by these screens; the quality assessment stays authoritative.
