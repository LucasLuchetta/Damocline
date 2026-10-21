# ADR 0124: Require target identity in saved run reports

## Status

Accepted

## Context

Run reports are evidence about measurements made against a configured target.
The report reader previously accepted an artifact with its `target` field
omitted, because JSON decoding supplied the Go string zero value. The report
could still be assessed and displayed, leaving no identity for the system that
received the measured workload.

## Decision

Require a non-empty `target` in every supported run-report schema. Reject a
missing or empty target at the shared typed-report validation boundary used by
run inspection, dashboard, export, and campaign phase validation. Preserve
existing historical schemas when they contain target identity. Do not infer a
target from a workload fingerprint, scenario name, or external metadata.

## Consequences

- A report cannot be presented as attributable run evidence when the target
  identity is absent.
- Tests remove `target` from every supported run-schema fixture and require
  rejection; current and historical fixtures with the field remain readable.
- The recorded target is still a declaration, not cryptographic attestation
  that traffic reached that system. Source authenticity and deployment
  identity remain separate evidence gaps.
