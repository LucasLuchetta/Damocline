# Roadmap

This is a working plan for an open-source project in **pre-alpha**. The native load-test runner is experimental; there is no stable release yet. Items listed below are not implemented unless checked.

## Foundation

- [x] Core product definition and boundaries.
- [x] Go CLI scaffold with help/version commands and tests.
- [x] Contribution, security, license, and continuous integration files.
- [x] Architecture and metric semantics.
- [ ] Gather design feedback from potential users.

## M0 — Safe HTTP engine

- [x] Explicit authorization flag, validated target and bounded traffic caps, timeout, and run duration (MVP); advanced stop conditions remain open.
- [x] HTTP GET/POST against local fixtures with connection reuse (write methods require opt-in).
- [x] Per-user cookie jars and optional distinct environment-backed identities; full login/CSRF recording is still planned.
- [x] Request-level latency, error categories, and throughput measurement with bounded run caps.
- [x] JSON report with metrics, sanitized paths, and no raw credentials; deeper redaction tests remain open.
- [ ] Generator profiling and controlled reference benchmarking (required before capacity claims).

**Exit:** local safe HTTP tests produce correct, verifiable metrics and refuse unsafe/unbounded configurations.

## M1 — Realistic workload modeling

- [ ] Versioned scenario YAML/JSON schema with strict validation.
- [ ] Authentication references, cookies, variable extraction, and assertions.
- [x] Closed-loop VUs with configurable think times.
- [x] Open-loop arrival scheduling, dropped-start accounting and rate-cap warnings (client generator profiling remains open).
- [ ] Isolated read/write workflows with explicit synthetic test data.

**Exit:** target slowdowns and failures cannot silently make observed delivery diverge from scheduled load.

## M2 — Capacity explorer

- [x] User-defined error/latency budgets and scenario-specific success criteria.
- [x] Bounded step-up, bracket refinement, confirmation runs, and time-budget stop.
- [x] Explorations of named mixed-step journeys; workload mixing by weighted independent user groups remains open.
- [x] Report tested range, repeat results, client cap and telemetry limitations; calibrated uncertainty remains open.
- [ ] Repeatable comparisons between builds and hosting configurations.

**Exit:** reports explain *what* passed, *under which conditions*, and *what remains uncertain*.

## M3 — Optional infrastructure telemetry

- [x] Optional native Linux host CPU/memory collector without a listening port; per-container collection remains open.
- [ ] Integrations with existing metric endpoints and OpenTelemetry.
- [ ] Database, queues, cloud and Kubernetes adapters as demanded.
- [x] Separate Linux host-time correlation reports with cautious diagnostic language.

## M4 — Accessible UX and scaling

- [ ] Local web UI for scenario building and report exploration.
- [ ] Browser journey recording and browser-vs-HTTP result distinctions.
- [ ] Distributed generators and reproducible coordinated execution.
- [ ] Multi-OS release artifacts and compatibility guarantees.

## Explicit non-goals

Unbounded stress testing by default, external scanning without authorization, deterministic predictions for any hardware from one run, or hard-coded "maximum user" claims.
