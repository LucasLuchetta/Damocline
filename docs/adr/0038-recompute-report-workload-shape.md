# ADR 0038: Recompute the report-reconstructable workload shape

## Status

Accepted (experimental).

## Context

Calibration equivalence used the run's persisted workload fingerprint, but that digest was not checked against the sanitized step metadata retained in the report. A changed saved path or method could therefore coexist with the original digest. Reports intentionally omit request templates, header values, body content, assertions, and extracted values, so a general digest cannot be reconstructed safely from a report.

## Decision

For schema-v11 and newer reports, recompute the fingerprint only for the narrow single-step `GET /work` reference shape when the saved step is named `work` in the default profile and there are no explicit profiles, separate sessions, or think time. Require the persisted digest to equal both this recomputation and the calibration fingerprint. Return `OUTSIDE_REFERENCE` for changed reconstructable step fields and `UNVERIFIED` when the report lacks fields or the workload is outside the supported subset. Do not upgrade overall quality based on this dimension.

## Consequences

This detects inconsistencies in the report fields that are persisted for the reference subset. It does not prove report authenticity or endpoint identity, nor does it validate request shapes whose information is omitted from reports. A broader workload matrix requires a separately versioned, privacy-safe evidence representation before it can be assessed.
