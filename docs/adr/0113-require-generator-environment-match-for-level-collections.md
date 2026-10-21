# ADR 0113: Require generator-environment matches for campaign level collections

## Status

Accepted

## Context

Offline campaign collections can compare multiple tested workload levels. They
already require matching declared workload, application revision, target,
objectives, and generator controls. A change in the executable, Go runtime,
architecture, logical CPU availability, GOMAXPROCS, or effective CPU
entitlement can also change delivered load and observed latency, but the
collection did not require those recorded conditions to match.

## Decision

For each campaign level, compare the path-free run identity and generator CPU
capacity fields across every required phase, excluding warm-up: OS,
architecture, Go version, runner identity method/value, logical CPU count,
GOMAXPROCS, effective CPU capacity, and capacity source. Require the resulting
environment signature to be known and identical across all levels before
aggregating observed passing/failing levels. Expose `CONSISTENT`, `CHANGED`, or
`UNKNOWN` in the collection assessment. Preserve every per-level assessment
when aggregation is blocked.

## Consequences

- Campaign levels measured by changed or unidentified generator environments
  remain individually reviewable but cannot form a combined level collection.
- Older reports without complete generator provenance remain readable and are
  explicitly `NOT_COMPARABLE` as a multi-level collection.
- Matching metadata establishes recorded process/runtime consistency only. It
  does not authenticate the report, identify the physical host or OS image,
  prove equal external contention, or establish target/application equivalence.
- Effective CPU capacity can change dynamically. A change across any required
  phase blocks aggregation rather than being averaged or silently ignored.
