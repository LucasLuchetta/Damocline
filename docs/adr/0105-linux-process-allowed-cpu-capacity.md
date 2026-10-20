# ADR 0105: Bound Linux CPU capacity by the sampled process allowed list

## Status

Accepted

## Context

Linux generator CPU entitlement already combines runtime logical CPUs, `GOMAXPROCS`, and finite cgroup quota ancestors. Cgroup v2 effective cpusets are also considered. This did not detect cgroup-v1 cpuset restrictions or CPU affinity changes made after the Go runtime's initial CPU-count snapshot.

## Decision

Read `/proc/self/status` `Cpus_allowed_list` during every Linux generator-health sample and use its CPU count as an additional upper bound. Preserve existing cgroup quota and v2 effective-cpuset calculations. If the status file or field is missing, malformed, duplicated, oversized, or unreadable, mark capacity unknown rather than treating the missing evidence as unrestricted. When this list is the tighter bound, identify it in the existing capacity-source metadata; no report schema change is needed.

The field represents the calling task's currently allowed CPUs. It provides evidence for effective cpuset and affinity constraints on that task, including changes after startup, but does not inspect every Go thread or attest homogeneous per-thread affinity.

## Consequences

Deterministic fixtures cover malformed and missing evidence, v1 cpuset/affinity restriction, and a change in the allowed list after startup. The read occurs within the existing snapshot collection timing, so its cost contributes to the existing wall-time quality screen. Source changes remain visible to exact calibration matching when they change the limiting evidence. Host CPU pressure, direct sampler CPU-cost calibration, and heterogeneous per-thread affinity remain open validation work.
