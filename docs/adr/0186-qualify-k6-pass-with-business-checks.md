# ADR 0186: qualify k6 pass with business checks

## Status

Accepted.

## Context

In the loopback wrong-marker control, all 51 k6 business checks failed on
HTTP 200 responses. The HTTP failure rate was zero and p95 met the
objective. The external comparator reported that k6 would pass.

## Decision

Treat reported failed checks as a veto independent of HTTP error and p95
objectives. Keep legacy summaries readable. When no failure is established,
withhold a passing verdict unless the generated script's named step checks
match the Damocline workload fingerprint. Require each expected step and
reconcile named totals with the aggregate checks and HTTP request count.

One generated check evaluates the expected HTTP status and, when
configured, the body marker. Configure the k6 HTTP response callback with
the same expected statuses. Use the existing optional `k6_would_pass`
field for the three outcomes: true, false, and absent if not assessable.

## Consequences

Older and custom summaries may still support failure or metric comparisons.
They do not establish a positive business verdict without the matching
named evidence. The contract checks reported declarations and counts; it
does not attest to the identity of the runner or the integrity of the
artifact. Additional custom checks that fail conservatively veto a pass.
