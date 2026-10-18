# ADR 0050: Preserve sub-microsecond HTTP durations

## Status

Accepted.

## Context

The HTTP trace conversion first truncated `time.Duration` to whole microseconds and then converted to milliseconds. A positive duration shorter than one microsecond became zero, which could erase real client-observed transport failures from phase totals and make a timing reconciliation test intermittent.

## Decision

Convert the monotonic `time.Duration` value directly to fractional milliseconds. Add deterministic tests for one-nanosecond, sub-microsecond, and quarter-millisecond durations. Document that preserving numerical resolution does not calibrate the accuracy of the system clock or HTTP trace callbacks.

## Consequences

Positive short durations remain positive in reports and phase sums. This improves representation fidelity without making claims about scheduler, timer, callback, or hardware accuracy.
