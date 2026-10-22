# ADR 0125: Reconcile TLS handshakes to accepted server sockets

## Status

Accepted

## Context

The schema-v10 local reference profile at 40 journey starts/s completed 200
requests with matching response-byte ledgers, while the client reported two
successful TLS handshakes and the server's request-attributed TCP ledger
contained one connection. The server ledger intentionally records only
connections that carry a request. Concurrent HTTP transport dials can complete
a TLS handshake before another connection wins the race to carry the requests;
the unused connection is invisible to that ledger. Requiring handshake count
to equal request-carrying TCP connections therefore compared different
quantities and incorrectly failed a valid fixture run.

## Decision

In calibration preflight schema v11, count TLS-wrapped sockets when the local
reference server accepts them. Keep this independent counter separate from the
per-profile set of TCP connections that carry HTTP requests. Require client TLS
handshakes to reconcile with accepted TLS sockets for the controlled HTTPS
fixture, and require request-carrying HTTPS connections not to exceed accepted
TLS sockets. HTTP profiles require zero TLS sockets and handshakes.

Advance the repeated acceptance manifest to v7 and require preflight schema
v11. Preserve schema-v1 through v10 validation semantics for historical
artifacts.

## Consequences

- Accepted HTTPS connections that carry no request remain visible in the
  evidence instead of being mislabeled as a client/server mismatch.
- Request, response-byte, request-carrying TCP, accepted TLS socket, and client
  handshake ledgers remain independently inspectable.
- The local fixture does not prove behavior for arbitrary proxies, TLS
  termination layers, HTTP/2, or remote targets. This remains generator
  validation only, not application-capacity evidence.
- Tests include an accepted TLS socket that completes the handshake without
  sending an HTTP request, and schema-v11 artifact checks distinguish it from
  request-carrying TCP connections.
