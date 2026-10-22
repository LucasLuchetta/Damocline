# ADR 0128: Reject missing exploration evidence before decoding

## Status

Accepted

## Context

Go's JSON decoder assigns zero values when persisted fields are omitted. An exploration report could omit its tested bounds, repeat count, outcome, or probe ledger and still be decoded by offline assessment or redacted export. This makes absence indistinguishable from an explicitly recorded zero and weakens reproducibility.

Campaign and run artifacts already have raw-byte evidence checks. Exploration artifacts need the same protection at the reader boundary, with schema-aware requirements and nested probe/run checks.

## Decision

Add raw JSON validators for closed-loop and open-arrival exploration schemas. Require their schema-defined top-level fields, a JSON array for probes and notes, each probe's load unit/status/run array, and a schema marker for each embedded run. Apply the checks before typed decoding in offline `goal` assessment and redacted export.

Explicit numeric zero remains present evidence and is not rejected by this structural layer. Empty arrays remain valid. Schema version controls the required field set; unsupported versions fail closed. Semantic range and consistency validation remains with the existing assessment and export validators.

## Consequences

- Missing exploration fields can no longer silently become Go zero values in these offline read paths.
- Goal and export behavior now rejects malformed artifacts before producing assessments or redacted outputs.
- This does not authenticate artifact producers or prove workload representativeness, generator independence, or application capacity.
- Profile verification and assessment-artifact readers still require a separate presence-coverage audit.
