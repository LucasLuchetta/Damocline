# ADR 0171: Validate the executed journey mix against the declared mix

## Status

Accepted.

## Context

Weighted profiles (ADR 0007) let a scenario declare a mix such as 60% view
dashboards, 25% search records and 15% update a record. Provenance labels
(ADR 0024) say where the weights came from, but nothing checked that the
executed workload matched them. Audit therefore always reported workload
representativeness as `NOT_ASSESSED` or `DECLARED_UNVERIFIED`.

Selected starts follow smooth weighted round-robin and track the weights
closely by construction. Completed journeys can still deviate when one
workflow fails, times out or is cancelled more than another, so the completed
mix is the more informative check.

## Decision

Run report schema v40:

- Records per workflow `writes` (any non-GET/HEAD step) and
  `data_variability` (`synthetic_per_journey` when a step references
  `{{synthetic_id}}`, otherwise `fixed`), derived from the validated scenario.
  Think time per workflow was already recorded.
- Records optional `config.profile_mix_reference_counts`: user-provided counts
  per workflow, for example from a sanitized access-log summary. They require
  a non-assumption `profile_mix_basis` and a `profile_mix_reference` label,
  must name each declared workflow exactly once, and are bounded. They are
  stored as a `user_provided_assumption`, never as measured facts.
- Records top-level `profile_mix`: per-workflow declared share, selected
  starts, completed journeys and reference count, plus three fits:
  selected starts vs weights, completed journeys vs weights, and the
  reference counts vs weights.

Each fit uses Pearson's chi-square goodness-of-fit test (`pearson_chi_square_v1`)
with fixed thresholds:

- `INSUFFICIENT_SAMPLES` when any expected count is below 5.
- `MIX_DEVIATED` only when p < 0.01 **and** the largest absolute share
  difference exceeds 2 percentage points (5 points for the reference fit).
- Otherwise `MIX_MATCHED` (`REFERENCE_MATCHED` / `REFERENCE_DEVIATED` for the
  reference).

The phase state is the worst of the starts and completed fits. The share
tolerance keeps very large samples from flagging negligible differences.
Thresholds are constants, not producer-supplied policy. Readers recompute
`profile_mix` from the saved profile counters and reject any mismatch, so
offline evidence matches what the engine produced. Pre-v40 reports must not
carry these fields.

Capacity assessments move to schema v7 and add
`confidence_dimensions.workload_mix`. Audit assesses the mix only when every
non-warmup phase carries v40 evidence for declared weighted workflows:
`workload_representativeness` becomes `MIX_MATCHED`, `MIX_DEVIATED` or
`INSUFFICIENT_SAMPLES`. Otherwise it stays `NOT_ASSESSED` (or the historical
`DECLARED_UNVERIFIED` / `INCONSISTENT_DECLARATION`). Blockers with remediation
name missing schema evidence, an undeclared mix, low counts, deviations,
missing reference counts and reference deviations. A matched mix never
upgrades decision confidence.

In a declared multi-workflow mix, a write workflow must also configure
`synthetic_data_prefix_env`, a per-actor identity
(`per_vu_cookie_env_prefix` or `profile_cookie_env_prefixes`) and reference
`{{synthetic_id}}` in a write step, in addition to `--allow-writes`.

## Consequences

- Users get per-phase evidence that the executed workload matches the
  declared mix, and a separate, explicitly assumed comparison with real usage.
- A matched mix shows that the declared mix was executed. It does not show
  that the declaration or the reference represents real demand.
- The chi-square approximation assumes independent journey outcomes. Actor
  and time dependence can make p-values optimistic; the share tolerance
  limits the effect.
- Open-arrival campaign audits do not use this assessment yet.
- Existing scenarios with a write workflow in a mix and no synthetic prefix or
  per-actor identity are now refused before traffic.
