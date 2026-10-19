# ADR 0063: Preserve campaign stage evidence and screen observation drift

## Status

Accepted (experimental). A stable-window screen is not a stationarity proof.

## Context

The default campaign wall-time cap equaled the sum of scheduled phase durations, while each native run can spend up to 30 additional seconds draining admitted work. A final phase could therefore be cancelled by the campaign cap during its own bounded drain. Offline demand assessment also accepted a schema-v1 PASS report with duplicate or reordered phase names because it checked phase presence rather than the complete schedule.

## Decision

Campaign schema v2 records the effective wall-time cap, a named evidence classification for every attempted phase, an abort reason when interrupted, and a bounded observation-window drift screen. The default cap includes up to 30 seconds of drain allowance per nonzero phase and repeat, while remaining within 15 minutes. An explicit shorter cap remains a deliberate safety abort bound. A default schedule whose nominal phases plus drain allowances exceed 15 minutes is rejected before any traffic.

For non-warm-up phases without an explicit scenario `window_duration`, the campaign assigns at least four windows, each at most 30 seconds. The observation screen requires at least three contiguous windows covering the observation interval, at least 20 completed requests and a valid p95 in each, and compares the first and last window. It labels a latency increase above both 20% and 5 ms, or a throughput decrease above 20%, `NON_STATIONARY`. Complete windows without either large change are `NO_LARGE_DRIFT_DETECTED`; missing or invalid windows are `INSUFFICIENT_WINDOWS`. A passing observation with either negative state makes the campaign inconclusive.

The stage classification distinguishes `PASS_OBSERVED`, `FAIL_OBSERVED`, `INVALID_DELIVERY`, and `INSUFFICIENT_EVIDENCE`. Dropped or cancelled planned arrivals, invalid report quality, and current-schema passing phases without adequate generator health/headroom cannot become observed application results. The generator screen requires valid runtime and process samples, nonzero sampled RSS, process/runtime CPU below the conservative 90% guardrail, and observed request throughput below 90% of configured `max_rps`. It uses mean process CPU; an isolated adjacent-sample peak remains diagnostic. This is a conservative short-trial delivery check, not a universal capacity threshold. Historical report schemas retain their prior classification behavior. The offline `plan` reader still reads historical schema v1, but these reports cannot make a positive demand claim because they lack the new provenance. For schema v2, it checks exact phase order, users, duration, target, budget, classifications, and recomputed observation-window state before a positive conclusion.

## Consequences

The screens detect coarse endpoint drift and generator/client delivery limits in a declared synthetic workload. They do not establish stationarity, independence, a confidence interval, or a sustainable capacity level. A short transient between the first and last windows can escape the drift screen; per-window SLO gates still apply independently. Ordinary external target reports remain `LIMITED`, so a campaign cannot gain a positive demand certification from this change alone. The local integration fixture sends only bounded requests to an ephemeral loopback server.
