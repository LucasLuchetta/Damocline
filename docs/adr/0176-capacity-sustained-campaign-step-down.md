# ADR 0176: Bounded sustained-campaign step-down in capacity

## Status

Accepted.

## Context

`damocline capacity` selects the highest repeatedly passing explore level and
runs one sustained campaign there. Explore stages are short (for example 20s),
while a campaign holds the level through warm-up, a sample-sized observation
and recovery, twice. In a pilot, a level passed both short explore stages but
its campaign failed with observation p95 far above warm-up p95. The protocol
then ended with FAIL and an inconclusive audit, giving no sustainable level.
Short stages can overestimate sustained capacity.

## Decision

- When the campaign at the selected level completes with outcome `FAIL`
  (latency or error objectives), capacity runs a campaign at the next lower
  repeatedly passing explore level. Explore runs with
  `--require-calibration`, so every candidate is a calibrated level.
- At most two step-downs run. Each campaign keeps its own `--budget` cap and
  its own sample-sized window derived from that level's explore rates.
- `INCONCLUSIVE` or `ABORTED` campaigns (invalid delivery, drift,
  insufficient evidence, generator limits, interruption) never trigger a
  step-down: they are not target verdicts.
- The dry-run plan declares the step-down rule, the candidate levels (with
  `--explore-file`) and a total wall time that reserves the step-down
  campaigns and their campaign-level calibrations.
- A private `campaign-ladder.json` records every attempted campaign. The
  audit covers the highest passing campaign, otherwise the originally
  selected one. `capacity.md` and `capacity.html` gain a step-down section
  listing all campaigns, stating that higher levels passed short explore but
  failed sustained, or that no campaign passed and no sustainable level is
  reported. It includes a protocol note on the explore-to-campaign gap; the
  cause of the gap is a hypothesis, not a measured fact.

## Consequences

- Users get a sustainable level when a lower explored level sustains.
- Worst-case wall time grows by up to two campaign budgets; it is declared
  before traffic.
- The step-down level is bounded by explore evidence; levels between explored
  levels are not probed.
