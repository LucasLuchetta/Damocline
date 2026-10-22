# ADR 0121: Bind open-arrival goal reports to declared identity and auditor

## Status

Accepted

## Context

Open-arrival campaigns retained workload revision, application revision, and
environment in each phase's run configuration, and offline goal assessment
already replayed the phase evidence. The resulting objective assessment did
not expose those declarations or identify the exact auditor executable that
interpreted the campaign. Its generated replay command pinned source bytes but
could silently produce a new assessment with a different auditor build.

## Decision

Open-arrival campaign goal assessments summarize the three identity fields
across all non-warm-up phases. Missing identity is `UNKNOWN`; changed identity
is `CHANGED`. The assessor examines all required phases; an observed change is
reported as `CHANGED` even when another phase also lacks identity fields, so an
early missing value cannot hide contradictory evidence. When both conditions
occur, separate findings disclose both. Neither status can support a positive
exact-target result. The report records the auditor version and exact
executable SHA-256.

Generated replay commands require both the saved campaign's exact byte
SHA-256 and the expected auditor executable SHA-256. The CLI verifies these
before writing outputs and rejects auditor pinning for artifact families that
do not record the auditor identity.

## Consequences

- Engineers can inspect the declared workload, application, and environment
  associated with the open-arrival evidence.
- Historical campaigns remain readable; missing phase declarations remain
  unknown and do not become favorable evidence.
- Replaying with a different auditor binary fails before outputs are written.
- Digests detect changes relative to the assessment but are unkeyed; they do
  not authenticate authorship, build origin, target state, or declarations.
