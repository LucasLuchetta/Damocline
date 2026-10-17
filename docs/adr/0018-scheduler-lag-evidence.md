# ADR 0018 — Report scheduler lag distribution with its measurement boundary

## Status

Accepted for an evidence-reporting slice; G0-06 remains open.

## Context

Arrival-mode scheduling already measured planned-slot lag and reported p95 and maximum. It did not expose sample coverage, median lag, or an explicit measurement boundary. Readers could mistake the duration representation or observed lag for a validated clock-accuracy guarantee.

## Decision

For every planned open-arrival slot, record monotonic planned-slot-to-admission delay. Report the sample count and empirical p50, p95, and maximum alongside a fixed method description. The report and documentation state that this includes timer wake-up and callback admission, does not isolate operating-system timer behavior, and has no validated accuracy tolerance yet. Scheduler lag remains diagnostic and cannot imply application latency or capacity.

Use nearest-rank empirical quantiles: sort observed durations and select the smallest rank whose cumulative share reaches the requested percentile. The p50 and p95 summaries are descriptive statistics over this run's planned arrivals, not confidence intervals.

## Safety and limitations

No traffic behavior, workload ceilings, or approval logic changes. The metadata contains only durations and counts. Injected stall tests verify that delayed scheduling is visible, while OS-load calibration, timer-vs-callback separation, and quantitative error tolerance remain future work.
