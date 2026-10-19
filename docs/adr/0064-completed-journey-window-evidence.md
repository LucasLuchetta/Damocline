# ADR 0064: Completed-journey observation-window evidence

## Status

Accepted

## Context

Schema-v21 reports preserve request and per-step latency per observation window. They preserve completed journeys per window only for explicit weighted profiles. A legacy scenario has one implicit workflow, so its full journey latency could not be checked by the offline demand planner. Slow full journeys could therefore remain hidden behind healthy individual request metrics.

## Decision

Advance run reports to schema v22 and add a completed-journey metric set to every observation window. Record only journeys that complete during the observation horizon; journeys finishing during drain remain in the run-level ledger but outside observation windows. Validate window metrics and ensure summed window counters do not exceed the run-level journey counters. When profiles are configured, require profile window counters to sum to the aggregate journey window.

The engine evaluates completed-journey latency and error objectives at the run and observation-window levels. A report with requests but no completed journeys is inconclusive; a measured journey SLO breach fails the run. Schema-v22 campaign classification rechecks these fields when an outcome claims `PASS`.

The offline demand planner requires schema v22 and assesses full-run and per-window journey sample counts, error bounds, and conditional IID p95 bounds against the declared objectives, alongside existing request and step checks. Campaign stability screening also requires adequate completed journeys in every observation window and checks the aggregate journey ledger for coarse latency/throughput drift. Historical reports remain readable but cannot support this positive demand or stability check.

## Consequences

Legacy and profile workloads now expose the same full-journey window evidence. Low-sample journey windows lead to `INSUFFICIENT_WINDOWS`, even when HTTP request windows have adequate counts. The report does not claim dependence-aware uncertainty or certify sustainable capacity. Percentile adequacy remains conditional on the existing IID approximation and is not proof that correlated journey samples are independent.
