# ADR 0123: Reject missing persisted metric counters

## Status

Accepted

## Context

Run and campaign JSON is decoded into Go structs whose integer fields default
to zero. A malformed or truncated artifact could omit request `total`,
`success`, or `failures` and still be rendered as a run with zero requests.
That made absence of evidence indistinguishable from an explicitly measured
zero in the report, dashboard, capacity goal, and export paths.

## Decision

Before decoding report evidence into typed results, require explicit
non-negative integer `total`, `success`, and `failures` fields in every
serialized metric set.
Apply the same validation to standalone reports and reports embedded in
campaign phases and exploration probes. Accept explicit zero counters; reject
missing fields as incomplete evidence. Keep optional diagnostics optional and
continue to use each artifact's schema version for fields introduced later.

## Consequences

- Run, campaign, and exploration evidence cannot silently turn omitted request
  counters into favorable zero-valued measurements.
- Legacy schema fixtures remain readable when they contain their original
  required counters; tests remove each counter across all supported run schemas
  and require rejection.
- The shared validation runs in run inspection, campaign assessment,
  dashboard, and redacted-export entry points.
- The JSON presence check covers the three base request counters' presence
  and token type. It does not replace
  schema-specific validation for other fields or authenticate the report
  producer.
