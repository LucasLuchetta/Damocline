# ADR 0019 — Preserve unavailable and invalid generator process readings

## Status

Accepted for a bounded validation slice; G0-07 remains open.

## Context

Generator process samplers return a supported/unavailable flag and counters for CPU time and resident memory. A supported reading with zero resident bytes or a non-finite/negative CPU counter is implausible, but previously could be accumulated as if it were valid. Linux page-to-byte and CPU-time conversions also need overflow guards.

## Decision

Exclude supported process readings that have zero RSS or invalid CPU seconds. Count them separately in the generator summary, preserve the numeric metrics as unknown when no valid sample exists, and make report quality LIMITED when any such sample occurred. Reject overflow in Linux page-size multiplication and CPU-time addition; reject Windows CPU-tick addition overflow and zero working set. A zero sample must not silently become evidence of low resource use.

Tests inject missing, zero, non-finite, stale-time and overflow conditions without relying on machine-specific load thresholds. These checks validate bookkeeping and input plausibility, not sampler accuracy or overhead.

## Safety and limitations

The change affects local generator telemetry only; it does not alter load generation or target traffic. Sampling overhead, OS-load accuracy, filesystem/process-counter semantics on all supported hosts, and stale-but-plausible samples are not fully calibrated. G0-07 remains unchecked.
