# ADR 0093: Measure client connection acquisition waits

## Status

Accepted (experimental).

## Context

The HTTP engine records scheduler delivery, rate waits, DNS, connection establishment, TLS, response timing, and generator health. It did not expose time spent acquiring a connection from `http.Transport`, so a configured client-side connection limit could delay delivered work without appearing in diagnostics.

## Decision

Add optional `max_connections_per_host` (1–100) to scenarios. Omission retains the worker-scaled default of `users + 4`. Explicit limits are applied to the shared transport, recorded in schema-v28 reports, and included in the workload fingerprint.

Use `httptrace.GetConn` and `GotConn` to retain acquisition count and duration, reused-connection count, and reused-connection acquisitions taking at least 10 ms. Emit a safe diagnostic signal when such a wait is observed. Keep raw addresses and route names out of signal text. Preserve wait count and total wait duration in redacted exports alongside the safe configured cap.

## Consequences

Operators can deliberately bound client connections and see when acquisition of a reused connection was delayed. This helps identify a possible generator-side queue that could limit delivered concurrency. It does not prove the configured cap caused the wait, does not establish target-side connection-pool exhaustion, and does not identify server root cause. Other diagnostics and ordinary target runs remain subject to their existing evidence limits.

## Validation

A local loopback fixture runs two actors behind a one-connection cap while the first response is delayed. It verifies that the client records a reused-connection wait and reconciles successful client requests against an independent server request counter. The fixture is bounded and uses no external target.
