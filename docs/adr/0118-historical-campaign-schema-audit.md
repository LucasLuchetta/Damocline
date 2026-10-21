# ADR 0118: Preserve historical campaign evidence without upgrading it

## Status

Accepted

## Context

Closed-loop campaign schemas evolved additively. Campaign schemas v2 and v4 predate journey-throughput spread and repeat-level robust p95 fields. Their embedded run reports also lack fields required for current quality and stability assessments. Comparing decoded structs as if every zero-valued field had been persisted made valid historical JSON appear altered, while applying current phase classifiers could reject old phase classifications.

## Decision

Keep historical campaign schemas readable and validate their recorded schedule and phase labels under the rules available to those schemas. For schemas v2 and v4, treat recorded `PASS_OBSERVED` phase labels as historical observations and require `NO_LARGE_DRIFT_DETECTED` on observation/soak phases; do not recompute them into a modern pass claim. Unknown or failed historical phase labels are rejected for a campaign that claims `PASS`. Schema v3 and newer retain their stricter recomputation gates. Compare only persisted spread fields. Schema v2 journey-throughput spread is `NOT_RECORDED`, not zero. Later-added robust repeat-p95 fields are never synthesized for older schemas. Independently recompute current run quality and stability where possible; missing evidence leaves the audit inconclusive or limited and never upgrades a historical campaign to capacity certification.

## Consequences

Offline readers can inspect supported v2/v4 campaign artifacts and preserve exact source-byte hashes. Reports remain conservative when modern generator, journey-window, stability, or workload evidence is absent. Historical `PASS` labels describe the producer's observed criteria and do not imply that current audit criteria were met.

## Validation

Checked-in synthetic loopback fixtures cover campaign schemas v2 and v4 through bounded JSON reading, structural validation, spread comparison, and offline audit. Tests require the historical audit verdict to remain `INCONCLUSIVE`, verify schema-v2 journey throughput is `NOT_RECORDED`, and verify schema-v4's persisted journey-throughput spread is recomputed and matched.
