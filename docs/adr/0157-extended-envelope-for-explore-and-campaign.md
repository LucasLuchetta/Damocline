# ADR 0157: Extended envelope for closed-loop explore and campaign

## Status

Accepted.

## Context

ADR 0155 added an opt-in, calibration-gated extended envelope for a single
`damocline run`. `explore` and `campaign` stayed capped at 50 VUs. A healthy
target can pass at 50 VUs, so a search capped there never reaches its limit.
Multi-level commands need the same contract applied to every level they may
send, including levels that depend on the data.

## Decision

1. `damocline explore` and `damocline campaign` (closed-loop) accept the same
   opt-in as `run`: `--extended-envelope --extended-load-authorized
   --target-class lab|staging --calibration FILE --require-calibration`.
   Production is refused. The hard maxima are `config.ExtendedEnvelopeHardLimits`
   (500 VUs). Without the opt-in, the 50 VU ceilings, flags, outputs and
   artifact schemas do not change.
2. Before any traffic, every planned level must be `READY` under
   `calibration.ExtendedCoverageAssessors`, assessed exactly as `run` does with
   the scenario copied at that VU level:
   - explore: the geometric ladder from `explorer.PlannedLevels`, including
     an in-range exact demand-goal stage (`--peak-users`/growth/reserve);
   - campaign: the base level and, when configured, the spike level.
   Any uncovered level, or a level above the hard maximum, refuses the command
   with `EXTENDED ENVELOPE BLOCKED` and sends no traffic.
3. Bracket refinement levels in explore depend on the data, so they cannot all
   be checked in advance. Decision: refinement **only probes calibrated
   levels**. For each step, the explorer chooses the `READY` level strictly
   inside the failing bracket that is closest to the midpoint, taking the
   lower level on a tie. It never probes an uncovered level. If no calibrated
   level is left inside a bracket wider than one VU, the exploration stops as
   `INCONCLUSIVE`. The reason starts with "Bracket refinement stopped" and
   names the bracket. `highest_passing_tested_users` and
   `first_failing_tested_users` are not set, so the result is never shown as a
   one-VU `BRACKETED` result. We rejected two alternatives. Refusing up front
   would block every useful search. Reporting the coarse bracket as
   `BRACKETED` would change what that outcome means.
4. Generator guardrails are always on in extended mode. They use the ADR 0155
   defaults, and an operator can only tighten them. `explore` takes the same
   flags as `run`. `campaign` reuses its existing
   `--max-generator-cpu-percent`, `--max-generator-rss-mb` and
   `--resource-guardrail-samples` flags. When a trial stops on a guardrail,
   the result is never passing and the reason is on the generator side:
   - explore: the probe becomes `INCONCLUSIVE` with the reason
     `GENERATOR_LIMITED: ...`, and the exploration stops `INCONCLUSIVE`;
   - campaign: the trial is `ABORTED`, and its `abort_reason` starts with
     `GENERATOR_LIMITED:`.
   Neither outcome is reported as an application limit.
5. Artifacts record the envelope. Standard artifacts keep their current
   schemas, so their evidence is unchanged:
   - exploration schema v3, written only by extended explorations, adds
     `load_envelope`;
   - closed-loop campaign schema v18, written only by extended campaigns, adds
     `load_envelope`.
   The aggregate envelope uses the run-report `load_envelope` shape (ADR 0155).
   `exceeds_standard` means the highest planned level is above 50 VUs. An
   aggregate envelope never carries `stop_reason`; stops are recorded on the
   affected run.
6. Readers check this offline and recompute it. They do not trust the
   producer:
   - `explorer.ValidateEnvelope` (used by `goal` and `report export`) rejects
     `load_envelope` in schema v1/v2, schema v1/v2 explorations above 50 VUs,
     a v3 envelope that does not equal the envelope recomputed from
     `max_users`, and any embedded run whose own envelope mode or target class
     differs or is invalid;
   - `capacityplan.ValidateCampaignEnvelope` (part of the shared offline
     campaign validation used by `audit` and collection assessment) rejects `load_envelope` before
     schema v18. For v18 it requires an extended envelope equal to the one
     recomputed from the base and spike levels, enabled generator guardrails,
     and the same envelope on every phase run. Saved schedules are validated
     against the extended VU ceiling only when this envelope is present.
7. Wording: explore notes, `goal` findings and `audit` findings state that
   the extended envelope was used and, when it applies, that levels above the
   standard 50 VU ceiling were tested under it. `audit` also names each
   `GENERATOR_LIMITED` trial as a generator-side result.

## Consequences

- A `READY` gate qualifies only the generator at those exact levels. It is
  not a claim about target capacity or headroom.
- Extended explorations whose calibration covers only coarse levels end
  `INCONCLUSIVE` and do not narrow a failing bracket. To narrow it, calibrate
  more levels.
- `explore-arrival` and `arrival-campaign` stay on the standard envelope.
  Extending them requires per-rate coverage of every planned arrival rate and
  of rate-dependent refinement, and is left as follow-up work.
- `campaign --rotate-vu-cookies` and `damocline capacity` protocol plans keep
  the 50 VU bound. The flags added here use the same names as `run`, so a
  later protocol command can pass them through unchanged.
