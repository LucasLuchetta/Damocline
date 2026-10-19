# ADR 0101: Report conditional repeat-trial pass uncertainty

## Status

Accepted.

## Context

Closed-loop campaign audits already retained each independent trial's outcomes and recomputed descriptive metric spread, but the spread had no confidence interpretation. Adjacent requests within a trial are correlated, while a small number of repeated trials does not support a narrow, distribution-free confidence claim.

## Decision

Offline campaign audit schema v2 computes a two-sided exact Clopper-Pearson interval for the proportion of terminal PASS outcomes among terminal PASS/FAIL trials. ABORTED, IN_PROGRESS, and other non-terminal/inconclusive outcomes are counted as excluded, never silently mapped to pass or fail. The audit states that the interval assumes independent, stationary repeat trials and concerns same-condition trial pass probability only. It does not change the demand verdict or promote decision confidence.

## Validation

Unit tests cover exact known interval endpoints, input bounds, repeat outcome mapping, and exclusion of incomplete trials. CLI tests verify the human-readable labels. The full Go suite and CI validate offline replay without sending traffic.

## Consequences

A report quantifies how little two to five repeats establish about repeat pass probability, while retaining every observed trial. Sequential trials can remain dependent, stationarity is not tested, and the interval does not estimate application capacity or future deployment success. Dependence-aware experiment design remains open.
