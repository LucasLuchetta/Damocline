# ADR 0031: Record bounded response-body shape observations

## Status

Accepted (experimental).

## Context

The shared local preflight validator could verify its own fixed fixture and declared scenario dimensions, but HTTP run reports did not record actual response-body lengths. As a result, reports could not expose an observed per-step response-size range or reconcile those body reads with the HTTP response ledger. Retaining bodies would add sensitive values and unbounded report data.

## Decision

Schema-v12 run reports record complete, interrupted and oversized response-body read counts per step, aggregate, and observation window. For complete bodies within the existing 2 MiB read bound, record minimum, maximum and total bytes returned by Go's response-body reader. Do not persist body contents, partial-read byte counts, or the truncated prefix for oversized responses. Validate that body outcomes reconcile with observed HTTP statuses, oversized counts match the existing HTTP diagnostic, complete size totals are arithmetically plausible, and per-step summaries reconcile with the aggregate.

## Consequences

Reports can show the response-size observations made in a specific run without disclosing values from the response. These observations do not establish an expected response-shape envelope, representative traffic distribution, endpoint identity, transport equivalence or generator-host equivalence. Older report schemas do not carry the data and must treat it as unknown. `VALID_WITHIN_ENVELOPE` remains unavailable until independent workload-shaped calibration and the remaining measurement-quality gates are implemented.
