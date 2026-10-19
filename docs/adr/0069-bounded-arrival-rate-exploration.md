# ADR 0069: Keep open-loop arrival exploration separate from VU exploration

- **Status:** Accepted
- **Date:** 2026-10-19

## Context

The native engine supports both closed-loop virtual users and open-loop scheduled journey starts. They describe different offered workloads. A VU level cannot be directly compared with journey starts per second, and neither is HTTP request throughput. Reusing the existing VU explorer's result fields risks implying equivalence and obscures worker underdelivery.

## Decision

Provide a separate `explore-arrival` CLI command and `internal/arrivalexplorer` package. Search scheduled journey starts/s with bounded doubling and bracket refinement, explicit worker and HTTP request ceilings, repeated passing probes, and optional exact-target insertion. Preserve each native run in a separate schema-v1 exploration artifact. Mark dropped or cancelled workload, inadequate sample coverage, invalid data quality, generator pressure, and a binding client request-rate ceiling inconclusive; never infer an application failure from invalid delivery.

## Consequences and limits

The result reports the tested workload bounds and does not certify sustainable capacity, establish a universal limit, or convert the rate into VUs or HTTP requests/s. It inherits the native engine's scenario, authorization, and traffic safety controls. A short repeated search does not replace phased campaigns, independent calibration, or statistical qualification.
