# ADR 0129: Validate open-arrival campaign artifact presence

## Status

Accepted

## Context

Open-arrival campaign JSON is decoded into Go structs by offline goal assessment,
the local dashboard, and redacted export. An absent string, number, boolean, or
slice can become the same Go zero value as a deliberately recorded value. The
semantic assessor catches many invalid combinations, but each reader should
reject missing schema-defined evidence before typed decoding and use the same
structural rules.

## Decision

Add a bounded raw-byte validator for open-arrival campaign artifacts. Require
the common root evidence and nested trial, phase, and embedded run schema
markers. Gate later fields by their introduction schema: cooldown from schema
v5, execution state from v6, and predeclared objectives and stability policy
from v8. Validate completed and pending trials. Require trial phase fields to
be present. Current checkpoint schemas v6+ require phase arrays, while
historical schemas v1-v5 retain the explicit `phases: null` representation for
an aborted trial that completed no phases. Empty arrays remain valid wherever
the contract records an array.

Use the validator before typed decoding in `goal`, dashboard, and redacted
export. Keep semantic campaign verification in the existing capacity assessor.
Preserve historical schema behavior and explicitly encoded zero values.
`campaign profile verify` already loads its source through the validated
profile decoder; assessment JSON is not parsed to recompute a verdict, though `report save` can archive JSON opaquely
by any CLI command.

## Consequences

All current open-arrival campaign readers share one structural contract and do
not interpret missing evidence as zero. Tests cover current and historical
schema markers, explicit zeros, duplicate keys, empty and null arrays, nested
trial/phase/run omissions, and rejection before assessment/export outputs are
created. This validates structure and declared evidence only; it does not
authenticate a producer or establish application capacity.
