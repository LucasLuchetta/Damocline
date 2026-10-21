# ADR 0127: Reject missing campaign evidence fields

## Status

Accepted

## Context

Campaign reports are decoded into Go structs, where omitted fields become zero
values. Semantic validation catches many resulting inconsistencies, but not
every missing required top-level field. In particular, a current campaign with
an omitted or malformed start timestamp could be accepted as if its timestamp
were simply unavailable. Historical campaign schemas have different recorded
timestamp semantics and must remain readable.

## Decision

Before typed decoding, require campaign schema version, scenario, base user
count, target, start timestamp field, warmup, observation, recovery, repeat
count, trials, and outcome to be present and non-null. For the current campaign
schema, require `started_at` to be a non-empty RFC3339 timestamp. Preserve the
existing historical schema behavior for timestamp values, including empty
values that were recorded by older fixtures.

Apply the same bounded structural check to individual campaign reads and
campaign collections. Keep explicit zero durations valid when their JSON fields
are present.

## Consequences

- Missing required top-level fields can no longer silently become Go zero
  values during offline campaign assessment.
- Current campaign start times have a reproducible timestamp representation;
  historical fixtures remain readable under their original schema semantics.
- This check validates structure and timestamp syntax only. It does not
  authenticate report producers, verify clocks, or certify application
  capacity.
