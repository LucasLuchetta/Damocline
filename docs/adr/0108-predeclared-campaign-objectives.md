# ADR 0108: Persist and replay predeclared campaign objectives

## Status

Accepted

## Context

Campaign acceptance criteria were distributed across scenario fields, run-phase configuration, and the stability policy. Reports retained those phase settings but did not expose one contract showing which journey SLOs, sample/window coverage, completion ratio, and dropped-arrival policy had been fixed before the campaign.

## Decision

Closed-loop campaign schema v13 and open-arrival campaign schema v8 persist an objective contract before phase execution. The contract records aggregate and per-workflow journey p95/error limits, per-step and per-window journey/step/workflow sample minima, required observation windows, 100% completion of started journeys, zero dropped arrivals, and fixed or automatic per-phase window sizing. Campaign preflight validates the contract. Offline audits validate its shape, check it against each phase's saved controls, and replay completion, drop, and window-coverage rules while preserving each campaign family's native delivery classifier. Markdown capacity reports and redacted evidence exports render the criteria.

## Consequences

Reviewers can reproduce which declared criteria governed an observed campaign outcome and detect phase evidence that no longer matches them. The contract does not prove workload representativeness, target identity, independence, stationarity, or capacity beyond the recorded conditions. Historical schemas remain readable and are labeled as lacking this contract.
