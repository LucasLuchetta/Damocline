# ADR 0023 — Retain structured HTTP outcome diagnostics

## Status

Accepted (experimental); G0-14 remains open until the full injected failure matrix and cross-platform transport classifications are validated.

## Context

Failure reason strings help explain individual categories but do not summarize successful and failed HTTP status families or reconcile common transport/assertion outcomes across the aggregate, named steps and observation windows. A reported 429 is a target response; it must not be confused with the local request-rate governor, which delays rather than suppresses admitted work.

## Decision

Add exact response-code counts and derived 1xx–5xx family counts to each metrics set, alongside explicit counters for recognized redirect responses, HTTP 429, configured response assertions, timeouts, TLS and transport failures, and responses above the bounded body limit. Record these on aggregate, step and window metrics, and require schema-v8 family/status and step/aggregate reconciliation in quality checks. Classify TLS errors internally without retaining raw transport error strings; TLS handshake/certificate failures are separate from generic transport failures. Display summaries in CLI and local report views.

Redirect counter covers 300, 301, 302, 303, 305, 307 and 308 responses; redirects remain unfollowed. `rate_limited_responses` means HTTP 429 received from the target. Client rate-token waits remain distinct and no local starts are silently discarded by that limiter. A status or failure category is an observation and does not attribute cause.

## Consequences and limitations

- Status codes count responses visible to `net/http`; internal retries and interim responses may not map one-to-one to wire requests.
- `AssertionFailures` counts configured expected-status mismatches and required-text mismatches.
- TLS classification uses typed errors plus stable broad TLS/certificate markers but has not been calibrated against every OS resolver, handshake, or certificate failure.
- Exact status counts are bounded by the configured engine workload in actual runs; the report validator rejects malformed and inconsistent imported v8 counts.
- Local fixtures cover 302, 429, required-text failure, request timeout, bounded-body overflow and a failed HTTPS handshake against a local plaintext fixture. Broader injected DNS/connect and certificate failures remain future work.
