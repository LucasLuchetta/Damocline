# ADR 0041: Qualify the calibrator-owned reference soak

## Status

Accepted (experimental).

## Context

Run quality correctly remained LIMITED because normal HTTP reports cannot prove that their target is the local reference fixture. The calibrator does own a separate loopback fixture process and can independently observe its request ledger. A positive result is useful only for qualifying that exact controlled execution and must not upgrade ordinary application runs.

## Decision

Add report schema v15 controlled-reference evidence containing the fixed fixture fingerprint and independent received/completed/success/failure/cancel/reject counters for the calibrator's 30-second soak. The calibrator queries a private loopback control endpoint before and after the soak; control requests are excluded from the work ledger. Recompute positive quality from the saved report, calibration record, exact simple-GET workload, response-body observations, descriptive latency floor, generator samples/headroom, current runtime and the independent server ledger. Require every calibration dimension and each required dimension occurrence to match. Ordinary `run --calibration` reports cannot provide this evidence and remain LIMITED. The quality state remains independent of the SLO outcome.

When VCS revision metadata is available, record that revision. Otherwise identify the exact running executable with a SHA-256 prefix. This establishes same-binary identity for the local calibration, not source provenance, physical host identity, or artifact authenticity.

## Consequences

The bounded calibrator soak can emit `VALID_WITHIN_ENVELOPE` when all checks actually pass; its command fails if a passing reference calibration cannot recompute that state. The report validates only this synthetic loopback fixture, runtime, binary, workload and 30-second observation window. It is not evidence of external target capacity, host-class equivalence, temporal independence, or a universal load-generator ceiling. Persisted hashes and counters are consistency evidence, not a cryptographic signature against coordinated artifact edits.
