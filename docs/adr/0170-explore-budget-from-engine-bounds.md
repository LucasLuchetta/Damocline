# ADR 0170: Explore budget from engine bounds, and an extended end-to-end check

## Status

Accepted. Amends ADR 0167.

## Context

ADR 0167 forwards the extended envelope through `damocline capacity` and
uses one bounded calibration for the ladder, refinement and confirmation
levels. The explore budget allowed a flat 5s of overhead per stage and
counted only ladder stages. In a real run with sample-sized stages, explore
was `ABORTED` during its last level even with a 15m budget. Each stage can
also use:

- its network baseline, up to `config.NetworkBaselineWallBudget`;
- a bounded drain after the schedule;
- setup time.

Bracket refinement probes were not budgeted at all.

## Decision

1. The per-stage overhead comes from the bounds that the engine enforces.
   It is `config.NetworkBaselineWallBudget` (unless the scenario declares a
   network baseline skip reason), plus `engine.DrainAllowance` for the
   explore scenario, plus a 2s setup allowance. Ramp-up runs inside the
   stage duration and is not counted again.
2. The stage count covers the worst case. The ladder runs up to a failing
   level, then that bracket is refined. The plan takes the maximum over
   every possible failing level, and over the full ladder when every level
   passes. Each probe runs `repeats` stages. Standard refinement bisects any
   level in the bracket. Extended refinement bisects only the calibrated
   levels inside it (ADR 0157).
3. Dry-run prints the stage count, the stage duration, the overhead and the
   resulting need. If the need is above the 15m explore cap, the protocol
   refuses before any traffic. It proposes a shorter ladder (`--max-users`)
   or shorter stages. An explicit `--explore-budget` below the need is still
   refused, and a default budget still grows to fit.
4. An end-to-end loopback test runs the full extended chain against a
   server whose capacity is known and above 50 VUs: baseline,
   self-calibration, explore, campaign, audit and executive report. Stage
   floors (explore stage, campaign warm-up and recovery) are package
   variables, so the test can shorten them within the explore and campaign
   validators. The test checks four things:
   - the highest passing level equals the known capacity;
   - the campaign passes with `VALID_WITHIN_ENVELOPE` quality;
   - the audit returns a defined verdict;
   - the real explore wall time is no more than the planned explore budget.

## Consequences

- Planned explore budgets are larger and conservative. A plan that used to
  abort midway is now refused up front. With default 20s stages and two
  repeats, the default standard ladder (2 to 20 VUs) needs 14m. Extended
  ladders much above about 32 VUs need shorter stages or a lower
  `--max-users` to fit within the 15m explore cap.
- The estimate is a worst-case bound, not a forecast. When the baseline is
  fast, real explore time is usually well below it.
