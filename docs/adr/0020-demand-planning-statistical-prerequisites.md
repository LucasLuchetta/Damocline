# ADR 0020 — Gate demand assessment on available statistical evidence

## Status

Accepted as a conservative prerequisite; dependence-aware capacity certification remains open.

## Context

The offline `plan` command previously used empirical phase PASS results alone. A campaign could meet its configured observed thresholds with too few samples to support even the existing conditional P95 and error-rate bounds. Aggregate results could also conceal an undersampled or degraded observation window.

## Decision

Before reporting `GOAL_MET_UNDER_TESTED_CONDITIONS`, require every non-warm-up phase to include:

- an existing two-sided approximate P95 order-statistic-rank interval whose upper latency bound meets the request p95 objective;
- an existing one-sided 95% binomial upper error bound that meets the configured nonzero error budget;
- the same checks for each named request step and every SLO window;
- contiguous SLO windows covering the complete observation horizon; and
- descriptive observation variability with a record for each independent repeat.

If any evidence is absent, malformed, undersampled, or outside its objective, return `INSUFFICIENT_EVIDENCE`. The positive assessment includes a limitation that the bounds assume independent, stationary observations. It does not claim the assumption is established or that run-to-run spread is a confidence interval.

## Consequences

Fifty error-free requests cannot establish a 1% error budget at the one-sided 95% binomial level. Campaigns with short or missing SLO windows, insufficient P95 tail observations, absent recovery evidence, or incomplete repeat spread remain inconclusive for demand planning. This gate is additional to explicit per-phase `VALID_WITHIN_ENVELOPE` measurement quality.

## Limitations and safety

The implementation uses existing report intervals and counts and does not retain raw request data. It does not test interval coverage on correlated samples, establish stationarity, model journey-level SLOs, or provide a dependence-aware run-level confidence interval. Current engine quality remains `LIMITED`, so no current campaign can pass the preceding data-quality prerequisite. No external target traffic or infrastructure recommendation is added.
