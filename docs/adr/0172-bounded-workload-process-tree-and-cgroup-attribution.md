# ADR 0172: Bounded workload process-tree and cgroup attribution

## Status

Accepted.

## Context

When a target runs in a container, `damocline-sensor --workload-pid` is often given the container's init process. That process does almost no work, so its process CPU and RSS stay near zero while a child process does the work. Only cgroup memory was meaningful. A report could therefore neither support nor reject the hypothesis that the application saturated a single CPU core when throughput plateaued while host CPU stayed mostly idle.

## Decision

Add opt-in, read-only workload attribution to the Linux host sensor:

- `--workload-descendants` (requires `--workload-pid`) observes the bounded process tree rooted at that PID. The sensor scans at most 16,384 `/proc` entries, keeps at most 512 tree processes and 4,096 threads, and reports the process count, summed RSS, tree CPU cores, up to eight top processes by CPU, and the busiest comparable thread. Exceeding a bound records `TRUNCATED`.
- `--workload-cgroup <path>` selects a cgroup-v2 directory relative to the visible mount. Without it, the PID's own cgroup is used when a PID is given. The sensor reports `cpu.stat` `usage_usec` deltas as cores used, `cpu.max` quota cores, `nr_periods`/`nr_throttled`/`throttled_usec` deltas, `memory.current`, and `memory.events` deltas. Paths with traversal or unexpected characters are rejected. A missing unified hierarchy or unreadable file is `UNAVAILABLE`.
- Deltas are computed only between comparable snapshots. Root restarts, PID reuse, tree membership changes, counter resets and first samples are `INCOMPLETE` and carry no total. Threads that start or exit inside the interval are not compared, so the busiest-thread value is then a lower bound with `INCOMPLETE` thread coverage.
- The sensor reading schema advances to v2. Readers accept v1 and v2, reject attribution inside v1 readings, and validate attribution invariants (coverage/scope agreement, totals only when complete, throttled periods ≤ periods, bounded top list).
- The capacity protocol report (schema v2) adds `workload_saturation`, always labeled `HYPOTHESIS`. It looks for consecutive tested levels where VUs rise ≥10%, median HTTP requests/s rise <5% and median observation p95 rises ≥20%. At the plateau level it reports measured facts and states the hypothesis as supported when the busiest thread reached ≥0.9 cores, cgroup CPU reached ≥90% of quota, or ≥5% of CFS periods were throttled. Absent attribution is `UNKNOWN`. Complete attribution with no such signal is `HYPOTHESIS_NOT_SUPPORTED_BY_MEASURED_FACTS`, and the plateau cause stays unknown. Only phases with complete host-sensor coverage are used.

## Consequences

Container workloads can be attributed without privileged operations, ptrace, or writes. Remaining limits: CPU from children that exited during an interval is not counted. Sampling is 1 s with kernel tick resolution. Throughput plateaus are assessed only between tested levels. A near-one-core thread or quota-bound cgroup is consistent with CPU saturation but does not prove it. Locks, I/O waits, dependencies or client limits can produce the same pattern. Process `comm` names are stored in local artifacts and should be treated as private evidence. Cgroup-v1 hosts receive explicit `UNAVAILABLE` cgroup evidence.
