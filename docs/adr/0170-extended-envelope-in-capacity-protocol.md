# ADR 0170: Extended envelope through the capacity protocol

## Status

Accepted. Amends ADR 0167.

## Context

ADR 0167 forwarded `--extended-envelope` to self-calibration only, so
`damocline capacity` stayed below the standard 50 VU ceiling. ADR 0157 now
lets `explore` and `campaign` run under the extended envelope, but extended
bracket refinement probes only calibrated levels. Calibrating only the
geometric ladder would end most extended explorations `INCONCLUSIVE`.

A real run also showed that a flat 5s per-stage allowance made the explore
budget too small. A stage can use its network baseline (up to its 30s wall
cap), a bounded drain and setup time, in addition to the stage duration.

## Decision

1. With `--extended-envelope --extended-load-authorized --target-class
   lab|staging`, `capacity` accepts `--max-users` up to the extended hard
   maximum (500 VUs). It forwards the same three flags to `explore` and
   `campaign`, together with `--calibration` and `--require-calibration`.
   Extended mode requires automatic self-calibration, so it refuses an
   external `--calibration` artifact. The standard envelope is unchanged.
2. The explore self-calibration covers the ladder and bisection fill-in
   levels in one bounded `damocline calibrate --extended-envelope` call.
   Fill-ins are added breadth first, starting with the top bracket, until
   the 12-level or 15-minute self-calibration ceiling is reached. The
   campaign level is calibrated separately, as in ADR 0167.
3. The worst-case number of refinement probes is reserved in the explore
   budget. This is the bisection depth over the calibrated levels inside
   the widest ladder bracket.
4. An extended exploration can still stop `INCONCLUSIVE` because no
   calibrated level remains inside the bracket. In that case, the protocol
   runs the campaign at the highest repeatedly passing tested level. The
   output states that the bracket was not refined. Every other
   `INCONCLUSIVE` outcome still stops the protocol.
5. The per-stage explore overhead uses the bounds the engine enforces. These
   are `config.NetworkBaselineWallBudget` (unless the scenario skips the
   baseline), `engine.DrainAllowance` and a 2s setup allowance. Ramp-up is
   part of the stage duration. Dry-run prints the stage count and the
   overhead. If the explore need exceeds the 15m cap, the protocol refuses
   before traffic and proposes a shorter ladder (`--max-users`) or shorter
   stages.

## Consequences

- Planned explore budgets are larger and conservative. Some default ladders
  with long sample-sized stages are now refused before traffic instead of
  aborting midway.
- Calibration wall time grows with the fill-in levels and stays within the
  self-calibration ceilings.
- The campaign confirms a coarse level that has not been refined. It does
  not claim a one-VU bracket.
- A `READY` calibration is evidence about the generator only. It does not
  measure target capacity.
