# ADR 0070: Keep arrival demand goals in scheduled starts per second

## Status

Accepted

## Context

The native engine already supports an open-loop journey arrival mode and a
bounded repeated-rate explorer. Business planning and offline demand assessment
were available for closed-loop VU explorations, but applying that model to
arrival artifacts could conflate scheduled starts/s with active workers,
admitted or completed journeys, or HTTP request throughput. A saved explorer
label alone also cannot establish that a calculated business target was tested
or that the native run evidence still supports its classification.

## Decision

Add a separate journey-start goal expressed in scheduled starts/s. Calculate the
target as the ceiling of peak demand multiplied successively by growth and
reserve factors. `explore-arrival` may insert this exact rate into its existing
bounded search, while `goal --file` assesses a saved schema-v1 arrival artifact
offline. The assessor validates the exact rate and native report controls,
recomputes run delivery, SLO and generator evidence, and never interpolates
nearby rates. A target beyond the configured ceiling or absent from recorded
probes cannot receive a positive finding. Limited measurement quality remains
insufficient, even when short trials report PASS.

The existing VU demand path remains separate. No conversion is defined between
VUs, scheduled starts/s, scheduler admissions, completed journeys/s, and HTTP
requests/s.

## Consequences

Owners can plan and verify that an exact arrival demand was attempted using a
single, unit-consistent workflow. The result remains a short-trial evidence
assessment. Current ordinary target runs do not qualify as
`VALID_WITHIN_ENVELOPE`; sustained campaigns, representative workload evidence,
and time-aligned application/dependency observations remain necessary before a
sustainable capacity or infrastructure decision.
