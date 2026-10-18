# ADR 0054: Fail closed on unknown calibration-stage quality

## Status

Accepted.

## Context

The loopback calibrator checks each engine run's delivery, request and journey outcomes, response shape, latency, generator health, and scheduler lag. It also persists the report's measurement-quality state. Previously, a missing or future unrecognized quality state could avoid the explicit invalid-state check and still pass when the separate fixture checks succeeded.

## Decision

Every healthy reference stage must contain a recognized measurement-quality state. `VALID_WITHIN_ENVELOPE` and `LIMITED` are recognized; `INVALID`, missing, or unknown states fail calibration with a stage-specific diagnostic. `LIMITED` remains acceptable because the fixture calibrator applies its own bounded reference gates and its general stage runs do not by themselves establish application-workload equivalence. A calibration pass is not a positive quality claim for an arbitrary application run.

## Consequences

Calibration cannot silently accept an invalid, absent, or future-incompatible quality state. The controlled soak retains its separate recomputed positive-quality gate. This improves fail-closed artifact handling but does not add workload equivalence, host attestation, or application-capacity certification.
