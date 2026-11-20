# ADR 0151: Count transparent transport retries

## Status

Accepted.

## Context

ADR 0021 records per-step HTTP client phase timing through Go `httptrace`.
Quality checks required at most one request write and one completed outcome
per request attempt. Go `http.Transport` transparently retries an idempotent
request on a new connection when a reused keep-alive connection fails before
any response byte, typically because the server closed it at its idle
timeout. During one `http.Client.Do` call, `GetConn`/`GotConn` and
`WroteRequest` then fire once per connection used. On a target whose idle
timeout was shorter than the closed-loop think time, a step reported 49
attempts and completions but 50 writes and acquisitions. The run was marked
INVALID although the request outcome ledger was correct.

## Decision

- The engine counts a `GotConn` that follows an earlier acquisition in the
  same request attempt as one transparent transport retry and reports the sum
  as `http_trace.transport_retry_count` (run report schema v36).
- Outcome counters (`request_attempt_count`, `complete_count`,
  `first_byte_count`, `body_count`) remain per attempt. Connection, DNS,
  connect, TLS and write counters remain raw callback events, so their
  duration sums include the abandoned connection and write.
- From v36 the invariants are relaxed by exactly the retry count:
  writes plus failed writes ≤ completions + retries; acquisitions ≤
  attempts + retries; retries ≤ reused acquisitions and ≤ acquisitions.
- Pre-v36 reports must not carry the field (the reader rejects it and the
  quality check invalidates it) and keep their original invariants.
- `insights` reports retries as a CAUTION finding: a client-observed
  connection-lifecycle signal, not an application failure.

## Consequences

Real targets with short keep-alive idle timeouts no longer make runs
INVALID. Retries remain visible instead of being hidden. The engine cannot
tell whether the server processed the abandoned write; a retried
idempotent request may have reached the server twice. HTTP/2 retries that
reuse the same connection without a new acquisition are not detected.
Older reports that were INVALID for this reason stay INVALID.
