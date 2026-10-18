# ADR 0035: Preserve named workload evidence in observation windows

## Status

Accepted (experimental).

## Context

Earlier reports retained request metrics by observation window only at aggregate level. A high weight fast workflow could keep aggregate p95 below its SLO while a low weight workflow degraded. Offline demand assessment could not independently verify each step inside every window.

## Decision

Report schema v13 records a metric ledger for every declared step in every configured observation window. Explicit weighted profiles also record completed journey metrics by completion window. Windows are allocated for the full configured observation horizon, including empty intervals, and the final window ends at the observation horizon. Window request counters, status diagnostics and response-body observations must reconcile with the aggregate window ledger, and schema-v13 reports must contain exactly one entry for every configured step and profile.

The engine evaluates the request p95 and error-rate criteria for each step and each explicit workflow profile in every window. Insufficient per-step or per-profile window samples make the run inconclusive; any step or profile breach fails the run even when aggregate metrics pass. Offline demand assessment requires this schema-v13 evidence and checks every named step and configured profile in every window.

## Consequences

The added ledgers expose temporal and workload-mix degradation hidden by aggregate request metrics. Profile p95 applies the configured latency objective to completed journey duration and is an additional check. The ledgers do not infer statistical independence or establish stationarity. Empty windows remain explicit and insufficient. Old schemas stay readable, but reports without per-step and configured-profile window evidence cannot substantiate a positive offline demand conclusion. The schema change does not enable `VALID_WITHIN_ENVELOPE`.
