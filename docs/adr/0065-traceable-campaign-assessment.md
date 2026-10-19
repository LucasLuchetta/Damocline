# ADR 0065: Traceable offline campaign assessment

## Status

Accepted

## Context

Campaign audit and business-demand planning were separate offline commands and
their outputs did not provide a single evidence index. Roadmap G3-11 and G3-12
require traceable conclusions while current statistical methods do not support
dependence-aware confidence or sustainable-capacity certification.

## Decision

Add a combined assessment artifact produced by `audit`, optionally including
the existing demand calculation when the caller supplies an exact demand goal.
Identify the exact saved campaign and each reviewed phase report with SHA-256
content digests. Include recorded workload fingerprints, run timestamps,
observation lengths, tested duration, observed levels, and named workflows with
observed failed journeys. State confidence as `NOT_ESTABLISHED`, host type as
`NOT_RECORDED`, and preserve unknowns. Do not sign, attest, or claim the digest
authenticates the producer.

## Consequences

The artifact links deterministic findings to the bytes that were reviewed and
can be reproduced offline. The same campaign may be reevaluated against
different explicit demand goals without generating traffic. A digest detects
content differences only when compared against a trusted copy; it is not an
authenticity mechanism. Tested duration describes the sum of required phase
durations, not wall-clock elapsed time or independent observation time.
Confidence and positive capacity certification remain unavailable.
