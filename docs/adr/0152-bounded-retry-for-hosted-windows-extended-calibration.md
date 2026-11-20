# ADR 0152: Allow one independent retry of the extended calibration on hosted Windows CI

## Status

Accepted.

## Context

On 2026-11-20, three CI runs on GitHub-hosted Windows runners failed the
extended local reference calibration. Each time, 2 of 250 or 500 planned
arrivals at the 50 or 100 arrivals/s stage were reported as dropped
because they were late. Each failure was on a hosted Windows runner, and
the identical revisions passed on hosted Linux and on independent
bare-metal Linux.

Damocline detected those late arrivals and reported them as dropped,
which is correct behavior. Hosted runners are shared virtual machines
and are not a qualified generator class (ADR 0149).

## Decision

On hosted Windows CI only, the extended calibration step may run one
additional, independent calibration when the first attempt fails:

- Both attempts write separate artifacts.
- A failed first attempt emits a workflow warning.
- The step fails if both attempts fail.

Linux CI and the bounded acceptance steps are unchanged.

## Consequences

- A single late-arrival failure on a hosted Windows runner no longer
  blocks merges.
- Repeated failures still block merges.
- The warning keeps the first failure visible in the CI log. A
  first-attempt failure is not a pass.
- Generator qualification still comes from repeated acceptance on the
  actual generator class, not from hosted CI.
