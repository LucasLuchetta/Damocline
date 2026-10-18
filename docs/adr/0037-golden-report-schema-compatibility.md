# ADR 0037: Keep golden fixtures for supported report schemas

## Status

Accepted (experimental).

## Context

Run reports evolve as measurement evidence is added. A reader that accepts historical JSON can still mislead if it treats a field absent from an old schema as though the value had been measured as zero, or if a newly emitted schema is not included in compatibility tests.

## Decision

Keep one synthetic JSON golden per supported HTTP run report schema from v1 through the current v13. Exercise each fixture through the bounded `insights.ReadRun` reader and JSON round-trip, assert a version-specific field, reject unknown future schemas, and test the absence-versus-zero distinction for response-body observations. Document the schema milestones and compatibility meaning in `docs/REPORT_SCHEMAS.md`.

## Consequences

Schema changes require updating the current version, reader, documentation and fixture suite together. This proves compatibility only for the preserved synthetic fixtures and fields asserted by tests; it does not constitute a migration that rewrites old files or prove every historical producer emitted identical JSON.
