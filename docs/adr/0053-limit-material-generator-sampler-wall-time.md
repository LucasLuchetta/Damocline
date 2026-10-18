# ADR 0053: Limit material generator sampler wall time

## Status

Accepted.

## Context

Generator reports already preserve total and maximum snapshot collection wall time. Quality recomputation did not use the total, so an unusually expensive sampler could occupy a material fraction of a short run without limiting the evidence.

## Decision

For schema-v10 and later reports, compute the fraction of the observation duration occupied by recorded snapshot collection wall time. At or above 5%, classify the measurement as `LIMITED`. Preserve the existing raw durations. This is a conservative operational screen, not a calibrated estimate of CPU cost.

## Consequences

This catches one observable form of monitor intrusion without claiming that collection wall time captures CPU consumption, scheduling interference, or indirect Go runtime effects. The threshold needs cross-platform and loaded-host validation before it can be treated as a validated tolerance. Missing or older fields remain unknown under the existing schema behavior.
