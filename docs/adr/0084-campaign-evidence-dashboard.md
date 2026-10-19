# ADR 0084: Visualize saved campaign phases and application metrics locally

## Status

Accepted (experimental).

## Context

Run schema v25 and campaign artifacts retain bounded timestamped application
metrics in the native report for each phase. The local dashboard displayed one
run at a time, leaving users to manually inspect campaign JSON to compare
observation, spike, recovery and soak evidence.

## Decision

Extend `dashboard --file` to recognize validated run, closed-loop campaign,
and open-arrival campaign artifacts. Render trials and phases separately,
preserve their recorded outcome and incomplete checkpoint state, and show the
load unit for each phase. Plot each application metric as individual timestamped
points inside its owning phase. Continue to bind only to loopback addresses and
make no target requests.

The dashboard reads at most 16 MiB and uses the existing run and campaign
validators before rendering. It does not combine different phases into a
single result or modify campaign verdicts.

## Consequences

Users can inspect saved campaign windows and metric observations without
opening raw JSON. HTTP windows use elapsed phase seconds while application
metric points show UTC collection timestamps in a separate phase plot; no
clock-offset correction or common-axis overlay is claimed. Sparse observations
are not connected, metric values are not interpreted as SLOs or causes, and
generator-resource and dropped-start data remain run-wide. The dashboard is a
local read-only view, not a capacity certification interface.
