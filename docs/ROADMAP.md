# Roadmap

This is a working plan for an open-source project in **pre-alpha**. There is no released load-test runner yet. Items listed below are not implemented unless checked.

## Foundation

- [x] Core product definition and boundaries.
- [x] Go CLI scaffold with help/version commands and tests.
- [x] Contribution, security, license, and continuous integration files.
- [x] Architecture and metric semantics.
- [ ] Gather design feedback from potential users.

## M0 — Safe HTTP engine

- [ ] Explicit authorized targets and safeguards (traffic caps, timeouts, duration, stop conditions).
- [ ] HTTP GET/POST against local fixtures with connection reuse.
- [ ] Independent user session state and deterministic accounting.
- [ ] Latency, error, and throughput measurement with bounded memory.
- [ ] Machine-readable report format and sensitive-field redaction.
- [ ] Generator profiling and controlled reference benchmarking.

**Exit:** local safe HTTP tests produce correct, verifiable metrics and refuse unsafe/unbounded configurations.

## M1 — Realistic workload modeling

- [ ] Versioned scenario YAML/JSON schema with strict validation.
- [ ] Authentication references, cookies, variable extraction, and assertions.
- [ ] Closed-loop VUs with configurable think times.
- [ ] Open-loop arrivals, dropped-start accounting, and generator-health warnings.
- [ ] Isolated read/write workflows with explicit synthetic test data.

**Exit:** target slowdowns and failures cannot silently make observed delivery diverge from scheduled load.

## M2 — Capacity explorer

- [ ] User-defined error/latency budgets and scenario-specific success criteria.
- [ ] Bounded step-up, bracket refinement, confirmation runs, and safety abort.
- [ ] Per-workflow capacity plus realistic mixed-workload studies.
- [ ] Report variability, valid load range, and test limitations.
- [ ] Repeatable comparisons between builds and hosting configurations.

**Exit:** reports explain *what* passed, *under which conditions*, and *what remains uncertain*.

## M3 — Optional infrastructure telemetry

- [ ] Linux/Docker resource collectors with minimal permissions.
- [ ] Integrations with existing metric endpoints and OpenTelemetry.
- [ ] Database, queues, cloud and Kubernetes adapters as demanded.
- [ ] Time-correlated evidence with cautious diagnostic language.

## M4 — Accessible UX and scaling

- [ ] Local web UI for scenario building and report exploration.
- [ ] Browser journey recording and browser-vs-HTTP result distinctions.
- [ ] Distributed generators and reproducible coordinated execution.
- [ ] Multi-OS release artifacts and compatibility guarantees.

## Explicit non-goals

Unbounded stress testing by default, external scanning without authorization, deterministic predictions for any hardware from one run, or hard-coded "maximum user" claims.
