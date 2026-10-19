# ADR 0081: Run explicit synthetic cleanup after cancellation

## Status

Accepted.

## Context

Configured final DELETE steps already run after ordinary business assertion
failures, but the engine's run context cancellation interrupted the journey
before those steps. A write workflow could therefore leave synthetic records
behind when its operator cancelled a run.

## Decision

Add an explicit `cleanup: true` step marker. Marked steps must form a final
suffix, use DELETE, and target `{{synthetic_id}}` with an environment-backed
synthetic prefix configured. They are skipped during ordinary workflow
execution and run after business steps, including after external cancellation
if a mutating business request was attempted. Cancellation cleanup uses a
detached context limited to the remaining run drain deadline and at most 30
seconds. It continues to use the shared request-rate governor. Run report
schema v24 records which steps are cleanup steps.

## Consequences

Explicit cleanup is scoped to an identifier generated for the current journey
and remains visible in per-step request metrics. It is best-effort: process
termination, unavailable identifiers, exhausted drain time, and failed DELETE
requests can leave data behind. Cleanup does not establish idempotency or
application-specific reset semantics.
