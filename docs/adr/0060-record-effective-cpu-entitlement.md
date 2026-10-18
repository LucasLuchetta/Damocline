# ADR 0060: Record effective CPU entitlement for generator health

## Status

Accepted

## Context

The generator previously normalized process and Go runtime CPU observations using `GOMAXPROCS`. In containers, `GOMAXPROCS` may reflect visible logical CPUs while a cgroup quota grants fewer CPU cores. This can understate generator saturation and make calibration evidence incompatible across different CPU entitlements.

## Decision

Record logical CPU count, `GOMAXPROCS`, and effective CPU capacity as separate runtime facts. On Linux, determine capacity from the process cgroup's v2 or v1 CPU quota when its location and controller files can be resolved safely; use the lower of that quota and `GOMAXPROCS`. The current reader does not walk parent cgroups or inspect cpuset constraints, so its source describes the process cgroup quota evidence available at that path, not a complete proof of every inherited CPU restriction. When cgroup evidence is unavailable or malformed, mark capacity unknown rather than treating it as unlimited. On other operating systems, use `GOMAXPROCS` as the documented capacity source.

Normalize process/runtime CPU ratios against known effective capacity. Persist the capacity, source, and known flag in report schema v18 and calibration preflight schema v7. Missing capacity limits quality; changed capacity is not equivalent calibration evidence. Preserve readers for earlier schemas and their original interpretation.

## Consequences

Containerized calibration now distinguishes scheduler-visible CPUs from the process cgroup quota used for normalization. The local Docker Desktop run used a Linux/WSL2 backend with a 4-core cgroup quota, 12 logical CPUs, and `GOMAXPROCS=12`; two controlled acceptance repeats passed after quality was recomputed from persisted evidence. This validates only that recorded container configuration and loopback fixture. It does not establish bare-metal Linux, cross-host equivalence, physical CPU identity, or application capacity. Cgroup limits are mutable runtime configuration and are not cryptographic host attestation.
