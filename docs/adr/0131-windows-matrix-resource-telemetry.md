# ADR 0131: Capture bounded Windows resource telemetry for local experiment matrices

- Status: Accepted
- Date: 2026-10-23

## Context

The bounded local experiment matrix can run independent target/generator workers concurrently. Shared-host contention changes the measurement environment, so campaign outcomes alone are insufficient to interpret latency differences across a parameter sweep. Windows does not use the Linux host sensor in this fixture, and the runner needs local resource context without adding Docker or an external service.

## Decision

Sample host CPU and physical memory plus each active worker process tree's CPU time and working set during matrix execution. Use Windows process cumulative processor-time ticks and WMI for process inventory and host counters. Persist raw samples and per-phase summaries in a private resource sidecar linked from each worker result. Record observed sampling intervals, collection errors, and gaps; map samples to campaign phases by UTC timestamps and label that alignment approximate. Include machine-wide contention and telemetry limits in the matrix manifest.

## Consequences

- Concurrent sweeps expose host pressure and per-target/generator resource trends while keeping the runner self-contained.
- Host-wide readings repeat across workers and cannot be attributed to one campaign; they are diagnostic covariates, not causal evidence.
- WMI collection time makes actual cadence slower than the requested interval, and polling overhead is unmeasured.
- Shared-host parallel runs improve exploration throughput but do not provide independent-host capacity evidence. Decision-grade confirmation still needs controlled resource conditions and a representative workload.