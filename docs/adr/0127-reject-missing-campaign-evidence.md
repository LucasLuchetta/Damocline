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
count, trials, and outcome to be present and non-null. For campaign schema v5
and later, require `started_at` to be a non-empty RFC3339 timestamp; that field
has been authoritative since v5. Preserve v2/v4 fixture semantics, including
their explicitly empty timestamp.

Apply the same bounded structural check to individual campaign reads,
collections, the dashboard, and redacted exports while raw bytes are available.
Require trial objects to contain number, phase array, and outcome, and phase
objects to contain name and a non-null embedded run object. Apply the same rule
to pending checkpoint trials. Each embedded run also requires an explicit
schema marker; metric counters are validated by the shared report-artifact
validator. Empty phase arrays remain valid for an aborted trial that stopped
before recording a phase. Keep explicit zero durations valid when their JSON
fields are present.

## Consequences

- Missing required top-level fields can no longer silently become Go zero
  values during offline campaign assessment.
- Missing trial/phase structure can no longer silently become empty arrays or
  zero-value run reports, including in checkpoint and redacted-report paths.
- Schema-v5+ start times have a reproducible timestamp representation;
  historical v2/v4 fixtures remain readable under their original semantics.
- This check validates structure and timestamp syntax only. It does not
  authenticate report producers, verify clocks, or certify application
  capacity.
