# ADR 0047: Persist scheduler-admitted arrival counts

## Status

Accepted.

## Context

Open-loop reports persisted planned arrivals and worker-started iterations, but omitted the scheduler's successful handoff count. The runtime already tracked admitted slots. Without persisting them, a report could not independently verify whether work accepted by the scheduler reached the journey worker.

## Decision

Add pointer-backed `arrivals_admitted` in report schema v17. Require its presence (including observed zero) for v17 and reject it in earlier schema versions. For arrival-mode reports, quality recomputation requires planned arrivals to equal scheduler admissions plus all rejected/drop categories, and admissions to equal worker-started iterations. Closed-loop reports must record zero scheduler admissions. Preserve v1-v16 quality semantics and reader compatibility.

## Consequences

Reports can audit the scheduler-to-worker boundary separately from journey and request completion. Missing historical admission evidence remains unknown. This proves count conservation only; it does not establish timing accuracy, endpoint receipt, or root cause for a failed admission.
