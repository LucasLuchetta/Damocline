# ADR 0133: separate HTTP token wait from request execution and preparation

## Status

Accepted.

## Context

The engine set each admitted step's rate-wait duration after executing HTTP,
which included request construction, transport and response work in a field
documented as token waiting. The same error affected explicit cleanup steps.
The run clock also started before collecting executable provenance, so local
identity I/O could shift the reported observation origin.

A delayed-close regression used a 350 ms workload and 200 ms drain allowance
with a first request token due at 500 ms. That left almost no room for the
intended 35 ms transport failure and could record cancellation instead.

## Decision

Capture each successful token wait immediately at admission, before request
execution, in both normal and cleanup paths. Cancelled token waits retain their
existing separate count and measured wait. Request and journey timing otherwise
keep their documented boundaries.

Resolve executable identity before starting the observation clock. Reported
elapsed time still includes execution and drain, while provenance preparation
is outside the load observation. The HTTP governor, configured duration,
request timeout, write authorization and drain ceilings are unchanged.

Use one explicitly configured open-arrival request for the delayed-close
fixture, a one-second scheduling window, a 100 request-starts/s ceiling and
the same 200 ms request timeout. Preserve exact connection/attempt counts,
transport failure classification, known-delay screening and serialization.

Regression tests establish that token admission precedes independent server
receipt, including cleanup after cancellation. They use monotonic event-order
bounds rather than a tight host timing tolerance. An injected 120 ms identity
provider verifies that preparation is excluded from observed elapsed time,
allowing two milliseconds for the existing report rounding.

## Consequences

Rate-wait totals no longer double-count HTTP execution. Current observation
timestamps exclude identity lookup; historical summaries are preserved and
cannot be retroactively corrected from their aggregated values.

These checks define software boundaries, not physical clock accuracy or broad
timing calibration under host pressure. Phases reported by HTTP tracing can
still overlap and must not be summed as disjoint network/server intervals.
