# ADR 0021 — Report bounded HTTP client phase timings

## Status

Accepted (experimental); G0-12 remains open until all phase boundaries and their accuracy are calibrated.

## Context

The engine reports full request and journey elapsed times, but cannot currently show when connection setup or response receipt contributed to a slow HTTP step. Any decomposition must preserve the distinction between client-observed timing and server-side work, and must handle reused connections without manufacturing zero-duration DNS/TCP/TLS phases.

## Decision

Use Go `net/http/httptrace` within each request to accumulate DNS, TCP connect, TLS handshake, first-response-byte, request-write, and body-receive observations. Measure full client duration around `http.Client.Do` and body read. Measure the configured rate-token wait separately, outside request latency. Store per-step totals and callback counts in the run report. Schema v7 introduces these fields; older reports retain unknown phase evidence. Publish the boundaries in the measurement guide and expose them in CLI and local report views. Reconcile `request_attempt_count` and `complete_count`, phase callback starts/completions, and body-complete/incomplete observations before accepting v7 timing evidence.

`GotFirstResponseByte` is a client end-to-end boundary and does not isolate server processing. The standard transport does not expose a pure request queue wait; time through `WroteRequest` is reported only as an explicitly labeled request-write boundary. Counts indicate observed callbacks. Reused connections normally have no DNS/connect/TLS callback, which is missing phase evidence rather than a zero measurement. Incomplete callbacks are not assigned durations. All report values omit request URLs, headers and bodies.

## Consequences and limitations

- The trace totals are diagnostic sums, not per-phase distributions; phases may overlap and first-byte includes earlier work.
- Rate-wait total may include a cancelled wait that did not start an HTTP request; request counters remain distinct.
- Callback/timestamp accuracy is not calibrated under load, and resolver behavior may vary by platform.
- This feature does not establish server-side time, network causality, capacity or a valid generator envelope.
- Local deterministic fixtures cover successful response/body boundaries, a transport failure, an interrupted body, and connection reuse; DNS/TLS fault matrices and accuracy tolerances remain future G0-12 work.
