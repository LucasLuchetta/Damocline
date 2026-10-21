# ADR 0126: Reject missing profile and calibration evidence

## Status

Accepted

## Context

Campaign profiles and calibration JSON are decoded into Go structs. Numeric,
boolean, and string fields that are absent then become zero values. Some
validators catch invalid values, but an omitted optional-looking field can be
indistinguishable from an explicitly recorded zero. This weakens replay of
campaign controls and interpretation of generator preflight evidence.

## Decision

Require current campaign profile schema fields that define the scenario,
campaign phase controls, explicit budget, and complete stability policy. Keep
intentional schema-v0 profile migration defaults and omission of runtime-only
consent fields.

Validate top-level calibration artifact fields and required embedded
preflight, generator, and fixture evidence before typed decoding. Require
effective CPU evidence from calibration schema v7 onward, fixture identity
from schema v2, and workload matrix evidence from schema v3. Preserve earlier
schema semantics and permit explicitly encoded zero-valued fields. The
calibrator's `checks` list may be null or empty when no discrepancies exist,
so it is interpreted by its existing length/status contract rather than a
presence check.

## Consequences

- Missing controls cannot silently become a plausible current campaign
  profile through Go zero values.
- Missing calibration evidence fails closed before it can be attached to a
  workload or inspected as a complete artifact.
- Historical profiles retain their explicit migration path, and historical
  calibration records remain governed by the fields their schema recorded.
- This validates artifact structure and declared evidence only; it does not
  authenticate producers or certify application capacity.
