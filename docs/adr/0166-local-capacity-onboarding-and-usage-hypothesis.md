# ADR 0166: Local capacity onboarding and usage hypothesis

Project local HAR previews into same-origin GET and HEAD candidates.
Discard request bodies, headers, query strings, cookies, and third-party
entries. Use placeholders for sensitive paths and inferred session-bound
steps. Operator review is required because a capture alone cannot
establish safe route semantics.

An explicitly requested executable receives a bounded session count and
returns a JSON array of cookie headers. Supply literal arguments without
shell interpolation, bound runtime and output, validate canonical
distinctness, discard hook diagnostics, and keep cookies in memory.

A people estimate is a separate hypothesis based on successful measured
HTTP actions per minute divided by declared or locally summarized action
intensity. It assumes equal action units and workflow mix and has no effect
on the measured or audited verdict.
