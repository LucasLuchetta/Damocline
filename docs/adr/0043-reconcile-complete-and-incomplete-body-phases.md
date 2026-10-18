# ADR 0043: Reconcile complete and incomplete body-read phases

## Status

Accepted.

## Context

The engine's HTTP trace counted any body read that reached the first response byte as a completed body read, then separately counted a read error as incomplete. Quality validation correctly requires complete and incomplete observations to be disjoint, so a locally reproduced truncated response caused otherwise consistent request accounting to become `INVALID`.

## Decision

Increment `body_count` only when the bounded response-body read succeeds. When a response reaches the first byte but its body read returns an error, increment `body_incomplete_count` instead. Continue recording elapsed body-read time for either outcome. Add a loopback server fixture that declares a larger body than it writes, then compare client request/failure/body counters with independent server request/handler counters.

## Consequences

The HTTP phase counters now conserve one body-read outcome per response with a first byte. A truncated response remains an observed request failure and can be `LIMITED` without becoming `INVALID` solely because the trace double-counted its body phase. This fixture establishes only the named local truncation behavior; it does not cover resets across all transports, network faults, or timing accuracy under load.
