# ADR 0036: Bound report readers to the current schema

## Status

Accepted (experimental).

## Context

The engine emits versioned JSON run reports, while local consumers need to retain compatibility with supported historical reports and reject future schemas they have not reviewed. After schema v13 added step and profile window ledgers, the insights reader still capped accepted reports at v12 and rejected valid current output.

## Decision

Define `report.CurrentSchemaVersion` as the emitted report schema and use it for the engine version and the insights reader's upper-bound check. Add deterministic reader tests that preserve schema-v13 window data and reject the next unknown version. Keep historical version fixtures to guard existing compatibility.

## Consequences

Readers do not silently accept unknown future semantics. This fixes the v13 reader regression but does not provide a full migration map or golden compatibility suite for all supported versions. No defaults convert absent historical fields into measured zeroes.
