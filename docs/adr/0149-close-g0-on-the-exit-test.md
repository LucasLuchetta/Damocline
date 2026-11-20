# ADR 0149: Close G0 on the exit test and carry per-gate gaps forward

## Status

Accepted.

## Context

`docs/ROADMAP.md` defines the G0 exit test:

1. Calibration is repeatable on the actual generator class.
2. The proposed workload lies within its validated local envelope.
3. Scheduling, cancellation, timing and counts reconcile.
4. Process health is present or explicitly unavailable.

Several G0-01–G0-17 rows also carry an open-ended "verify remaining"
list, such as broader matrices, more hardware classes, real full-disk
failure or an independent clock. These lists describe further hardening.
Fixture work alone cannot exhaust them.

## Decision

G0 closes when the exit test holds within the local reference scope. It
does not wait for every row's "verify remaining" list to be empty.

Each row keeps its documented remaining gaps in `docs/G0_STATUS.md`.
These gaps move to post-G0 hardening and must not be described as
verified. ADR 0148 also defers four items past G0: record signing,
physical host attestation, validated calibration lifetime and default
gate enforcement.

## Evidence

The exit test is backed by the following evidence, all within the local
reference scope:

- **Repeated calibration on independent hardware:** repeated two-run
  acceptance on an independent bare-metal Linux x86-64 host passed. All
  rules passed, effective CPU capacity was detected and every controlled
  soak was `VALID_WITHIN_ENVELOPE`.
- **Hosted CI:** hosted Windows and Linux CI acceptance also passed.
- **Reconciliation:** deterministic loopback fixtures reconcile
  scheduling, cancellation, timing and counts against independent server
  ledgers. This includes bounded in-process CPU pressure, where
  over-tolerance runs are classified as degraded rather than valid.
- **Process health:** generator process health is recorded, or marked
  explicitly unavailable.
- **Gating:** opt-in exact calibration gates stop traffic before a run
  when evidence is stale or not equivalent.

## Consequences

- Passing G0 is not a sustainable application capacity result. It
  qualifies only the generator's local reference envelope.
- A skipped, failed, stale or non-equivalent preflight must never be
  described as a scientifically validated capacity result.
- Ordinary target reports remain LIMITED unless an opt-in exact gate
  passes.
