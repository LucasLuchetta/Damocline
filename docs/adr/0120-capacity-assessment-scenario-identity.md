# ADR 0120: Preserve scenario identity in capacity assessments

## Status

Accepted

## Context

Run reports already persist declared workload revision, application revision, and environment. Campaign-level collection validation required these values, but the individual capacity audit exposed only the scenario name and target. This made reports harder to defend and allowed a level collection's signature to omit an explicit identity-consistency state.

## Decision

Offline campaign audits summarize the workload revision, application revision, and environment from every non-warm-up phase. Missing identity is `UNKNOWN`; values that change within a campaign are `CHANGED`. Neither status can support a favorable capacity conclusion. Reports render the values and status.

Multi-level collections require all three declarations to be present and stable across every required phase and identical across all tested levels. Per-level audits remain available when this check fails, but the collection is `NOT_COMPARABLE` and has no aggregated pass/fail boundary.

These fields are declarations copied from the scenario, not independent attestations. They do not authenticate application deployment, host, target, data state, or workload representativeness. Exact campaign/profile byte hashes and the workload fingerprint remain separate identities.

## Consequences

- Engineers can inspect which declared application/workload revision and environment a campaign used.
- Changed or absent declarations remain visible without being silently combined.
- Historical artifacts remain readable and are labeled `UNKNOWN` where those fields were not recorded.
- A matching declaration does not upgrade measurement quality or establish physical-host equivalence.
