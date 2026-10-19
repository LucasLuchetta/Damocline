# ADR 0095: Record closed-loop campaign calibration preflight

## Status

Superseded by [ADR 0097](0097-exact-closed-loop-vu-calibration.md) (experimental).

## Context

At the time of this decision, calibration records contained only exact arrival-mode workload profiles. Closed-loop campaigns schedule virtual users, which are a different workload unit; treating arrival-rate profiles as VU evidence would be incorrect.

## Decision

Add optional `campaign --calibration FILE` evidence and `--require-calibration`. Evaluate each distinct planned base and spike VU level before dry-run output or execution. The initial reference record had no closed-loop profiles, so its envelope was BLOCKED and required calibration stopped before any phase. Persist the envelope, source path, and required-policy bit in closed-loop campaign schema v12 for review. Do not infer coverage from arrival rates.

When calibration is provided without the required policy, the campaign proceeds and retains BLOCKED evidence as a warning. The record stores only the operator-supplied file path, never calibration credentials or target data.

## Consequences

The initial implementation let operators explicitly require calibration while all reference records remained BLOCKED. [ADR 0097](0097-exact-closed-loop-vu-calibration.md) later adds independently validated exact VU fixture profiles, allowing matching levels to become READY. Existing campaigns remain opt-in. The persisted source is local operator input and is not proof of fixture identity or target capacity.
