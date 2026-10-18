# ADR 0061: Bound Generator CPU Capacity by Runtime and Cgroup Ancestors

## Status

Accepted for the measured Linux cgroup v1/v2 quota paths. Cpuset discovery and cross-host CPU equivalence remain open.

## Context

The prior Linux capacity reader examined only the process cgroup quota. Container and service managers can impose a tighter CPU quota on an ancestor, so the leaf quota alone can overstate the generator capacity used to normalize CPU observations. Reports also stored logical CPU count in preflight metadata but not in the run's generator summary.

## Decision

For supported Linux cgroup v1 and v2 layouts, walk from the process cgroup to the mounted hierarchy root and use the smallest finite quota found, bounded to 256 ancestors. Missing, malformed, or unreachable ancestor evidence makes effective capacity unknown; it is never treated as unlimited. Bound the result by both `runtime.NumCPU()` and `GOMAXPROCS`. Persist logical CPU count separately in report schema v19 and treat absent values in older schemas as unknown.

The reader does not directly inspect cpuset restrictions or dynamically monitor CPU-affinity changes. `runtime.NumCPU()` is the Go runtime's view at sampling time, not host attestation. CPU entitlement and host identity remain distinct evidence.

## Consequences

Deterministic fixtures cover tight parent quotas and missing parent files for cgroup v1/v2; the report quality evaluator rejects impossible or absent schema-v19 logical CPU evidence. Two Docker Desktop/WSL2 repeated acceptance runs passed all 13 gates each, with 980/980 independent loopback requests and 10/10 transport cases; both manifests record hierarchical quota evidence. This does not establish bare-metal Linux, cross-host equivalence, or a universal CPU capacity result.
