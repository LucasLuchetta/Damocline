# ADR 0080: Persist campaign phase checkpoints

## Status

Accepted.

## Context

Campaign commands required an output path, but wrote the artifact only after
the entire campaign returned. A forced process exit could therefore discard
previously completed phase reports. Atomic report replacement already provides
a safe persistence mechanism for complete JSON artifacts.

## Decision

Closed-loop campaign schema v10 and open-arrival campaign schema v6 write a
checkpoint after each completed phase. A checkpoint is marked `RUNNING`, keeps
completed trials intact, and stores the current trial with outcome
`IN_PROGRESS`. A checkpoint callback is synchronous; failure to persist stops
the campaign before another phase begins. Final outcomes are written as
`COMPLETED` artifacts. Existing schemas remain readable.

Offline audits recognize a valid `RUNNING` checkpoint, validate its completed
phase prefix and pending trial, and report it as inconclusive. They do not
pretend the campaign or pending trial has a final outcome.

## Consequences

Completed phase evidence survives termination between phases. The currently
executing phase cannot be checkpointed until its runner returns its report, so
termination during that phase leaves only earlier phase evidence. A final
checkpoint write failure is returned as an error and stops further execution.
Checkpoint writes use the existing bounded, atomic report writer.
