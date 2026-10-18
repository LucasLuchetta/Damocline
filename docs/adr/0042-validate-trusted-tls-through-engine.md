# ADR 0042: Validate trusted TLS through the native engine

## Status

Accepted (experimental).

## Context

The bounded transport reference matrix exercised local TLS with a standalone HTTP client, while the native engine had a controlled TLS failure case but no successful trusted TLS request with an independent server-side ledger. That left the engine's successful TLS path unverified by the reference fixture.

## Decision

Add a bounded test using an ephemeral trusted `httptest` TLS server and execute requests through `engine.Run`. Count requests independently at the server and compare received and completed counts with the engine's completed request ledger. Keep the test certificate and target loopback-only and scoped to the test process.

## Consequences

The test validates the native engine's successful TLS request path and counter reconciliation for this local fixture. It does not establish TLS handshake or request timing accuracy under load, remote trust behavior, broad transport equivalence, or a performance envelope. TLS calibration and the remaining G0-03 transport cases stay open.
