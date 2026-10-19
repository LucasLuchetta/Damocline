# ADR 0092: Version workflow scenario files

## Status

Accepted (experimental).

## Context

The scenario JSON format grew from one flat `steps` list to weighted `profiles`. Strict unknown-field decoding catches misspellings, but the file has no declared version and readers cannot distinguish old semantics from future structures. Migration must preserve existing local scenarios and must not overwrite their source.

## Decision

Keep versionless scenario JSON readable with its current `steps` and `profiles` semantics. Define scenario version 1 with a `workflows` list; each workflow has an ordered step list, a required positive weight, and the existing optional timing and per-actor authentication settings. Permit one workflow so a legacy flat step list can migrate without changing its order, methods, paths, or assertions.

Add `migrate-scenario --config OLD --out NEW`. It converts flat legacy steps into a `default` workflow of weight one, or renames legacy `profiles` into `workflows`, while preserving other scenario fields. It refuses unknown versions, ambiguous legacy input, identical input/output paths and existing output files. The source remains untouched. The ordinary loader rejects unknown future versions and mixed version-1/legacy workflow fields; no automatic rewrite occurs during a run.

## Consequences

Scenario versioning is separate from report schema versioning. Version 1 uses the same engine validation, safety limits, authorization gates, fingerprints and report evidence as the legacy representation. The migration command is a local file conversion; it does not validate target authorization, credentials or workload representativeness. Future incompatible scenario changes require a new version and an explicit migration policy.
