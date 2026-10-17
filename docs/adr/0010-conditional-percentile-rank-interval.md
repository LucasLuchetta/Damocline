# ADR 0010: Report conditional percentile-rank uncertainty without raw samples

## Status

Accepted.

## Decision

For a metric set with enough observations, report an approximate two-sided 95%
interval for P95 using normal-approximation binomial order-statistic ranks. The
metric snapshot maps those ranks to the sorted in-memory latency observations,
then discards the raw durations as before. The interval is emitted only when
both expected binomial groups contain at least ten observations.

Every presentation labels the interval as an IID-assumption approximation. It
does not enter pass/fail evaluation and does not turn request samples from one
VU, deployment period, or time window into independent evidence.

## Consequences

Schema v6 adds lower/upper P95 values and an explicit approximation flag while
remaining able to read earlier reports. This gives a sample-adequacy signal
without retaining potentially sensitive latency samples in the JSON report.
Dependence-aware methods, confidence analysis across independent campaigns,
and statistical capacity certification remain future work.
