# ADR 0116: Reject unsupported embedded run-report schemas during campaign audit

## Status

Accepted

## Context

Campaign reports embed HTTP run reports for each phase. The campaign reader already rejected unsupported campaign schema versions, but audit did not bound the schema version of each embedded run. A future run schema could therefore be interpreted with current field and quality rules even if its semantics changed. That can turn unknown evidence into an apparently valid phase.

## Decision

Before evaluating campaign phases, offline audit rejects any required phase whose embedded run-report schema is below the first supported schema or above the current run-report schema. Warm-up phases remain excluded from this measurement gate because they do not contribute to campaign conclusions. Pending checkpoint phases receive the same validation when normalized for audit.

## Consequences

Auditors must understand every embedded report schema that contributes to the result. Newer producers remain usable after a compatible auditor is released. Unsupported evidence is reported as invalid rather than silently interpreted. This does not change the separate compatibility rules for campaign schemas or migrate historical reports.

## Validation

Audit regression coverage rejects zero and future run-report schema versions in a required observation phase. Existing campaign and capacity-plan tests retain compatibility behavior for legacy warm-up fixtures.
