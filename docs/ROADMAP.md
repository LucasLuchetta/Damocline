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
- [x] Bounded local HTTP reference fixture and deterministic scheduler accounting tests.
- [x] Separate-process loopback calibration for known delay, 4 KiB bodies, 2–100 arrivals/s, a 30-second sustained stage, worker underdelivery, and error recovery; this bounds evidence to the tested host/platform and does not claim the generator's maximum throughput.
- [x] Sample Go heap, goroutines, GC cycles, Go scheduler CPU utilization, and supported Windows/Linux process CPU/RSS during each run; these are generator observations, not target/host capacity limits.
- [x] Reconcile scheduled, started, completed, cancelled, failed, and dropped work; test scheduler stalls, insufficient samples, response-size bounds, and generator process CPU/RSS sampling.
- [x] Establish a bounded reference envelope on Windows and Linux through 100 arrivals/s, repeated trials, a 4 KiB payload, and a 30-second soak; validate generator headroom and keep untested rates/payloads outside the claim.

**Exit:** local safe HTTP tests produce correct, verifiable metrics and refuse unsafe/unbounded configurations. P0 closes for the measured envelope on Windows and Linux. Each capacity campaign still needs a host-local `damocline-calibrate --extended --repeats 2` preflight; results do not establish a universal generator maximum or cover every payload size.

## M1 — Realistic workload modeling

- [x] Versioned, strictly validated JSON scenario schema; YAML is not supported.
- [x] Environment-backed cookie/header references, cookie jars, HTTP response assertions, bounded JSON Pointer/header extraction, and dependent steps within one journey; dynamic login/CSRF recording remains open.
- [x] Closed-loop VUs with configurable think times.
- [x] Open-loop journey arrivals with scheduled/admitted/busy-dropped/late-dropped accounting; model/session representativeness remains open.
- [ ] Isolated read/write workflows with explicit synthetic test data.
- [x] Weighted named workload profiles with deterministic closed-loop assignment, weighted arrival selection, per-profile journey accounting, and explicit configuration limits; production-derived weights and stateful response-driven sessions remain open.

**Exit:** target slowdowns and failures cannot silently make observed delivery diverge from scheduled load.

## M2 — Capacity explorer

- [x] User-defined error/latency budgets and scenario-specific success criteria.
- [x] Bounded step-up, bracket refinement, confirmation runs, and time-budget stop.
- [x] Explorations of named mixed-step journeys; workload mixing by weighted independent user groups remains open.
- [x] Report tested range, repeat results, client cap and telemetry limitations.
- [x] Report a one-sided 95% error-rate bound under stated independent/stationary Bernoulli assumptions; percentile and repeat-to-repeat uncertainty remain open.
- [x] Calculate business demand targets and directly probe exact target VU levels without raising safety ceilings; this does not predict capacity.
- [ ] Sustained capacity campaigns with warm-up, steady windows, soak, spikes, recovery, independent repeats, and time-series pass criteria. Bounded warm-up/observation/recovery repeats and request time-window SLO checks are implemented; soak duration beyond 3m, injected spikes, and a full stability protocol remain open.
- [ ] Statistical uncertainty for latency percentiles and repeat-to-repeat variation, with dependence-aware methods.
- [ ] Repeatable comparisons between builds and hosting configurations.

**Exit:** reports explain *what* passed, *under which conditions*, and *what remains uncertain*. Goal assessment and exact target probing are implemented as short observations; they do not close the certification gate.

## M3 — Optional infrastructure telemetry

- [x] Optional native Linux host CPU/memory collector without a listening port and an explicitly authorized local Docker container CPU/memory snapshot; continuous per-container correlation remains open.
- [ ] Integrations with existing metric endpoints and OpenTelemetry. A bounded explicit collector for Prometheus-text application metric endpoints is implemented; OpenTelemetry remains open.
- [ ] Database, queues, cloud and Kubernetes adapters as demanded.
- [x] Separate Linux host-time correlation reports with cautious diagnostic language.

## M4 — Accessible UX and scaling

- [ ] Local web UI for scenario building and report exploration. A loopback-only read-only report dashboard is implemented; scenario building remains terminal-based.
- [ ] Browser journey recording and browser-vs-HTTP result distinctions.
- [ ] Distributed generators and reproducible coordinated execution.
- [ ] Multi-OS release artifacts and compatibility guarantees. CI compiles the source CLI for Linux amd64, Windows amd64, macOS amd64, and macOS arm64; signed releases and runtime guarantees remain open.

## Explicit non-goals

Unbounded stress testing by default, external scanning without authorization, deterministic predictions for any hardware from one run, or hard-coded "maximum user" claims.
