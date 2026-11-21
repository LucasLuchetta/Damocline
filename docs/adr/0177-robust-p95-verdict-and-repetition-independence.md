# ADR 0177: Robust p95 verdict and repetition-independence screen

## Status

Accepted.

## Context

Open-arrival campaigns judged the p95 SLO on point estimates. A window whose
p95 point estimate sits just below the limit can pass even though sampling
uncertainty alone could place the population p95 above it. Repetitions were
also treated as exchangeable without checking for warm-up or carry-over that
makes later repetitions systematically slower or faster.

Native run reports already carry an exact distribution-free 95% interval for
p95 from binomial order-statistic ranks (`p95_iid_95_lower_ms` /
`p95_iid_95_upper_ms`) when enough samples exist.

## Decision

Open-arrival campaign schema v11 (extended-envelope v12) persists a
`p95_robustness` verdict on completed campaigns:

- Every judged window (stabilization, observation, soak phases with
  `PASS_OBSERVED`) of every repetition contributes its journey p95 interval.
- A PASS is `ROBUST_PASS` only when every judged window's upper bound is
  strictly below the journey p95 limit. An upper bound at or above the limit
  yields `LIMITED`; a window without an exact interval (too few samples)
  yields `INCONCLUSIVE`. Non-passing campaigns are `NOT_APPLICABLE`.
- A repetition-independence screen computes the Mann-Kendall S statistic over
  per-repetition observation p95s. With at least three repetitions, a
  perfectly monotonic series (|S| = n(n-1)/2) with at least 10% relative
  change between the first and last repetition is `MONOTONIC_TREND_DETECTED`
  and limits the verdict. Fewer repetitions are `NOT_ASSESSABLE`.

The campaign `outcome` is unchanged for compatibility. The offline `goal`
assessor recomputes the verdict from native windows, rejects a saved verdict
that disagrees, and for schema v11+ only reports
`SUSTAINED_TARGET_PASSED_UNDER_TESTED_CONDITIONS` when the verdict is
`ROBUST_PASS`; otherwise it stays `INSUFFICIENT_EVIDENCE` with the reasons.
Earlier schemas remain readable; their recomputed verdict is informational.

## Consequences

- Passing claims need margin below the limit or more samples per window.
- The interval assumes IID, stationary observations; correlated HTTP samples
  can undercover. The trend screen is a heuristic, not a proof of
  independence; small repeat counts have low power and false alarms are
  bounded only by the relative-change threshold.
