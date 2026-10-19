# ADR 0086: Export descriptive evidence through an allowlist

## Status

Accepted (experimental).

## Context

Native run and campaign reports intentionally retain operational detail needed
for offline validation, including target origins, route templates, scenario
labels, absolute timestamps and externally supplied metric names. Sharing those
files can disclose private infrastructure or workload identifiers.

## Decision

Add an explicit `report export` command that validates run, closed-loop
campaign, open-arrival campaign, closed-loop exploration, or open-arrival exploration input and writes a separate
`damocline-redacted-evidence-v1` allowlist document. Preserve bounded workload
units, phase/window ledgers and aggregate HTTP/journey measurements. Replace
workflow and step labels with deterministic per-report aliases. Omit target
origins/routes, free text, revisions, environment declarations, fingerprints,
absolute timestamps, and external metric names and scalar values. Keep the
source file unchanged and atomically write only the caller-selected destination.

## Consequences

The export supports descriptive review while reducing direct disclosure. It is
not a native report and cannot be used by capacity assessors; the original
artifact remains necessary for full audit. External metric values are omitted
because generic custom telemetry cannot be assumed free of identifiers.
Numerical aggregate measurements and durations are retained, so a user should
still review them for sensitive business information before publication.
