# ADR 0044: Reconcile iteration and journey ledgers

## Status

Accepted.

## Context

Run quality checked iteration counters and aggregate journey counters independently. A report could therefore contain individually plausible values while omitting a completed journey or a cancelled iteration, leaving the execution lifecycle unreconciled.

## Decision

For schema-v15 reports, require completed iterations to equal completed journeys and require started iterations minus completed iterations to equal explicitly cancelled journeys. Apply this rule during quality recomputation, including for controlled-reference evidence. Preserve older report interpretation where the schema may not guarantee the same persisted lifecycle fields.

## Consequences

Current reports with duplicated or missing journey outcomes become `INVALID`; a run whose interrupted journeys are explicitly accounted remains arithmetically valid but `LIMITED`. This checks lifecycle conservation, not whether each journey's business semantics or remote server effects are correct.
