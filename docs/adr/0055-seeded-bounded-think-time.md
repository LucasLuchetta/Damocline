# ADR 0055: Seed bounded between-step think times

## Status

Accepted (experimental).

## Context

Fixed pauses make repeated synthetic journeys predictable but cannot exercise bounded timing variation. Unseeded random pauses make a change in observed throughput harder to compare with another run.

## Decision

A scenario or individual profile may declare `think_time_range` with `min` and `max` Go durations. The range is uniform in nanoseconds, inclusive, and bounded to 0–30 seconds with a positive maximum. It applies only between steps; a profile-level fixed `think_time` or range overrides the scenario-level policy. A fixed duration and range cannot be declared at the same level.

A scenario or profile may instead declare `think_time_samples` with 2–32 discrete Go durations, each within 0–30 seconds. The seeded selector picks one entry per pause; repeated durations express higher relative frequency. Fixed, uniform and sampled policies are mutually exclusive at one level. A profile policy overrides the scenario policy.

A profile using a variable policy must contain at least two steps, so the policy has an observable pause boundary.

`workload_seed` selects reproducible timing decisions and defaults to 1 when a variable policy is used. Closed-loop decisions are keyed by actor, journey ordinal, and step index. Arrival-mode decisions are keyed by scheduler callback ordinal and step index, so worker handoff timing does not change the selected pause. The timing policy and seed enter the workload fingerprint and schema-v21 run configuration. Older fixed-pause fingerprints remain unchanged.

## Consequences

The configured sequence of timing choices can be replayed for the same actor/arrival schedule. OS scheduling and actual sleep duration remain measured effects, not guaranteed deterministic timings. Uniform and sampled pauses are synthetic assumptions, not evidence that human behavior follows either distribution. Sample lists contain durations and multiplicities, not raw user event traces. This change does not model idle sessions or independently arriving sessions.
