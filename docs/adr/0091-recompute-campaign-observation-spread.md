# ADR 0091: Recompute and expose campaign observation spread

## Status

Accepted.

## Context

Closed-loop campaigns persist the minimum and maximum request p95, request error rate, HTTP throughput, and successful completed-journey throughput across independent observation phases. The offline audit previously left this information buried in the campaign artifact and did not check whether the persisted summary matched the saved phase reports. That made it harder to inspect repeat variation and allowed a changed summary to pass unnoticed.

## Decision

During offline campaign audit, derive one structured observation record for every saved base-load `observation` phase. Include request and completed-journey counts, p95 values, error rates, and successful completed journeys per second. Recompute the existing campaign spread from these phase reports and compare it exactly with the persisted spread when present. A mismatch makes the campaign evidence invalid; a missing legacy spread remains visible as `NOT_RECORDED` while recomputed phase data is retained.

Render trial observations and the recomputed min/max spread in both single-campaign and multi-level audit output. Label the spread as descriptive rather than a confidence interval. Do not change the decision-confidence state: repeated phases do not validate independence, stationarity, representativeness, target identity, or host equivalence.

## Consequences

Users can inspect request and useful-journey behavior for each independent observation trial and see whether the saved aggregate spread matches its sources. Old campaign artifacts remain readable. A matching spread establishes internal arithmetic consistency only; it does not increase measurement or decision confidence.
