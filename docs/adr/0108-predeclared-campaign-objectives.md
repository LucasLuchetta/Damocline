# ADR 0108: Persist and replay predeclared campaign objectives

## Status

Accepted

## Context

Closed-loop campaign acceptance criteria were distributed across scenario fields, run-phase configuration, and the stability policy. Reports retained those phase settings but did not expose one contract showing which journey SLOs, sample/window coverage, completion ratio, and dropped-arrival policy had been fixed before the campaign.

## Decision

Campaign schema v13 persists an objective contract before phase execution. It records aggregate and per-workflow journey p95/error limits, per-step and per-window journey/step/workflow sample minima, required observation windows, 100% completion of started journeys, zero dropped arrivals, and fixed or automatic per-phase window sizing. Campaign preflight validates the contract. The offline audit validates its shape, checks it against each phase's saved controls, and replays completion, drop, and window-coverage rules. Markdown reports render the same contract.

## Consequences

Reviewers can reproduce which declared criteria governed an observed campaign outcome and detect phase evidence that no longer matches them. The contract does not prove workload representativeness, target identity, independence, stationarity, or capacity beyond the recorded conditions. Historical schemas remain readable and are labeled as lacking this contract. Open-arrival campaign audit equivalence remains future work.
