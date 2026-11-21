# ADR 0179: Step-down recovery gate and bisection

## Status

Accepted

## Context

ADR 0176 added a bounded step-down to `damocline capacity`: after a
sustained campaign FAILs on latency or errors, the next two lower repeatedly
passing explore levels each run a campaign. In a pilot, every step-down
campaign started immediately after an overloaded campaign and failed, while
separate runs at the same lower levels had passed. Back-to-back campaigns
can be contaminated by carry-over from the previous overload (server
backlogs, queues, garbage collection, connection pools). Walking down one
level at a time also wastes the bounded campaign count.

## Decision

- Before every step-down campaign a recovery gate runs: a fixed 30s
  cooldown, then the scenario's existing bounded, sequential,
  credential-free network-path probe (ADR 0153), repeated every 15s until
  its p50 is at most 2x the pre-load reference p50 + 25 ms, within at most
  3m in total. The reference is the median network-baseline p50 recorded by
  explore at its lowest probed level (falling back to all explore runs).
- If the target does not recover in time, the ladder stops and records
  `NOT_RUN_TARGET_NOT_RECOVERED` with the reason "target did not recover;
  later campaigns would be contaminated" instead of a misleading FAIL. When
  no reference exists (baseline skipped), only the cooldown runs and the
  gate is recorded as `UNVERIFIED`.
- Step-down levels are chosen by bisection over the repeatedly passing
  calibrated explore levels below the failing level, bounded to three
  campaigns. A PASS raises the lower bound, a FAIL lowers the upper bound;
  INCONCLUSIVE or ABORTED evidence stops the ladder. The sustainable level
  is the highest level whose sustained campaign passed.
- `campaign-ladder.json` records each gate (`recovery`) and any
  `stop_reason`. The dry-run plan reserves three campaigns and three
  recovery gates in the declared wall time.
- Reports flag a short-explore pass with a sustained FAIL as a
  time-dependent knee and explain it in plain language as a hypothesis
  (waiting work likely grows over time), not a measured root cause.

## Consequences

Step-down verdicts are less likely to reflect carry-over, and the bounded
campaign count covers more of the level range. The gate measures only the
network-path probe; a target can look recovered on that path while other
state is still degraded, so recovery is evidence, not proof. Protocol wall
time grows by up to 3m per step-down.
