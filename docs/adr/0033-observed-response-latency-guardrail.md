# ADR 0033: Screen observed request latency against the local fixture guardrail

## Status

Accepted (experimental).

## Context

The fixed local reference fixture has a 20 ms response delay and the preflight runner already rejects request and journey p50/p95 values outside 15–150 ms. A run with attached calibration previously exposed response-size comparison but did not report whether its per-step request percentiles crossed that coarse reference guardrail.

## Decision

For schema-v12 runs with attached validated calibration, derive `observed_response_latency` from every named step's client-observed request p50 and p95. Require at least 100 successful observations for each step; otherwise report `UNVERIFIED`. Report `OUTSIDE_REFERENCE` when a sufficiently sampled step's p50 or p95 falls outside the inclusive 15–150 ms fixture guardrail. Report `MATCHED` only when every named step meets the sample floor and both percentiles remain inside the guardrail. Recompute the dimension from the report fields during quality assessment. The sample floor is a screening rule, not statistical sufficiency or a confidence claim. Overall quality remains LIMITED.

## Consequences

The comparison exposes a coarse client-observed timing signal without asserting server processing time or identifying a cause. Aggregate per-step percentiles can hide degraded observation windows; temporal dependence, response distributions, endpoint identity and workload equivalence remain unknown. This dimension cannot certify a sustainable capacity result.
