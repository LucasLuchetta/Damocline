# ADR 0070: Persist bounded campaign stability policy

## Status

Accepted (experimental). Configurable coarse drift limits do not establish statistical stationarity.

## Context

Campaign schema v4 used a fixed observation-window screen: at least three complete windows with 20 samples per aggregate and named series, a p95 increase above both 20% and 5 ms, or a throughput decrease above 20%. Users could not select stricter screening thresholds, and a saved campaign did not state which screen an offline reader had to reproduce.

## Decision

Campaign schema v5 persists a `stability_policy` containing minimum windows, minimum completed samples per window, maximum p95 increase percent and milliseconds, and maximum throughput drop percent. Defaults preserve the schema-v4 thresholds. Values have hard bounds; the campaign rejects a minimum window count that the declared observation or soak duration and `window_duration` cannot supply before sending traffic.

Every observation and soak window is checked for continuous coverage, sufficient aggregate and named-series samples, valid completed metrics, and latency/throughput changes relative to the first window. A policy breach is `NON_STATIONARY`; missing coverage or sample adequacy is `INSUFFICIENT_WINDOWS`. The offline campaign reader validates and recomputes schema-v5 classifications from the persisted policy. Historical schemas retain their fixed behavior. Cross-level campaign collection includes the effective policy in its control signature, so campaigns screened under different limits are not combined.

## Consequences

The screen is adjustable to catch larger or smaller declared changes in a bounded run. It remains a coarse first-window comparison and cannot prove stationarity, detect every transient or leak, or establish sustainable capacity. An active wait-until-stable stabilization stage and broader variance/backlog criteria remain separate roadmap work.
