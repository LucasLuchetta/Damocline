# Architecture

> This is the **proposed architecture**, not a description of currently implemented load testing.

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

**Scenario specification.** Versioned target, request or journey steps, authentication references, think times, workload mix, acceptance criteria, traffic limits, and output options.

**Workload scheduler.** Two execution models: closed-loop virtual users and open-loop arrivals. Track scheduled starts, actual starts, dropped starts, iteration completion, and concurrent requests. A slow target must not silently reduce load in an open-loop test without the report explaining that shortfall.

**Virtual-user runtime.** Each actor maintains its own cookies, values extracted from responses, and place in the workflow. Sessions, steps, and scenario failures must be isolated and measurable.

**HTTP execution.** Use Go's networking primitives for HTTP/TLS. Damocline itself owns the load-control logic, sequencing, timing, measurement, failure accounting, and safety guards. Handle keep-alive, redirects, deadlines, and response-body cleanup explicitly.

**Capacity explorer.** Run bounded, reproducible experiments to find the highest *validated passing* level against explicit error and latency targets. Refine brackets, confirm candidates across runs, and stop on safety limits or generator saturation.

**Metrics pipeline.** Separately report connection, protocol, status-code, assertion, and journey failures; distributions for requests and whole workflows; offered vs achieved traffic; generator telemetry; time windows, including warm-up and cool-down.

**Optional telemetry adapters.** Collect host, Docker, database, or cloud metrics with consent and configured permissions. An absent metric is unknown, not proof of health. Avoid transferring secrets or unrestricted production logs.

**Reporting.** Store scenario/environment identity, constraints, time window, achieved workload, results, variability, and diagnostic limitations. Distinguish direct observations, correlations, and unverified hypotheses.

## Hosting models

- **HTTP-only:** reachable, authorized applications on VPS, dedicated servers, Docker, Kubernetes, PaaS, or serverless.
- **Host/container observability:** optional, permissioned Linux and Docker collectors.
- **Cloud/orchestrator observability:** future cloud provider and Kubernetes adapters.
- **Browser experience:** future browser-based journey execution, independent from protocol-level load.

The application-under-test may comprise multiple services. There is no single physical "server limit" when autoscaling and managed dependencies are involved; the report should identify the tested configuration and first observed limiting condition.

## Safety must precede load generation

Before an actual test command ships, require explicit targets, strict duration/concurrency/rate ceilings, deadlines, stop conditions, redaction, and opt-in write operations. Default to test environments and synthetic data.

## Development boundaries

Keep a small Go CLI and standard-library-first core. Add deterministic unit/integration tests using local HTTP servers. Only add dashboards, distributed workers, and automatic infrastructure diagnostics after correct single-node scheduling and measurement are demonstrated.

Related: [Measurement model](METRICS.md), [Roadmap](ROADMAP.md), [Go core ADR](adr/0001-go-core.md).
