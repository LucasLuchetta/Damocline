# ADR 0072: Add repeated open-arrival campaigns

## Status

Accepted

## Context

Damocline can schedule independent open-loop journey starts and can explore
short repeated rates. Its existing phased campaign is measured in closed-loop
virtual users, so reusing it for arrival workloads would conflate scheduled
starts/s with worker count. Owners also need an offline exact-demand check for
the phases that were actually recorded.

## Decision

Add a separate bounded `arrival-campaign` executor and artifact. Keep the base
scheduled journey-start rate constant through warm-up, observation, recovery and
an optional final soak; allow a separately declared higher-rate spike. Retain
each phase's complete native run report and recompute its evidence and coarse
window drift screen in the offline demand assessor. Require an exact base-rate
match and the complete schedule for a positive result. Apply the existing
15-minute campaign ceiling, bounded phase drains, configured request ceiling,
authorization and write controls. Keep arrivals distinct from VUs, scheduler
admissions, completed journeys/s and HTTP requests/s.

Reject phase/window combinations that cannot produce three complete observation
windows before sending traffic. An offline SLO-miss finding requires a
reproducible configured-objective breach plus valid report quality, workload
delivery and generator headroom; a campaign `FAIL` label alone is insufficient.

## Consequences

This provides repeated sustained observations for the declared open-arrival
schedule and supports offline review without generating traffic. Current
external run measurement quality remains LIMITED; therefore passing campaign
reports do not certify sustainable capacity or establish a root cause. The
artifact uses its own schema version and does not change HTTP report schemas.
