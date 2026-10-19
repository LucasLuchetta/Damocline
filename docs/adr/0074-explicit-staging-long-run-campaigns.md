# ADR 0074: Explicit staging long-run campaigns

## Status

Accepted (experimental).

## Context

Closed-loop campaigns and open-arrival campaigns were limited to a 15-minute
wall-time budget, with phase limits no longer than 10 minutes for observation
and 3 minutes for soak. Some staging investigations need longer windows to
observe gradual behavior, but removing those limits silently would make a
campaign much easier to run for an unexpectedly long time.

## Decision

Keep existing limits as the default. A caller may opt into longer phases only
when the scenario declares `environment: staging` and the command includes
`--allow-long-run --staging-confirmed --authorized`. Long-run mode also requires
an explicit `--budget` no greater than two hours and `--out` so phase evidence
is retained. Observation and soak phases are capped at 30 minutes; warm-up,
spike and recovery retain their existing 3-minute limits. Each report is
limited to 120 observation windows. Caller cancellation and the explicit
campaign budget remain the stop controls.

Closed-loop campaign schema v6 and open-arrival campaign schema v2 persist the
long-run declaration and user-declared environment. Offline readers validate
the same phase and total-time bounds. The declaration is not proof of
deployment topology, and no resource-based automatic abort is implied.

## Consequences

Operators must choose long-run mode, identify the scenario as staging, set a
hard wall-time budget and save the full artifact. Ordinary 3-minute runs,
15-minute campaign limits and bounded exploration remain unchanged. The mode
allows a larger observation window; it does not provide application-side
resource telemetry, prove stationarity, qualify a generator for an external
target, or certify sustainable capacity. Staging tests still require live
operator oversight and an independent abort plan.
