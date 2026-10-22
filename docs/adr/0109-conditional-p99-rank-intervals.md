# ADR 0109: Record conditional p99 rank intervals when samples permit

## Status

Accepted.

## Context

Run reports exposed nearest-rank p99 point estimates without a sample-adequacy state. A small number of observations can make a p99 estimate depend on only one or two requests, while a precise-looking point estimate alone gives no indication of that limitation. The existing exact p95 order-statistic interval implementation is bounded and already states its IID/stationarity assumption.

## Decision

Advance run report schema to v32. Apply the same exact binomial order-statistic construction to p99, requiring at least ten expected observations in each binomial tail and retaining the one-million-sample resource bound. Persist p99 method, confidence, and assumption metadata. Record interval endpoints only when the exact interval is available and expose `INSUFFICIENT_SAMPLES`, `ANALYSIS_RESOURCE_LIMIT`, or `CONDITIONAL_IID_INTERVAL` in human-facing insights and capacity reports. Preserve the descriptive nearest-rank p99 point estimate in all cases.

## Validation

Deterministic tests cover the 999/1,000 sample boundary, the upper resource bound, schema-v31 compatibility, current-schema method validation, and human-readable sample-state output. Schema-v32 golden reports exercise historical reader round trips. The exact interval calculation reuses the method validated for p95 in ADR 0100.

## Consequences

The report makes a basic order-statistic sample screen reproducible and reviewable. It does not test independence or stationarity, model temporal or actor-level dependence, establish workload representativeness, or define a p99 service objective. Insufficient-sample p99 point estimates remain descriptive and must not be treated as reliable tail guarantees.
