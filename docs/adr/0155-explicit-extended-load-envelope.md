# ADR 0155: Explicit, calibration-gated extended load envelope

## Status

Accepted.

## Context

The standard per-run ceilings (G0-11) are 50 closed-loop VUs or 50
open-arrival workers, 100 configured HTTP request starts/s and 3 minutes per
run. A healthy target frequently passes at the ceiling, so its limit is never
observed. G0-11 allows a wider envelope only through a separate opt-in with
independently validated envelopes. Raising the default would silently widen
the blast radius of every scenario and would let generator saturation
masquerade as target behavior at levels no calibration has measured.

## Decision

1. The standard envelope is unchanged and remains the default.
   `TestStandardRunSafetyCeilings` is unchanged. `config.StandardEnvelopeLimits`
   names the existing values.
2. `damocline run` accepts `--extended-envelope`. It is refused unless all of
   the following are present: `--extended-load-authorized` (a separate
   acknowledgement that the target owner authorized load above the standard
   ceilings, in addition to `--authorized`), `--target-class lab|staging`
   (production is not an accepted class, and a scenario `environment` label
   containing `prod` is refused), `--calibration FILE` and
   `--require-calibration`.
3. The extended envelope raises ceilings to a fixed absolute maximum of 500
   VUs/arrival workers, 1000 configured request starts/s (and arrival starts/s)
   and 15 minutes per run. No flag or scenario field raises it further. The
   values are a 10x/10x/5x step over the standard envelope: large enough to
   find the limit of a typical single-origin service, while staying within what
   one generator process can schedule and what a 15-minute campaign wall budget
   already permits elsewhere. Runs beyond this need a distributed generator,
   which is out of scope.
4. Before any traffic, the exact planned level must be covered by a fresh,
   matching generator calibration. `calibration.AssessExtendedEnvelope`
   evaluates `calibration.ExtendedCoverageAssessors`. The built-in assessor
   uses the existing exact-match interfaces (`AssessArrivalEnvelope` for the
   exact arrival rate, `AssessClosedLoopEnvelope` for the exact VU level),
   which already enforce the 24-hour freshness window, runtime identity,
   workload shape and no interpolation. Any status other than `READY` refuses
   the run before traffic.
5. Extension point: a workload-specific self-calibration artifact type adds
   one `ExtendedCoverageAssessor` to `ExtendedCoverageAssessors`. The first
   `READY` verdict wins; an empty or all-blocked list refuses. No other code
   changes are needed for the gate to accept the new artifact. Until such an
   artifact exists, the existing reference calibration only measures closed-loop
   profiles at `max_rps` 100 and arrival rates up to 100/s, so most extended
   levels are refused. That is intentional: the gate fails closed.
6. Generator guardrails are enabled by default in extended mode (process CPU
   85% of detected capacity, RSS 2 GiB, 3 consecutive samples). Operators may
   tighten them with `--max-generator-cpu-percent`, `--max-generator-rss-mb` and
   `--guardrail-samples`, never loosen or disable them. A trigger aborts the
   run (outcome `ABORTED`, existing guardrail trigger evidence) and records
   `load_envelope.stop_reason = GENERATOR_LIMITED`: the result describes the
   generator, not the target.
7. Run report schema v38 adds top-level `load_envelope` with the mode, the
   applied limits, target class, acknowledgement, calibration coverage,
   `exceeds_standard` and `stop_reason`. The engine records it for every run.
   Readers reject pre-v38 reports carrying it, a standard record whose
   configuration exceeds the standard ceilings, an extended record without
   acknowledgement, accepted target class, `READY` coverage or enabled
   guardrails, and a stop reason that disagrees with the guardrail trigger.
   `insights` and the run summary state that results above the standard ceiling
   were produced under the extended envelope.

## Consequences

- Campaign, explore, explore-arrival and arrival-campaign commands stay on the
  standard envelope in this change. Their child run reports carry
  `load_envelope.mode = standard`, so campaign and explore artifacts record the
  envelope through every embedded run. Extending them needs per-level
  calibration coverage for every probed level and is a follow-up.
- A `READY` extended gate qualifies only the generator at that exact level; it
  is not a capacity or headroom claim about the target.
- The absolute maximum is a code constant. Changing it requires a new ADR.
