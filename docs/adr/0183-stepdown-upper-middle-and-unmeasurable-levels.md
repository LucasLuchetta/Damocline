# ADR 0183: Step-down upper-middle choice and unmeasurable levels

## Status

Accepted

## Context

In a pilot of `damocline capacity`, explore passed 25, 50, 100 and 150 VUs
twice each and failed 175 and 200. The sustained campaign at 150 VUs FAILED.
Two problems followed:

1. The bisection (ADR 0179) picked 50 VUs instead of 100. It chose by index,
   and with three candidates the index midpoint is the lower middle.
2. The campaign at 50 VUs was recorded `NOT_RUN` because its sample-sized
   window exceeded the 60s ceiling, and the ladder stopped, so no sustainable
   level was reported although 100 VUs had never been tried.

The 60s ceiling is part of the campaign objective contract
(`internal/campaign/objectives.go`): every observation window must be 1s-1m
and contain at least the per-window minimum samples (zero-error and p95-rank
bounds). Pooling samples across windows or lengthening windows past 60s would
weaken the per-window stability criteria, so neither is done here.

## Decision

- The next step-down level is chosen by value: take the ceiling of the
  midpoint between the lower bound (last passing or excluded level, otherwise
  the lowest candidate) and the lowest failing level, then try the lowest open
  candidate at or above it; if none is, try the highest open candidate. For
  candidates 25/50/100 below a FAIL at 150 the first step-down is 100.
- A level that cannot be planned honestly (window over 60s or campaign wall
  budget too small) is recorded `NOT_RUN` with its reason. It and every lower
  candidate are excluded, because lower levels deliver fewer samples per
  second and need even longer windows. The bisection continues over the
  remaining higher candidates within the same bounded number of step-downs
  instead of stopping. An excluded level never counts as a pass.

## Consequences

- The search reaches the highest plausible levels first.
- A low level that cannot meet the per-window sample floor no longer hides a
  measurable higher level. The minimum-sample guarantees are unchanged.
- When every remaining candidate is unmeasurable, no sustainable level is
  reported and the ladder records why.
