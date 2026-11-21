# ADR 0180: Continue capacity from unresolved explore levels and retry a partially failed pre-load baseline

## Status

Accepted

## Context

In a pilot of `damocline capacity`, explore passed four levels twice each,
failed the next ladder level, and then its first refinement probe ended
INCONCLUSIVE because the probe's pre-load network-path baseline (ADR 0153)
completed 19 of 20 requests. The single failure happened right after the
previous overloaded probe. Explore therefore ended INCONCLUSIVE without a
recorded highest passing level, and capacity exited with "explore recorded
no reproducible passing capacity bracket", discarding levels that had
passed every repeat.

## Decision

- When explore stops ABORTED or INCONCLUSIVE after at least one level passed
  every repeat, the explore artifact records that level as
  `highest_passing_tested_users` and adds a note that the stopped level and
  everything above it are unresolved, with the stopping reason. No failing
  level is recorded for an unresolved probe.
- `damocline capacity` continues its sustained campaign and step-down from
  that level for both outcomes and states in its output and HTML report
  facts that levels above it are unresolved and why. Nothing above it is
  presented as passing or failing.
- Before load, the engine retries the bounded network-path baseline exactly
  once, after a 2s settle wait, when the first attempt had at least one
  transport failure, at least one completed request, and no request left
  unattempted by the wall budget. The recorded baseline is the retry, with
  `retried_after_failures` holding the discarded attempt's failure count.
  Both attempts share the existing 30s baseline wall budget; a retry cut
  short by that budget records not-attempted requests and still limits the
  run.
  Recovery-gate probes between campaigns (ADR 0179) are not retried.

## Rationale

A retry was chosen over tolerating one failure in 20. A tolerance would
accept a 5% failure rate on every run, including a path that is genuinely
lossy, and it changes the meaning of an already-recorded field. A retry
keeps the existing all-requests-complete rule: a transient failure after a
loaded stage is followed by a clean second attempt, while a genuinely bad
path fails the retry as well and keeps invalidating the probe. A baseline
with no completed request or one that exhausted its wall budget is not
retried, because it is already strong evidence of a bad path and a retry
would only double the wait.

## Consequences

- The worst-case pre-load baseline time is unchanged, so capacity's planned
  wall time and stage overheads are unchanged.
- A retried baseline is visible in run reports through
  `retried_after_failures`; consumers that ignore unknown fields are
  unaffected.
- Capacity can now conclude from a lower repeatedly passing level when
  higher levels are unresolved. The conclusion stays bounded by that level
  and never extrapolates upward.
