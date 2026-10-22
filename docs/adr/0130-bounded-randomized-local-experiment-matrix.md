# ADR 0130: Bounded randomized local experiment matrix

- Status: Accepted
- Date: 2026-10-22

## Context

The WordPress fixture validates an open-source application workflow and its
telemetry, but provisioning and generator calibration make it slow to use for
many parameter variations. The synthetic lab target is fast and exposes
bounded delay, concurrency-slot, and queue parameters. Repeated experiments
also need fresh target state and a recorded execution order.

Running several target/generator pairs on one machine improves experiment
throughput, but those workers contend for the same CPU, memory, network, and
scheduler. Their measurements cannot be treated as independent-host capacity
evidence.

## Decision

Add a local PowerShell matrix runner for the synthetic target. It randomizes
bounded variants with a recorded seed, starts a fresh target process on a
unique loopback port for every replicate, runs campaign and offline audit, and
records outcomes, configuration, order, and configured concurrency in a
private manifest. Parallelism is configurable and capped at eight workers.

The runner is an exploratory and integration-test tool. It labels the
concurrency condition in each scenario and reports that shared-host contention
limits interpretation. Capacity-confirmation campaigns must use controlled
resource conditions or independent hosts, representative workloads, and a
qualified generator envelope.

## Consequences

- Parameter sweeps can be run and repeated more quickly than rebuilding the
  application fixture for every variation.
- Every replicate has isolated target state, its own port, and saved campaign
  and audit artifacts.
- Shared-host parallel runs remain limited evidence. Recording concurrency
  makes them reproducible but does not remove worker interference.
- The synthetic target does not establish capacity for WordPress or a business
  application.
