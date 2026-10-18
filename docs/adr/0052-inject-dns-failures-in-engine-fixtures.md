# ADR 0052: Inject DNS failures in native-engine fixtures

## Status

Accepted.

## Context

Testing a reserved hostname through the machine's configured resolver is not deterministic and can depend on network configuration. The native HTTP engine must also expose whether a resolution attempt occurred before it reports a transport failure.

## Decision

Permit the package-private engine runner to receive a cloned `http.Transport` for deterministic fixtures. Use Go's resolver with an injected loopback DNS dial function and a bounded local UDP responder that returns NXDOMAIN. Assert that DNS trace callbacks and the resolver query ledger observe the lookup, transport diagnostics count the failed request, and no destination TCP connection or HTTP response is observed.

## Consequences

The production `Run` path continues to construct its default cloned transport. The fixture exercises the real engine and standard HTTP transport while controlling only name resolution. This proves NXDOMAIN classification for this local fixture; DNS-specific failure categories, timeout/SERVFAIL behavior, resolver behavior under load, and real network performance remain unvalidated.
