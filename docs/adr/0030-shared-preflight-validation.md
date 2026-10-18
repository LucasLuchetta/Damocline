# ADR 0030: Share preflight validation with run reports

## Status

Accepted (experimental).

## Context

The loopback calibrator validated its versioned artifact inside its command package. The HTTP run command could not reuse that validation and reports did not preserve which local generator evidence had been considered. Copying a passing calibration status into a run without checking the artifact's ledgers would overstate evidence.

## Decision

Move preflight record and scheduler-lag schema validation into the shared internal `calibration` package. The run command may read a bounded local artifact, validate the complete preflight record, stage reconciliation and scheduler-lag observations, and compare declared load controls against the reference fixture. It stores only sanitized preflight metadata and named dimension statuses, never the calibration file path, target URL, credentials, routes or response data.

Declared matches remain distinct from observed equivalence. The current fixed fixture cannot establish endpoint identity or the target's response-size/timing distribution; reports must retain those dimensions as `UNVERIFIED`, and data quality remains `LIMITED` or `INVALID` when applicable. No authenticated host identity or cryptographic artifact signature is claimed.

## Consequences

The same bounded validator now protects offline calibration inspection and run attachment. Missing, malformed, oversized or inconsistent artifacts cannot be attached. This is partial G0-02/G0-08/G0-16 progress and does not close any of those gates. Workload-shaped engine calibration, authenticated host equivalence, direct response-shape evidence, campaign integration and statistical sufficiency remain open.
