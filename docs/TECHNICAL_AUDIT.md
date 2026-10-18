# Technical audit: integrated capacity milestones

This audit uses `origin/master` at `56654c9` (merged PR #89) as its baseline. It distinguishes implemented behavior from the validation required for a capacity claim. The local baseline passed `go test ./...` and `go vet ./...` on Windows; `go build ./cmd/damocline` exited successfully, although Go reported a denied write to its module stat cache outside this workspace.

## What the recent merges delivered

PRs #76–#89 extended the bounded G0 acceptance lab, calibrated 30 exact request-shape/rate/connection-policy combinations, recorded effective Linux cgroup CPU entitlement, classified DNS and connection-establishment failures, and added loopback ledgers for drain, interruption, and response-size boundaries. These changes strengthen the local fixture and report accounting. They do not validate arbitrary application endpoints or establish sustainable capacity. `docs/G0_STATUS.md` records the exact fixture and platform limits.

## Implementation versus acceptance

| Area | Current implementation evidence | Remaining product gate |
| --- | --- | --- |
| G0 measurement | `internal/scheduler`, `internal/engine`, `internal/calibration`, `internal/report`, and `internal/acceptancelab` reconcile controlled arrivals, responses, process health, and independent loopback counters. | Ordinary target runs remain `LIMITED`; external endpoint identity, broader generator envelopes, host pressure, and dependence-aware uncertainty are not validated. |
| G1 workload | `internal/config` and `internal/engine` support legacy steps, weighted profiles, deterministic profile selection, shared rate cap, per-actor cookies, response extraction, and synthetic journey IDs. | Only fixed between-step think time is available. Timing/data variation, representative weights, partly open sessions, and synthetic write-data lifecycle remain open. |
| G2 campaign | `internal/campaign` runs bounded warm-up, observation, spike, and recovery phases with repeats; the engine records observation windows. | Stabilization and predeclared long-soak protocols, safe early abort criteria, and representative end-to-end sustained qualification remain open. |
| G3 decision | `internal/statistics`, `internal/capacityplan`, `internal/insights`, and `internal/report` retain conditional IID bounds and cautious offline assessments. | Correlated observations lack dependence-aware uncertainty; ordinary reports cannot qualify a positive capacity decision. |
| G4/G5 diagnosis | Host and optional snapshot adapters, comparison labels, and demand arithmetic exist. | Time-aligned target telemetry, controlled alternatives, and evidence-based cost or bottleneck recommendations remain open. |

The roadmap correctly leaves these gates open, but its suggested next-PR list describes several capabilities already present in code. Completing them requires integrated behavioral evidence and the missing workload semantics, not another copy of the same components.

## Cohesive delivery sequence

1. **Reproducible workload behavior (G1-05/G1-06/G1-07/G1-11):** bounded seeded timing variation, report/fingerprint provenance, actor-isolation and three-profile loopback tests, with cancellation and global-budget checks in the same PR.
2. **Sustained campaign decisions (G2-03/G2-04/G2-06/G2-08/G3-01/G3-02):** stabilization and drift evidence, exact per-window journey SLOs, generator-limited aborts, and controlled stable/degrading fixtures in one PR.
3. **Evidence-grade demand answer (G3-04–G3-08/G3-11/G3-12/G5-08/G5-09):** trial-level uncertainty, workload/measurement/decision evidence kept separate, traceable campaign artifacts, and a single offline demand verdict with correlated-failure counterexamples.
4. **Optional diagnosis (G4-01–G4-04/G4-09/G4-12):** read-only time-aligned adapter contract and bounded Docker/PostgreSQL fixtures, while preserving the HTTP-only unknown-cause path.

Each block should include implementation, local success/failure/cancellation fixtures, reader compatibility where output changes, documentation, and a reproducible validation record. None alone certifies human-user capacity.
