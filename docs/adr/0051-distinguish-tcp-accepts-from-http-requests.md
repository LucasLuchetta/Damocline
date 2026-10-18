# ADR 0051: Distinguish TCP accepts from HTTP requests

## Status

Accepted.

## Context

Transport failures can occur after a TCP connection is accepted but before the server parses an HTTP request. Reconciliation that treats accepted sockets as received requests would silently invent server-side work and distort failure counts.

## Decision

Use bounded loopback fixtures to induce refused connections and TCP resets through the native engine. Require transport-failure classification and the absence of an invented HTTP response status. In reset tests, count accepted TCP connections independently and report that ledger as connection evidence only; do not assert it equals HTTP request receipt.

## Consequences

The failure matrix covers two local pre-response transport cases without asserting a request reached the application. Resolver-injected NXDOMAIN is covered separately in ADR 0052. DNS timeout/SERVFAIL and broader TLS-certificate behavior remain gaps. These fixtures establish classification behavior, not network-path performance or a generator envelope.
