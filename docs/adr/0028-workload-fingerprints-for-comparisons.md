# ADR 0028: Require workload fingerprints for controlled comparisons

## Status

Accepted.

## Context

The offline comparator compared report-visible route and profile summaries,
but run reports did not preserve headers, assertions, body source declarations,
or response extraction rules. Two runs could therefore receive a controlled
comparison label while using materially different step definitions. Older
reports also lacked a complete workload identity.

## Decision

Schema-v11 run reports include a SHA-256 fingerprint over the declared
profiles, ordered steps, methods, paths, header declarations, expected status
codes, text assertions, body environment-variable names and body byte lengths,
extraction rules, and session/synthetic-identifier modes. Header names and
expected status sets are canonicalized before hashing. The digest is opaque;
raw headers, paths and assertion text are not added to report fields by this
feature. Resolved credentials, environment-backed body contents, extracted
response values, and generated synthetic identifiers are excluded from the
digest.

The offline comparator treats runs as controlled only when both reports are
schema v11 or newer, fingerprints are well-formed and equal, and the declared
workload revision, environment, target, load controls, authentication mode,
SLOs, timing, and workload mix match. Application revision is recorded as the
treatment; when it differs, the output explicitly says the runs do not
represent an equivalent-build comparison and still makes no causal claim.
Missing or different declared workload conditions produce
`NOT_COMPARABLE`; matching declared conditions are labeled
`MATCHED_DECLARED_WORKLOAD`, not a controlled or causal comparison. Every
comparison states that run reports do not embed loopback preflight evidence or
authenticated generator host identity. Raw metric differences remain
descriptive observations.

## Consequences

Changes to a declared request or journey shape now invalidate controlled
comparison eligibility even when the report-visible route summary is unchanged.
Legacy reports remain readable but are not eligible as controlled comparisons.
This does not establish that the environment-backed body values or resolved
credentials were equivalent, or that the same physical generator host was
used. Matching preflight fixture evidence and build/runtime labels do not prove
host identity or equal headroom. It does not establish workload
representativeness or causal effect.
