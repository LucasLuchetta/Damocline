# ADR 0048: Gate positive measurement quality on scheduler lag

## Status

Accepted.

## Context

The calibrator's short healthy stages screened scheduler lag, but the 30-second controlled soak and report-side positive-state recomputation only required complete arrival accounting. A report could therefore claim `VALID_WITHIN_ENVELOPE` without proving acceptable scheduler timing for the soak itself.

## Decision

Require one monotonic due-slot dispatch-lag sample for each of the 300 planned arrivals in the controlled 10-arrival/s soak. Use the engine's canonical method identifier, distinct from the injected-pause calibration method. Apply the same fixture-specific screening limits in calibration and report recomputation: p95 at or below 50 ms and maximum below 100 ms. Treat these as local-fixture qualification bounds, not general scheduler-accuracy guarantees.

## Consequences

Missing, mismatched, or out-of-bound scheduler evidence prevents the controlled positive quality state and causes calibration validation to fail with the observed sample and lag values. Existing `LIMITED` and `INVALID` meanings remain independent of application SLO outcome. OS-load, timer-jitter, and hardware-class validation remain open.
