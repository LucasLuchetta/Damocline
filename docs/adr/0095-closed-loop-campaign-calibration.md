# ADR 0095: Record closed-loop campaign calibration preflight

## Status

Accepted (experimental).

## Context

Calibration records currently contain exact arrival-mode workload profiles. Closed-loop campaigns schedule virtual users, which are a different workload unit; treating arrival-rate profiles as VU capacity evidence would be incorrect.

## Decision

Add optional `campaign --calibration FILE` evidence and `--require-calibration`. Evaluate each distinct planned base and spike VU level before dry-run output or execution. Since the current reference record has no closed-loop profiles, its envelope is BLOCKED and required calibration stops before any phase. Persist the envelope, source path, and required-policy bit in closed-loop campaign schema v12 for review. Do not infer coverage from arrival rates.

When calibration is provided without the required policy, the campaign proceeds and retains BLOCKED evidence as a warning. The record stores only the operator-supplied file path, never calibration credentials or target data.

## Consequences

Operators can explicitly require calibration and receive a safe preflight failure until closed-loop fixture profiles exist. Existing campaigns remain opt-in. The persisted source is local operator input and is not proof of fixture identity or target capacity. Future closed-loop profiles require independent exact profile semantics and validation before this gate can become READY.
