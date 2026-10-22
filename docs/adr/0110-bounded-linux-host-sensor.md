# ADR 0110: Serve bounded Linux host telemetry through an optional sensor

## Status

Accepted

## Context

The load generator cannot inspect a remote application's host. A separate, opt-in process is needed to make selected Linux resource observations available during an authorized campaign without embedding host access into the generator.

## Decision

Add `damocline-sensor` with a loopback-only default listener and require TLS for non-loopback binds. Require a bearer token and experiment identifier, permit one bounded collection at a time, and expose host CPU, memory, swap, disk and network counters. An explicitly selected process may add RSS/CPU and visible cgroup-v2 limits and memory events. Unavailable kernel/controller readings remain unknown.

## Consequences

- The generator can consume host observations through a small HTTP interface without shell access to the target.
- Sensor sampling is bounded and authenticated, while host telemetry remains descriptive and does not attribute a bottleneck or certify capacity.
- Remote deployments must configure TLS and protect the bearer token; sensor and generator should run with only the permissions needed for their declared scope.
