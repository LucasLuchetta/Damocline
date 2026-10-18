# ADR 0032: Compare observed response sizes with the fixed preflight fixture

## Status

Accepted (experimental).

## Context

Schema-v12 run reports now contain bounded per-step response-body size observations. The local preflight fixture has one measured healthy response size (4 KiB), while `run --calibration` previously reported response shape as wholly unknown without considering response bytes actually observed in the run.

## Decision

When a validated fixed-fixture calibration is attached, derive an `observed_response_size` coverage dimension from the report's schema-v12 per-step body ledgers. Mark the dimension `MATCHED` only when every named step has at least one complete body and every complete body's measured min/max equals the fixture's fixed 4 KiB size, with no incomplete or oversized observations. Mark a complete differing size or oversized body `OUTSIDE_REFERENCE`; mark missing, incomplete, or malformed evidence `UNVERIFIED`. Recompute the dimension during quality assessment and reject persisted status/reason values that disagree with run fields. Keep the overall coverage `INCONCLUSIVE` and quality LIMITED while response timing distribution and controlled endpoint identity remain unverified.

## Consequences

The report distinguishes observed byte-size agreement from the broader response-shape and endpoint-equivalence claims. It provides no interpolation to other sizes and no positive capacity or measurement certificate. The local report and CLI assessment expose the sanitized dimension name, status and reason without routes or response data.
