# ADR 0046: Separate externally cancelled arrivals from scheduler lateness

## Status

Accepted.

## Context

When open-loop execution was externally cancelled before its configured schedule ended, the scheduler counted every remaining planned slot as late. That made intentional run termination indistinguishable from timer/scheduler failure and weakened generator diagnostics.

## Decision

Classify due arrivals that are not admitted because the run context was externally cancelled before the schedule deadline as `CancelledBeforeAdmission`. Keep natural-deadline slots whose dispatch misses the scheduler tolerance in `DroppedLate`, and worker shortages in `DroppedBusy`. Schema v16 persists this category as a pointer-backed field so an observed zero differs from historical absence. Recalculate current report quality by reconciling the three drop categories with scheduled minus started arrivals; preserve the v1-v15 interpretation.

## Consequences

Reports and insights can distinguish run-control interruption from generator lateness. Any cancelled arrival makes delivery incomplete and therefore prevents positive quality. Historical reports remain readable, but their cancellation category stays unknown. This distinction does not attribute why a run was cancelled or validate scheduler accuracy under operating-system load.
