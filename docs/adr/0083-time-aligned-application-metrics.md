# ADR 0083: Retain bounded application metrics with campaign phases

## Status

Accepted (experimental).

## Context

The `metrics` command could take one local Prometheus-text snapshot, but it was
separate from repeated run phases. Operators had to collect and align external
application observations manually, and a campaign artifact could not retain
which metrics overlapped a particular phase.

## Decision

Add opt-in `--app-metrics-endpoint` and `--app-metrics-interval` to a single
`run`, closed-loop `campaign`, and open-arrival `arrival-campaign`. When enabled,
the collector scrapes concurrently with each bounded run phase and attaches its
own timestamped timeline to the run report. Campaigns share ceilings of 1,000
scrape attempts and 4 MiB of serialized metric samples across all phases. The report records successful samples,
failed collections, interrupted scrapes and whether the campaign-wide ceiling
was reached. Scrapes do not enter the load engine's request counters, `max_rps`
governor or SLO/data-quality verdict.

Keep collection local and bounded: HTTP(S) only, redirects disabled, request
timeout 10 seconds, response body at most 1 MiB, at most 64 distinct aggregated
metric names per scrape and names at most 128 bytes. Discard Prometheus labels.
Reject userinfo, query strings and fragments so credentials are not embedded in
the endpoint or reports; persist only the endpoint origin, not its path. Run
report schema v25 adds the optional timeline.

## Consequences

The snapshots can be aligned manually to their owning run phase. They do not
establish clock accuracy, application health, target identity or a bottleneck
cause, and no metric name is automatically interpreted as request rate, error
rate or duration. Scrapes are additional target traffic and can affect the
application under test. The endpoint has no authentication mechanism in this
version; use an explicitly permitted endpoint that does not require URL
credentials. Partial collection remains visible and never changes the load
phase's PASS/FAIL/INCONCLUSIVE classification.
