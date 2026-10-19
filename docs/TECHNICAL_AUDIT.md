# Technical audit: integrated capacity milestones

This audit uses `origin/master` at `d7828bc` (merged PR #93) as its code baseline and includes the schema-v22 journey-window increment under validation. It separates implemented behavior from the evidence still needed for a capacity claim.

## What the recent merges delivered

PRs #76 through #93 extended the bounded G0 acceptance lab, calibrated 30 exact request-shape/rate/connection-policy combinations, recorded effective Linux cgroup CPU entitlement, classified DNS and connection-establishment failures, and added loopback ledgers for drain, interruption, and response-size boundaries. PR #92 added persisted campaign auditing and named step/profile drift screening; PR #93 removed a false closed-loop arrival signal. These changes strengthen local fixtures and report accounting. They do not validate arbitrary application endpoints or establish sustainable capacity. `docs/G0_STATUS.md` records the fixture and platform limits.

## Implementation versus acceptance

| Area | Current implementation evidence | Remaining product gate |
| --- | --- | --- |
| G0 measurement | `internal/scheduler`, `internal/engine`, `internal/calibration`, `internal/report`, and `internal/acceptancelab` reconcile controlled arrivals, responses, process health, and independent loopback counters. | Ordinary target runs remain `LIMITED`; external endpoint identity, broader generator envelopes, host pressure, and dependence-aware uncertainty are not validated. |
| G1 workload | `internal/config` and `internal/engine` support legacy steps, weighted profiles, deterministic profile selection, shared rate cap, per-actor cookies, response extraction, synthetic journey IDs, and bounded seeded fixed/uniform/discrete between-step pauses. | Representative timing and weights, partly open sessions, and synthetic write-data lifecycle remain open. |
| G2 campaign | `internal/campaign` runs bounded warm-up, observation, spike, and recovery phases with repeats; schema-v2 campaigns reserve bounded drain time, classify stage evidence, and screen request, step, and profile windows for coarse drift. The schema-v22 increment adds full-journey sample adequacy and coarse drift for legacy workflows. | A configurable stabilization protocol, predeclared long soak, broader early abort criteria, and representative end-to-end sustained qualification remain open. |
| G3 decision | `internal/statistics`, `internal/capacityplan`, `internal/insights`, and `internal/report` retain conditional IID bounds and cautious offline assessments; schema v22 adds completed-journey window ledgers and checks journey SLOs per window. | Correlated observations lack dependence-aware uncertainty; p99 objectives, broader sample adequacy, and ordinary-report capacity qualification remain open. |
| G4/G5 diagnosis | Host and optional snapshot adapters, comparison labels, and demand arithmetic exist. | Time-aligned target telemetry, controlled alternatives, and evidence-based cost or bottleneck recommendations remain open. |

The roadmap leaves these gates open where validation is incomplete. Completing them requires integrated behavioral evidence and remaining workload semantics, not duplicating existing components.

## Cohesive delivery sequence

1. **Reproducible workload behavior (G1-05/G1-06/G1-07/G1-11):** bounded seeded timing variation, report/fingerprint provenance, actor-isolation and three-profile loopback tests, with cancellation and global-budget checks in the same PR.
2. **Sustained campaign decisions (G2-03/G2-04/G2-06/G2-08/G3-01/G3-02):** stabilization and drift evidence, journey-level run/window SLO checks, generator-limited aborts, and controlled stable/degrading fixtures in one PR.
3. **Evidence-grade demand answer (G3-04 through G3-08/G3-11/G3-12/G5-08/G5-09):** trial-level uncertainty, workload/measurement/decision evidence kept separate, traceable campaign artifacts, and a single offline demand verdict with correlated-failure counterexamples.
4. **Optional diagnosis (G4-01 through G4-04/G4-09/G4-12):** read-only time-aligned adapter contract and bounded Docker/PostgreSQL fixtures, while preserving the HTTP-only unknown-cause path.

Each block should include implementation, local success/failure/cancellation fixtures, reader compatibility where output changes, documentation, and a reproducible validation record. No single block certifies human-user capacity.
