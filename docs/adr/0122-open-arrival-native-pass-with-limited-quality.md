# ADR 0122: Preserve native open-arrival passes with limited measurement quality

## Status

Accepted

## Context

The open-arrival campaign runner defines a trial `PASS` from its required phase
outcomes, delivery classification, and stability screens. The offline capacity
assessor applies an additional bar: every required phase must have
`VALID_WITHIN_ENVELOPE` measurement quality before it can support a positive
capacity decision. Without a local calibration record, a real loopback
campaign can pass its native phase SLOs but remain below that capacity-quality
bar. The assessor treated that valid campaign result as contradictory and
refused to generate a report.

## Decision

Keep the campaign runner's `PASS` meaning tied to its recorded native phase
outcomes. During offline assessment, accept a saved `PASS` when every required
phase passes its native SLO, delivery, stability, coverage, and generator
checks but measurement quality alone is below the positive-assessment bar.
Return `INSUFFICIENT_EVIDENCE`, retain the native observations, and state that
measurement quality is insufficient. Continue rejecting a saved `PASS` that
conflicts with any native phase outcome, delivery, stability, coverage, or
generator evidence.

## Consequences

- A valid open-arrival campaign remains auditable even when it cannot establish
  capacity.
- Native SLO observations and the capacity decision have distinct, explicit
  meanings.
- Uncalibrated external campaigns cannot become positive capacity conclusions
  through this compatibility rule.
- A bounded loopback end-to-end test verifies campaign persistence, identity,
  offline assessment, deterministic replay, and zero replay traffic; it does
  not certify application capacity.
