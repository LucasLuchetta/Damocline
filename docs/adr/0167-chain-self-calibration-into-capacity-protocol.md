# ADR 0167: Chain self-calibration into the capacity protocol

The capacity protocol (ADR 0165) previously forwarded an optional,
manually produced calibration artifact. Workload self-calibration (ADR 0154)
now makes generator evidence available for any scenario, so the protocol
runs it automatically.

## Decision

After the bounded network baseline, `damocline capacity` runs
`damocline calibrate` for every planned exploration level, then explores
with `--calibration` and `--require-calibration`. After exploration it
writes the derived campaign scenario, calibrates that exact level, and runs
the campaign with the same gate. Calibrating the derived scenario avoids
reusing evidence recorded for a different window or workload fingerprint.
A passing artifact that still matches the generator, workload and levels is
reused on resume; any other existing artifact is renamed, never deleted.

The mirror is sized from a recorded run of the target (`--reference-report`)
or from declared sizes. A calibration failure stops the protocol before
target workload traffic for those levels.

Dry-run prints every stage with its cap, the sample-sizing formula and the
total planned wall time (baseline, calibration, exploration and campaign
caps). Calibration plans exceeding the level or wall-time ceilings are
refused with a proposed adjustment. With a reference report, the protocol
warns before traffic when `max_rps` would cap the requested level, and warns
again after exploration when the campaign level delivered at least 90% of
`max_rps`.

Standard ceilings apply. `--extended-envelope` requires explicit
authorization and a lab or staging target class and is forwarded to
calibration only; exploration and campaign remain within the standard
50 VU ceiling until those stages support the extended envelope. ADR 0170
supersedes this paragraph: the opt-in now reaches explore and campaign.

## Consequences

Total wall time grows by the calibration allowance. The rate-cap estimate is
a linear per-VU extrapolation from one recorded run and is a hypothesis, not
a measurement. Calibration is generator-side evidence only and does not
establish target identity or application capacity.
