# ADR 0006 — Separate-process reference check

- **Date:** 2026-10-17
- **Status:** Accepted

## Context

In-process HTTP fixture tests are useful for deterministic integration checks, but the fixture's work shares the generator process. A separate fixture process makes request counters independent and avoids attributing fixture Go-runtime work to the generator's Go-runtime samples.

## Decision

Provide a bounded developer command that starts the fixed loopback reference workload in a separate child process. Run a repeated arrival-rate matrix, a 30-second soak, a deliberately slow single-worker underdelivery stage, and a first-two-fail/then-recover stage. Reconcile planned, started, dropped, completed, cancelled, and server-observed requests. Record raw run reports and environment details locally. A passing matrix establishes only the measured generator envelope; it does not establish application capacity or a universal generator ceiling.

## Consequences

The generator's runtime and OS process samples exclude the fixture process, while both still compete for the same host resources. The tested envelope covers Windows and Linux, up to 100 arrivals/s, a 4 KiB response, and a bounded soak. It cannot characterize maximum generator throughput, every payload size, other host classes, or a real application's behavior. Re-run it on each load-generator host before interpreting a capacity campaign.
