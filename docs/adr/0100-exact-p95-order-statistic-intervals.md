# ADR 0100: Use exact binomial ranks for p95 intervals

## Status

Accepted.

## Context

The existing p95 uncertainty interval selected order-statistic ranks with a normal approximation to the binomial distribution. A deterministic coverage calculation found that this approximation covered only about 93.35% at n=230 for a nominal 95% interval. The report did not persist the percentile estimator or identify that approximation as the method used, making offline review depend on implementation knowledge.

## Decision

Keep the exact in-memory nearest-rank percentile estimator and state it explicitly. Replace the new-report p95 rank interval with a conservative central interval selected from the exact binomial order-statistic distribution: choose ranks so each excluded tail probability is at most 2.5%. Require at least ten expected observations in each binomial group, retain the existing IID/stationarity assumption, and cap interval calculations at one million samples. If the resource cap or sample screen is exceeded, omit the interval and report insufficient evidence.

Advance HTTP run reports to schema v30. Persist the percentile estimator, p95 interval method, confidence level, and IID/stationarity assumption. Mark exact intervals per metric set; continue to read historical schema-v6 through v29 intervals with their original approximation flag. Reject current-schema reports with missing or altered method metadata.

## Validation

Deterministic tests check known rank results and nearest-rank boundary cases. A fixed-seed IID-uniform simulation checks nominal coverage; a fixed-seed block-correlated simulation shows that the same IID interval under-covers dependent samples. The rank construction follows the binomial order-statistic interval in [NIST Technical Note 2119, Section 5.3](https://doi.org/10.6028/NIST.TN.2119).

## Consequences

New intervals avoid normal-approximation coverage error for IID samples and reports disclose the method. They do not handle temporal or actor-level dependence, prove stationarity, or certify capacity. Historical approximate intervals remain interpretable but retain their weaker method. The computation is bounded and may omit intervals above one million samples.
