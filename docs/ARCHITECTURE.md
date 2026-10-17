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

**Scenario specification.** Versioned target, request or journey steps, authentication references, think times, workload mode, acceptance criteria, traffic limits, and output options. Weighted profiles and response-driven extraction are planned, not currently implemented.

**Workload scheduler.** The engine supports closed-loop virtual users and open-loop journey arrivals. It tracks scheduled starts, actual starts, late and worker-busy drops, scheduler lag, and iteration completion. A partly open model with independently arriving sessions and dependent steps within each session is not implemented.

**Virtual-user runtime.** Each actor can maintain its own configured cookie and place in the sequential workflow. Response assertions are supported. Response-driven value extraction, dependent request construction, and dynamic multi-step sessions remain planned.

**HTTP execution.** Use Go's networking primitives for HTTP/TLS. Damocline itself owns the load-control logic, sequencing, timing, measurement, failure accounting, and safety guards. Handle keep-alive, redirects, deadlines, and response-body cleanup explicitly.

**Capacity explorer.** Run bounded repeated probes against explicit error and latency targets, and optionally attempt an exact business target. Its short experiments report observed passing and failing levels; they do not establish a sustained or statistically certified capacity boundary.

**Metrics pipeline.** Report request and completed-journey distributions, failures, arrival accounting, scheduler lag, and sampled Go-runtime plus supported OS process health for the generator. Interrupted journeys are counted as cancelled and make an otherwise passing run inconclusive; their partial duration is not presented as a completed journey latency. The current report is aggregate: it does not provide aligned steady-state time series or percentile confidence intervals.

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
