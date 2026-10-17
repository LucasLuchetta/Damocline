# ADR 0022 — Separate request, journey and think-time observations

## Status

Accepted (experimental); G0-13 remains open until timing accuracy and broader journey abort cases are calibrated.

## Context

Per-step HTTP timing and completed journey elapsed time answer different questions. A journey includes the rate governor waits between its requests and configured between-step think-time sleeps. Without a separate observation of the sleeps, a slower journey could be misread as a slower HTTP request. An interrupted journey also has no completed elapsed-time sample.

## Decision

Continue recording HTTP request latency around the client request and bounded response read, and journey latency from journey start through its last completed step. Add per-run and per-profile totals and counts for actual wall time spent in configured between-step sleeps. Schema v8 includes these observations. If a sleep is interrupted, record the elapsed partial interval while keeping the journey in the cancelled count. Never add cancelled partial journey duration to completed journey percentile samples.

## Consequences and limitations

- Observed think time covers only configured between-step sleeps; it does not model idle time outside journeys or user behavior beyond the scenario.
- Journey elapsed time includes configured rate-token waits, request durations and think-time sleeps. Per-step request latency excludes the rate-token wait.
- Sleep duration is measured on the local monotonic clock but timing accuracy is not calibrated under host load.
- Tests use bounded loopback handlers, a fixed known sleep and cancellation of a multi-step journey; variable think-time distributions and full cross-platform timing tolerances remain open.
