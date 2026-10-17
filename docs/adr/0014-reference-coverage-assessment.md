# ADR 0014 — Fail-closed workload-to-reference coverage checks

## Status

Accepted as an experimental **offline evidence comparison**; not an application-capacity acceptance gate.

## Context

ADR 0013 stores a versioned, internally validated description of the 4 KiB loopback fixture. A globally passing reference run is often mistaken for proof that unrelated remote HTTPS workloads can be generated accurately. In fact, the reference measured specific rates, a fixed payload and GET-only arrivals without production authentication or network complexity.

## Decision

Expose a read-only `damocline-calibrate --check-scenario <file> --calibration <file>` path that never runs HTTP traffic or opens sockets. Parse a bounded scenario JSON and reference artifact; require a self-consistent v1 preflight plus matching per-stage run evidence. Do not interpolate between tested rates or confuse closed-loop VUs with scheduled journey arrivals.

Report each dimension as `MATCHED`, `OUTSIDE_REFERENCE` or `UNVERIFIED`:

- calibrated fixture check result and **exact** observed arrival rates
- workload model, actor budget, global rate safety ceiling and single-step GET-only request mix
- tested observation duration, configured timeout/think/ramp and loopback transport
- missing response-size distribution, connection/server identity and generator/environment equivalence

Return structured JSON with top-level `INCONCLUSIVE` and a nonzero CLI exit code, even if the declared scenario shape resembles the fixture. Recorded rates represent test observations, not a generator maximum, and not a customer workload acceptance.

## Safety

The comparison does not resolve DNS, inspect a target URL, use cookies or read authentication environment variables, run the fixture or produce traffic. It does not print scenario URLs, names, paths, header values, or user-provided tokens. Local calibration files remain private; the SHA-256 fixture fingerprint is **not** authentication or a security signature.

## Deferred requirements

A positive application-specific readiness verdict is **not possible** using current v1 loopback metadata. G0-03 must validate multiple payload/transport/connection configurations and G0-08 must check host identity, age, workload equivalence, and generator health on the actual machine. G0-02 remains incomplete until those measured dimensions can support a positive decision without inference.
