# ADR 0082: Gate selected runs on exact generator calibration

## Status

Accepted (experimental).

## Context

The calibration artifact contains exact, bounded open-arrival workload profiles,
but `run --calibration` only attached their assessment after traffic. Capacity
campaign operators could therefore start rates or request shapes outside the
local fixture's measured generator envelope without an explicit preflight stop.

## Decision

Add `run --require-calibration` as an opt-in gate requiring `--calibration`.
The gate is supported only for open-arrival workloads covered by a current,
passing schema-v6-or-later local reference. It checks exact arrival rate,
request shape, request rate ceiling, worker budget, timeout, connection policy,
timing controls, calibration age and runtime metadata. It never interpolates
rates. A failed gate returns before the engine starts.

Add optional `arrival-campaign --calibration`. When supplied, it checks every
planned base and spike rate before any phase starts, and blocks if any rate is
outside the exact envelope. Phase runs retain their sanitized calibration
evidence. Dry-run applies the same check. Without the option, the campaign
prints that no calibration readiness check was run.

## Consequences

Only local synthetic fixture delivery is qualified. A ready gate does not
identify the configured endpoint, validate its response distribution, prove
physical host identity, or establish sustained headroom for campaign phases
longer than the short reference profile. Reports remain limited for ordinary
targets. Closed-loop campaigns and arrival exploration are not gated by this
decision.
# ADR 0082: Gate selected runs on exact generator calibration

## Status

Accepted (experimental).

## Context

The calibration artifact contains exact, bounded open-arrival workload profiles,
but `run --calibration` only attached their assessment after traffic. Capacity
campaign operators could start rates or request shapes outside the local
fixture's measured generator envelope without an explicit preflight stop.

## Decision

Add `run --require-calibration` as an opt-in gate requiring `--calibration`.
The gate is supported only for open-arrival workloads covered by a current,
passing schema-v6-or-later local reference. It checks exact arrival rate,
request shape, request rate ceiling, worker budget, timeout, connection policy,
timing controls, calibration age and runtime metadata. It never interpolates
rates. A failed gate returns before the engine starts.

Add optional `arrival-campaign --calibration`. When supplied, it checks every
planned base and spike rate before any phase starts, and blocks if any rate is
outside the exact envelope. Phase runs retain sanitized calibration evidence.
Dry-run applies the same check. Without the option, the campaign prints that no
calibration readiness check was run.

## Consequences

Only local synthetic fixture delivery is qualified. A ready gate does not
identify the configured endpoint, validate its response distribution, prove
physical host identity, or establish sustained headroom for campaign phases
longer than the short reference profile. Reports remain limited for ordinary
targets. Closed-loop campaigns and arrival exploration are not gated by this
decision.
