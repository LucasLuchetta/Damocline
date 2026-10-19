# ADR 0096: Private local campaign profiles

## Status

Accepted

## Context

Repeating a campaign currently requires reproducing its scenario and phase flags manually. Reusable profiles should preserve workload, bounded campaign controls, stability and generator guardrails, while trial outcomes must remain evidence from a particular execution. Reusable configuration may contain private target details and therefore must stay local.

## Decision

Store versioned profile JSON under `.damocline/profiles/`, which is gitignored. Profile schema v1 includes the full scenario and campaign settings, plus migration from schema-less/v0 data. Saving an existing name requires `--replace`; deletion is explicit. Literal HTTP header values are rejected in legacy and versioned workflow scenarios; environment variable references remain unresolved. Consent, credentials, calibration paths, and application-metrics endpoints are supplied again for each run.

Running a profile uses the existing campaign engine and report schema. A non-dry execution requires fresh `--authorized`; mutation methods, plaintext remote HTTP, long-run staging confirmation, and calibration remain separately opted in at execution. The default output is a uniquely timestamped report in `.damocline/reports/`, preserving every trial and phase checkpoint.

## Consequences

Profiles improve reproducibility but do not freeze target state, credentials, application revisions, or environment conditions. The scenario may refer to external body data and environment variables which must be available at execution. A profile is not evidence of sustainable capacity; only its resulting report can support a bounded claim, with scenario, environment, pass criteria, observation window, and uncertainty.
