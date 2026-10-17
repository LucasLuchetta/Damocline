# ADR 0001 — Native Go core

- **Date:** 2026-10-16
- **Status:** Accepted

## Context

Damocline needs concurrent HTTP execution, reproducible workloads, precise scheduling, explicit safety policies, low-friction local installation, and cross-platform builds. The owner also wants an independent implementation rather than a wrapper around an existing benchmark engine.

## Decision

Use **Go** for the CLI and load-generation engine. Initially rely on the standard library for networking and concurrency, while implementing our own workload scheduling, virtual-user lifecycle, measurements, and reporting.

Keep the core independent from any web UI. Only add external dependencies with a documented technical need and license review.

## Alternatives

- **Delegate generation to a third-party engine:** less initial engineering work, but does not meet the native-engine and measurement-ownership goals.
- **Rust:** technically viable, but greater initial development complexity for the planned contributors and scope.
- **Python-only core:** accessible but less suitable for the desired native single-binary engine design.

## Consequences

A self-contained cross-platform CLI is achievable. However, we must independently establish scheduling accuracy, load-generator limits, safe traffic bounds, correct latency/error accounting, and reproducibility. Using Go does not guarantee benchmark accuracy.

Other tools may be used as calibration and comparison references, never silently delegated to at runtime.
