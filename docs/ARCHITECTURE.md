# Architecture

This document describes the current architecture and planned extensions. The measurement contract and roadmap identify which capabilities are implemented and which remain unvalidated or planned.

## The boundary

Damocline measures observed application behavior under a defined synthetic workload. It will not pretend that an external URL exposes the CPU, memory, database, or autoscaling behavior of its host. Resource correlations are possible only when optional telemetry is connected.

```text
                  Scenario + SLOs
                       |
                Capacity explorer
                 /             \
         Scheduler        Safety controller
             |                  |
     Virtual-user runtime <-----+
             |
       Native Go HTTP engine
             |
      Application under test  ----> Optional infrastructure telemetry
             |                                |
         Responses ---------------------------+
                       |
             Metrics and validation
                       |
           Evidence-based capacity report
```

## Logical components

**Scenario specification.** Versioned target, optional safe workload-revision, environment, and workload-mix provenance labels, request or journey steps, authentication references, think times, workload mode, acceptance criteria, traffic limits, and output options. Scenario files are limited to 1 MiB; parsed workloads are limited to 50 VUs, 100 RPS, three minutes, and 20 total steps. A scenario may define either one legacy journey or two to ten weighted profiles with their own named steps and optional think time. The report records configured weights, self-declared mix basis/reference and observed journey counts. A step can extract a bounded JSON Pointer or response-header value and a later step in the same journey can use it in a path segment, header, or environment-backed body. An opt-in environment-backed synthetic prefix enables a new private `{{synthetic_id}}` for each journey. Dynamic login recording and data lifecycle management remain planned. Mix provenance labels do not prove representativeness.

**Workload scheduler.** The engine supports closed-loop virtual users and open-loop journey arrivals. Schema v16 preserves external cancellation separately from scheduler lateness, and quality requires the dropped categories to reconcile. It tracks scheduled starts, actual starts, late and worker-busy drops, externally cancelled starts not admitted, scheduler lag distribution, and iteration completion. Open-arrival reports identify monotonic planned-slot-to-admission lag and expose sample count, p50, p95, and maximum. This includes timer wake-up and callback admission; its accuracy tolerance is not calibrated. A partly open model with independently arriving sessions and dependent steps within each session is not implemented.

**Virtual-user runtime.** Each actor can maintain its own configured cookie and place in a sequential workflow. Closed-loop VUs keep one deterministic weighted-profile assignment for the run; arrival mode selects a profile per planned journey start. Steps can extract a bounded header or JSON Pointer value for later steps in the same journey. Response assertions are supported. Dynamic login/CSRF recording and a full partly open session model remain planned.

**HTTP execution.** Use Go's networking primitives for HTTP/TLS. Damocline itself owns the load-control logic, sequencing, timing, measurement, failure accounting, and safety guards. Handle keep-alive, redirects, deadlines, and response-body cleanup explicitly. Scenarios select `connection_policy` as `reuse` (the default) or `new_per_request`; the engine applies it to its shared transport and persists it in schema-v14 reports and workload fingerprints. Schema-v15 adds optional independent counters for the calibrator-owned child-process soak; only that exact loopback run can reach `VALID_WITHIN_ENVELOPE` when all persisted dimensions and generator health reconcile. Current preflight evidence covers reuse only, so the other policy remains outside that envelope. Schema-v7 reports include per-step client-observed timing totals for rate wait, DNS, TCP connect, TLS, request write, first response byte, body read and full completion; schema v8 adds structured status and failure counts; schema v10 records generator sampler cadence and collection wall time; schema v11 includes a redacted-by-construction workload fingerprint for offline comparison; schema v12 records per-step complete, interrupted, and oversized body-read counts plus min/max/total bytes for complete bounded bodies; schema v13 adds reconciled per-step and explicit-profile request/journey ledgers in each observation window; schema v14 records the connection policy; schema v15 can persist the controlled-reference counter evidence; schema v16 records cancelled planned arrivals. Body byte counts exclude content and describe bytes returned by the response-body reader, not wire traffic. They do not establish endpoint identity or workload equivalence. Timing totals do not isolate server processing, and phase accuracy is not calibrated under load. The fingerprint excludes resolved credentials and environment-backed body values, so it does not prove runtime data equivalence.

**Capacity explorer.** Run bounded repeated probes against explicit error and latency targets, and optionally attempt an exact business target. Its short experiments report observed passing and failing levels; they do not establish a sustained or statistically certified capacity boundary.

**Metrics pipeline.** Report request and completed-journey distributions, failures, arrival accounting, scheduler lag, sampled Go-runtime plus supported OS process health for the generator, optional request time windows, and a conditional IID-assumption P95 rank interval. Schema-v13 observation windows retain per-step request metrics and completed-journey metrics for explicit profiles, and evaluate their configured criteria independently of aggregate request metrics. Interrupted journeys are counted as cancelled and make an otherwise passing run inconclusive; their partial duration is not presented as a completed journey latency. Schema-v10 reports also record sampler interval, maximum observed sample gap, and wall time spent collecting samples. A run may attach a bounded local calibration artifact; the shared validator checks its preflight schema, stage ledgers and scheduler-lag observations, then stores only sanitized provenance and declared-dimension findings. For ordinary runs, the fixed fixture cannot prove target response shape or endpoint identity, so attached evidence remains inconclusive. The calibrator separately observes its own child-process fixture and can qualify only that controlled 30-second soak. Windows and percentile intervals do not establish steady state or account for correlated samples. Reports also record a request-boundary Little's Law consistency signal when average in-flight sampling, throughput, and mean latency share the configured observation window.

Calibration attachment derives per-step response-size and coarse request-latency guardrail dimensions from the run fields. Neither match establishes response distributions, per-window stability, endpoint identity, or workload equivalence; overall measurement quality remains limited while these are unknown.

**Optional telemetry adapters.** Collect host, Docker, database, or cloud metrics with consent and configured permissions. An absent metric is unknown, not proof of health. Avoid transferring secrets or unrestricted production logs.

**Reporting.** Store scenario/environment identity, constraints, time window, achieved workload, results, variability, and diagnostic limitations. Distinguish direct observations, correlations, and unverified hypotheses.

## Hosting models

- **HTTP-only:** reachable, authorized applications on VPS, dedicated servers, Docker, Kubernetes, PaaS, or serverless.
- **Host/container observability:** optional, permissioned Linux and Docker collectors.
- **Cloud/orchestrator observability:** future cloud provider and Kubernetes adapters.
- **Browser experience:** future browser-based journey execution, independent from protocol-level load.

The application-under-test may comprise multiple services. There is no single physical "server limit" when autoscaling and managed dependencies are involved; the report should identify the tested configuration and first observed limiting condition.

## Safety must precede load generation

The shipped test commands require explicit authorization for traffic and enforce configured duration, concurrency/rate ceilings, request deadlines, and opt-in write operations. Prefer staging and synthetic data; these controls bound tests but do not establish target ownership or guarantee that every workload is safe for every application.

## Development boundaries

Keep a small Go CLI and standard-library-first core. Add deterministic unit/integration tests using local HTTP servers. Only add dashboards, distributed workers, and automatic infrastructure diagnostics after correct single-node scheduling and measurement are demonstrated.

Related: [Measurement model](METRICS.md), [Roadmap](ROADMAP.md), [Go core ADR](adr/0001-go-core.md).
