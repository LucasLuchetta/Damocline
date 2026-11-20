# ADR 0156: Start post-ramp campaign phases at target load and use a robust drift reference

## Status

Accepted.

## Context

Every campaign phase is a separate bounded engine run. Before this change,
each run re-applied the scenario `ramp_up`, so a closed-loop observation
phase that followed a warm-up started by ramping virtual users from zero
again. On a real target (closed loop, 50 VUs, 10 s ramp, 2 s think time,
30 s warm-up, 2 min observation, 30 s windows), the first observation window
consistently carried about 16% fewer requests and a lower p95 than the later
windows. The drift screen compared every window with the first one. A
ramp-biased light window therefore became the reference, and later windows at
ordinary steady load looked like a latency rise. Healthy targets were
classified `NON_STATIONARY`, and observation aggregates were diluted by ramp
traffic.

Open-arrival campaigns already force `ramp_up` to zero for every phase, so
they do not re-ramp. They share the drift screen, though, and a cold first
window could bias their reference the same way.

## Decision

1. **Steady post-ramp phases.** In closed-loop campaigns, only the first phase
   of each trial and a configured spike apply the scenario ramp. Every other
   phase (stabilization, observation, recovery, soak) starts with all of its
   VUs at t=0, because an earlier phase of the same trial already ramped the
   workload. A spike is a fresh, higher-VU run, so it keeps the scenario
   ramp. A trial with no warm-up or stabilization still ramps its
   observation, because nothing ramped before it. We chose this over cutting
   a ramp interval out of the windows. Cutting would shorten the declared
   observation horizon, make window boundaries depend on ramp length, and
   still leave ramp traffic in the native run's aggregates. Running at
   target load keeps every declared window and aggregate at steady load.

2. **Recorded per-phase ramp.** Closed-loop campaign schema v17 records
   `ramp_up` on each phase. The native run's `config.ramp_up` records the same
   value. The offline reader requires both to agree and to follow the rule
   above, with the first phase's ramp as the scenario ramp. Earlier schemas
   must not carry this field.

3. **Robust drift reference.** Schema v17 closed-loop campaigns and schema v9
   open-arrival campaigns use the `first_window_unless_light_leading_window`
   reference rule. The rule tests the first window's request rate against the
   median request rate of the later windows. If the first window is below
   that median by more than the policy's `max_throughput_drop_percent`, it is
   *light*: it still has to pass every completeness and contiguity check, but
   the second window becomes the reference. Only a single leading window can
   be excluded, and at least `minimum_windows` windows must remain from the
   reference onward, or the result is `INSUFFICIENT_WINDOWS`. A degrading
   target has a heavy first window, not a light one, so it is still compared
   against its first window and flagged. Degradation after an excluded light
   window is still measured against the second window. Each phase records the
   zero-based reference as `stability_baseline_window` (omitted when it is
   0). Closed-loop campaigns also record `drift_baseline`.

4. **Replay parity.** The reference rule is selected by campaign schema, not
   by the stability policy object. Profiles, profile verification and
   collections therefore keep their existing policy comparisons. Offline
   validation, audit and `goal` replay the same rule from the saved windows,
   and they reject an altered label, reference window, ramp or rule name.
   Earlier schemas keep replaying the historical first-window reference.

## Consequences

- Observation windows after a warm-up measure steady load, and a ramp-biased
  window can no longer be the drift reference.
- With no ramp, all VUs start at the same instant at the beginning of each
  post-ramp phase, so their think-time phases are aligned again across the
  engine-run boundary. A short synchronized burst can occur at the start of
  the phase. This is a known limit of running each phase as a separate engine
  run, not a continuous one.
- Excluding a light leading window also hides a real throughput rise at the
  start of observation, for example from cache warming that the warm-up did
  not finish. The excluded window still counts in the phase's native
  aggregates. The screen stays coarse and is not a stationarity test.
- Readers older than this change reject schema v17 closed-loop and schema v9
  open-arrival campaigns, as they do any newer schema.
