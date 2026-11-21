# ADR 0185: Require an assessable p95 repetition screen

## Status

Accepted.

## Context

ADR 0177 introduced a repetition screen with a minimum of three
observations. A passing campaign could still earn `ROBUST_PASS` with two
repetitions and `NOT_ASSESSABLE`, even if their p95s differed substantially.

## Decision

An otherwise robust p95 pass is `INCONCLUSIVE` when the screen cannot run.
Keep the native campaign outcome and repetition limits. Existing interval
and trend findings continue to qualify a campaign; the screen is heuristic
and the intervals still assume independent, stationary observations.

The offline assessor verifies schema v11 and later reports against native
evidence. It also accepts the original serialized verdict if the unavailable
screen is the sole change and the saved fields match the earlier derivation
exactly. The recomputed verdict governs the goal assessment, so that case
yields `INSUFFICIENT_EVIDENCE`.
