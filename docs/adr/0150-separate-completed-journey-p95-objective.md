# ADR 0150: Separate completed-journey p95 objective

## Status

Accepted.

## Context

Run-level SLO checks compared completed-journey p95 with `p95_limit_ms`,
the same limit used for individual HTTP requests. A completed journey
includes configured think time. A scenario with five steps and 2 s think
time has journeys of at least 8 s. With a request-sized limit, `run` and
`explore` can therefore never pass a human-like workload. Campaign
objective contracts already record a `journey_p95_limit_ms`, but they
copied it from `p95_limit_ms`.

## Decision

- Add an optional scenario field `journey_p95_limit_ms` in (0,120000] ms.
- When it is set, every completed-journey latency check uses it instead of
  `p95_limit_ms`: aggregate journeys, per-window journeys and per-profile
  window journeys in the engine. The same applies offline to campaign
  evidence classification, objective contracts, goal assessment, demand
  planning blockers, audit workflow breaches and sealed-protocol matching.
- Journey error-rate checks are unchanged. HTTP request, step and
  request-window checks keep `p95_limit_ms`.
- When it is absent, behavior is identical to earlier versions. Runs that
  configure nonzero think time then carry a warning, and `insights` shows a
  caution, suggesting `journey_p95_limit_ms`.
- Run report schema v35 records the field in `config` with `omitempty`.
  Readers reject it before v35 or out of range. Offline readers use
  `RunConfig.JourneyLatencyLimitMS()`, which falls back to `p95_limit_ms`.
- Assessors that required exactly the current schema now accept v34 and
  v35, because v35 changes nothing for reports without the field. The
  campaign generator-capacity gate is pinned to v34 and later instead of
  following the current schema.

## Consequences

- A schema bump is required. An older reader that ignored the field
  would recompute a different verdict from the same saved report.
- A sealed capacity protocol's `max_p95_ms` describes complete journeys.
  It now matches the effective journey limit. The request `p95_limit_ms`
  is not sealed separately when a journey limit is set.
- The limit is a declared objective. It does not validate that the think
  time or the journey limit represent real users.
