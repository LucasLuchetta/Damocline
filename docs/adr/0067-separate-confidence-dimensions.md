# ADR 0067: Separate confidence dimensions in offline assessments

## Status

Accepted

## Context

An offline assessment can contain good measurement accounting while the
workload mix is only an owner declaration, and statistical assumptions may
remain unvalidated. One overall confidence label could let a strong generator
result mask an unrepresentative scenario or unsupported decision.

## Decision

Add independent fields for measurement confidence, workload
representativeness, and decision confidence to the combined campaign
assessment. Map measurement quality only to its validated envelope. Mark
profile-mix metadata as `DECLARED_UNVERIFIED` when every required phase has the
same nonempty basis and reference; use `NOT_ASSESSED` when labels are absent
or incomplete and `INCONSISTENT_DECLARATION` when they differ. Keep decision
confidence `NOT_ESTABLISHED` unless the underlying evidence is invalid. None
of these dimensions promotes the assessment state.

## Consequences

Readers can see which evidence dimension is missing without inferring it from
the campaign verdict. Declared workload labels remain self-reported and do
not establish representativeness. The decision confidence remains unknown
while dependence, stationarity, target equivalence, and host identity are
unvalidated. This is an explanatory evidence model, not a calibrated
probability of correctness.
