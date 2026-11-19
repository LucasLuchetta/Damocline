# ADR 0136: preserve recorded CPU observation presence

## Status

Accepted.

## Context

A persisted run could retain `process_cpu_valid: true` but omit its window-mean
ratio. Ordinary Go decoding produced zero, allowing a headroom screen to miss
known process pressure. The same ambiguity affects numeric observations behind
a valid Go CPU claim. The shared raw metric validator already reaches direct
runs, completed/pending campaign phases and exploration probes.

The mean process CPU field arrived during report schema v11: master commit
`9d30d8e` introduced the window-mean implementation while still emitting v11;
its predecessor also emitted v11 without that field. Schema v12 is therefore
the first complete version whose producers always wrote it. Requiring the mean
on every v11 artifact would reject legitimate historical files.

## Decision

For run schemas v12+, require explicit non-null mean/peak process CPU ratios
when process CPU is declared valid, and busy/capacity/ratio observations when
Go CPU is declared valid. Keep absent generator evidence and unavailable sources
readable as unknown; explicit numeric zero remains valid presence. Existing
semantic validation still checks sample counts, ranges, accounting and quality.
No report schema bump or source-file rewrite is needed to enforce these existing
producer fields.

Decode an absent/null historical process mean as an optional
`mean_process_cpu_unavailable` diagnostic when process reads are otherwise valid.
Preserve that marker during re-encoding, so an unknown mean cannot become
measured idle after a round trip. Native summaries have explicit Go values and
omit the diagnostic. A known adjacent-sample peak remains available independently.

Require an available mean in shared CPU headroom screening, limit quality when
it was not recorded, retain independent peak-pressure signals and describe the
unknown mean in insights. Redacted format-v2 exports add an explicit
`mean_process_cpu_valid` boolean alongside their existing source flags.

## Validation and limits

Deterministic tests remove or null each claimed-source observation across direct
runs, both campaign families, pending trials and exploration. They retain
explicit zeros, unknown sources and legitimate pre-v12 absence. Decoder tests
check unknown/known-zero behavior and round-trip stability; presentation/export
tests distinguish an unavailable mean from a known peak. A compile overlay
restoring the old raw validator must fail the missing-mean regression.

This validates presence and interpretation, not producer authenticity, snapshot
freshness, physical host identity or capacity. Historical files retain their
recorded versions and source bytes. Canonical decoded evidence IDs may change
when older unknowns are represented correctly; original-byte and auditor hashes
remain separately available for pinned replay. Other artifact-field contracts
and remaining G0 acceptance gaps still require work.
