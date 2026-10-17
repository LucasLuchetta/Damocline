# ADR 0026: Enforce bounded scenario and report artifact sizes

## Status

Accepted.

## Context

The scenario loader used a 1 MiB limited reader but could accept an object that fit within the limit followed by more than 1 MiB of whitespace. Report readers already reject artifacts above 16 MiB, while the writer had no matching cap.

## Decision

Read at most 1 MiB plus one byte and reject scenario files whose total size exceeds 1 MiB. Reject JSON report artifacts whose encoded size exceeds 16 MiB before creating the destination directory or replacing a file. Keep existing workload, response-body, campaign, and time-series bounds in force.

## Consequences

The loader and writer now have explicit, tested byte boundaries and produce clear failures. This is a partial G0-09 measure: it does not measure peak process memory, prove leak freedom, or replace stress/fuzz and interruption testing. The output cap matches existing 16 MiB report readers.
