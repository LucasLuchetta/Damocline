# ADR 0059: Record independent per-profile TCP connection counts

## Status

Accepted

## Context

Calibration schema v5 reconciled client arrivals and response bodies against an independent child-server request and byte ledger. It recorded the declared connection policy, but the artifact did not establish how many server-accepted TCP connections actually carried each calibrated request shape. Existing engine tests observed connection events separately, but that evidence was not persisted or required by the end-to-end acceptance lab.

## Decision

Configure the loopback reference HTTP server's `ConnContext` hook to assign a unique identity to each accepted TCP connection. When a valid synthetic fixture request reaches the handler, associate that connection identity with its fixed request path. The independent snapshot reports the number of distinct accepted connections that carried each path, separate from request, completion, and body-byte counts.

Advance preflight calibration to schema v6. Persist each workload profile's observed TCP connection count and require an exact match for a passing artifact: one connection for the serialized reusable profile, or one connection per planned request for `new_per_request`. Keep schema-v1 through v5 artifacts readable with their original evidence scope. Advance the repeated acceptance manifest to schema v2 and require the connection ledger for every one of the 30 exact shape, policy, and rate profiles.

## Consequences

The calibration record can now detect when a declared connection policy did not produce the expected connection behavior against the controlled fixture. This expectation is specific to the 20 ms loopback response, exact tested rates, and one-step profile execution. It does not qualify TLS, HTTP/2, remote endpoints, or application capacity. Connection IDs are ephemeral and only aggregate counts by fixed synthetic path are persisted; no socket addresses or customer data are recorded.
