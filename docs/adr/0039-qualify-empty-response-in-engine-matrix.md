# ADR 0039: Qualify empty HTTP responses in the native-engine matrix

## Status

Accepted (experimental).

## Context

The independent transport fixture covered zero-byte responses, while the native engine's controlled response-size matrix covered positive sizes only. The shared reference server used a default non-empty marker body when its configured response size was zero, so a zero setting could not express an empty successful response.

## Decision

Add an explicit programmatic `EmptyResponse` fixture option. Keep the existing default marker response unchanged and use the option only for a bounded local native-engine test. Assert exact completed-body count, min/max bytes and total bytes alongside the server's independent request ledger.

## Consequences

The engine regression matrix now validates the empty-body boundary for the tested loopback run. This does not calibrate throughput, TLS success, DNS, resets, incomplete responses or other payload distributions, and it does not expand the positive quality envelope.
