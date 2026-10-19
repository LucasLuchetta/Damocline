# ADR 0077: Enforce the observation admission boundary

## Status

Accepted.

## Context

Open-arrival scheduling computed planned slots from monotonic elapsed time, but a timer delivered just after the observation duration could still invoke the admission callback when the slot's lag was shorter than one interval. Closed-loop actors likewise relied on context timer delivery to stop their next journey. This made timer scheduling jitter capable of extending offered work beyond the configured observation boundary.

## Decision

Treat the configured duration from the run's monotonic start as the admission cutoff. The open-arrival scheduler classifies any due slot accounted at or after that cutoff as late and does not invoke the admission callback. Closed-loop actors check the same monotonic deadline before starting another journey. Work admitted before the boundary continues under the existing bounded drain context; incomplete work remains counted by the existing cancellation and journey ledgers.

## Consequences

The boundary is deterministic at the engine's admission decision, and open-arrival counts remain reconciled when a timer wakes late. Timer/OS scheduling accuracy under load still requires broader calibration. This does not claim that the operating system can be prevented from preempting execution between the admission decision and the first network syscall.
