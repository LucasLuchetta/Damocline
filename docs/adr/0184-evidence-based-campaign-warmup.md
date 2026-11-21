# ADR 0184: Evidence-based campaign warm-up settling

## Status

Accepted

## Context

In a pilot of `damocline capacity`, a step-down campaign at 100 VUs used the
fixed 10s warm-up, then a 1m39s observation in three 33s windows. Over the
whole observation every step p95 was within its 1000ms SLO, but in the first
window one step had a p95 of 1290ms, so the campaign FAILED. The later
windows were within the SLO. The previous campaign at a higher level had
ended about a minute earlier, and all VUs start together, so the first window
still held the start-up transient. A fixed 10s warm-up is too short for a
workload whose journey cycle is several seconds long.

Two approaches were considered:

1. An adaptive loop of short separate warm-up runs that stops when
   consecutive p95 values settle. Each native run carries its own network
   preflight and a bounded drain allowance in the wall-time budget. Several
   extra runs per trial would exceed the default 12m per-level capacity
   budget for realistic levels.
2. One windowed warm-up run that is sized from the level, followed by a
   settling verdict on its own windows.

## Decision

- `campaign` gets an optional warm-up settling policy (`--warmup-settle-window`,
  `--warmup-settle-tolerance-percent`, `--warmup-settle-tolerance-ms`,
  `--warmup-settle-consecutive`; the per-series sample floor is
  `--stability-min-samples`). When it is set, the warm-up run is split into
  settling windows. Request, journey and step p95 are compared between
  consecutive complete windows, and a series is compared only when it has at
  least the sample floor. Request p95 must always be comparable. A
  comparison is within tolerance when
  `|current - previous| <= max(previous * percent / 100, ms)`.
- The warm-up is `STABILIZED` only when its last `consecutive` comparisons are
  all within tolerance. The verdict records each window's p95 values, the
  largest change and the time at which the stable tail began. A short tail is
  never extrapolated.
- If the warm-up does not settle (`NON_STATIONARY`), or has too few windows
  (`INSUFFICIENT_WINDOWS`), the trial ends `INCONCLUSIVE` before any judged
  phase starts, and the campaign note gives the reason. This is never a PASS.
- Warm-up windows are never judged against the SLO. Observation, recovery and
  the other judged phases keep their window-by-window judgement unchanged.
  No breaching observation window is dropped or averaged away.
- `damocline capacity` enables settling with 25% / 50ms tolerance and two
  consecutive comparisons. Each settling window is a third of the level's
  sample-sized observation window, clamped to 5s-15s. The warm-up is the
  scenario ramp plus three per-VU journey cycles measured at that level in
  explore (users x elapsed / completed journeys), rounded up to whole
  settling windows. It is at least three settling windows (and at least the
  previous 10s floor) and at most 90s. It is shortened toward that floor
  only as far as the wall-time cap requires. The plan and dry run print the
  warm-up, its basis, its bounds and the settling rule, and the existing
  minimum-budget check still applies.
- The offline audit recomputes the settling verdict from the saved warm-up
  windows and the saved policy. It rejects an altered or removed verdict,
  and it accepts an unsettled warm-up only as the terminal phase of an
  INCONCLUSIVE trial.
- capacity.md and capacity.html explain the rule and each trial's settling
  verdict in plain language.

## Consequences

- Campaigns without a settling policy keep their behaviour and evidence. The
  schema version does not change: the new fields are optional.
- The warm-up length is bounded and planned in advance, not stopped early.
  The time to stabilize is reported, but the wall time does not shrink when
  the target settles quickly.
- Each judged phase is still a fresh native run in which all VUs start
  together, so a short synchronized burst at the start of observation stays
  possible. A warmed target absorbs it better, but the first observation
  window can still breach and the campaign then still FAILs, as it should.
