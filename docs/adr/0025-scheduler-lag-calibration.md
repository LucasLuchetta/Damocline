# ADR 0025: Validate planned-arrival scheduler lag with injected pauses

## Status

Accepted.

## Context

Arrival reports calculate lag from each planned monotonic slot to the scheduler's processing time. Counter conservation alone does not verify that this value responds accurately when the scheduler is delayed.

## Decision

The bounded local calibration runner injects 90, 130, and 210 ms pauses into the scheduler callback, three times per pause, at 20 arrivals/s. It records each expected and observed maximum lag and checks the expected lag (pause minus one 50 ms slot interval) within 50 ms absolute tolerance. Every planned arrival must still reconcile as admitted or dropped. Passing fixture evidence is required when an offline reader considers a calibration report eligible for further coverage checks.

## Consequences

This tests the timing calculation on the current host for selected synthetic delays. It does not prove accuracy under arbitrary OS load, cross-host clock agreement, or generator equivalence to an application workload. A calibration failure blocks a passing fixture report; it does not imply an application bottleneck.
