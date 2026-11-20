# ADR 0148: Defer record signing, host attestation and default gate enforcement past G0

## Status

Accepted.

## Context

The G0 exit test requires repeatable calibration on the actual generator
class, a workload inside its validated local envelope, reconciled
scheduling, cancellation, timing and counts, and process health that is
present or explicitly unavailable. It does not require proof that
evidence is authentic against deliberate editing.

Three items were still listed as open in the G0 matrix:

- G0-01: calibration records are not signed.
- G0-08: there is no physical host attestation and no empirically
  validated calibration lifetime.
- G0-02 and G0-08: the calibration gates stay opt-in instead of being
  enforced by default.

Each of these is its own feature with its own threat model or UX
decision. None of them changes what a measurement means or how it is
reconciled.

## Decision

Record signing, physical host attestation, validated calibration
lifetime and default gate enforcement are moved to a later milestone.
They are no longer G0 exit blockers.

Until then:

- Reports and calibration records are not authenticated. Damocline must
  not describe them as tamper-evident.
- Cgroup or runtime CPU capacity is runtime configuration, not machine
  identity.
- The existing inclusive 24-hour age limit stays an operational bound,
  not a validated lifetime.
- Ordinary target runs stay LIMITED unless an opt-in exact gate passes.
- A skipped, failed, stale or non-equivalent preflight must never be
  described as a scientifically validated capacity result.

## Consequences

G0 can close on measurement correctness evidence alone. The deferred
items stay listed as known limitations in `docs/G0_STATUS.md` and must
be scheduled explicitly in a later milestone.
