# ADR 0085: Overlay HTTP and external metrics on a phase UTC timeline

## Status

Accepted (experimental).

## Context

ADR 0084 added a loopback dashboard for saved run and campaign phase evidence,
but HTTP observation windows and external application metrics appeared on
separate time representations. This made manual temporal comparison harder.

## Decision

Render each HTTP observation-window p95 at the UTC midpoint of its elapsed
window, alongside timestamped external metric points on one phase-local UTC
axis. Keep the series visually distinct and use independent value scales. Store
new run start timestamps with subsecond precision to reduce avoidable rounding
when converting elapsed windows to UTC.

## Consequences

The view supports temporal inspection but does not correct clock offsets,
estimate clock uncertainty, interpolate sparse samples, map external metrics to
SLOs, or establish causal relationships. Older reports may have only
second-resolution start timestamps. HTTP windows with no completed requests do
not contribute a p95 point. Campaign phases continue to be rendered separately.
