# Technical audit: evidence-grade capacity reporting

## Audit scope

The merged baseline is `master` at `439de70`, after PR #145. This audit distinguishes that baseline from the current candidate, PR #149. This audit snapshot records the candidate tree at `ac7184b` on 2026-10-21. GitHub reported PR head `41d5b0a`; runs #381 (`fefbbcb`) and #382 (`3fc1706`) cover earlier commits, not this candidate snapshot. Treat candidate changes below as unmerged until the PR head and checks cover the exact commit.

PRs #142–#145 added path-free generator runtime/executable provenance (run schema v31), explicit highest-passing and first-failing tested campaign levels, and a predeclared closed-loop objective contract (campaign schema v13). PR #143 only removed changelog conflict markers. The campaign boundary remains exact-tested-level evidence: it does not infer untested load or establish a universal capacity limit.

## Current state against the product question

Damocline can execute bounded closed-loop VU and open-arrival journey workloads, retain phase/window ledgers, and create offline JSON and Markdown assessments. The report can expose the scenario's declared objectives, observed tested levels, generator environment, data quality, and conditional uncertainty. It cannot yet certify ordinary external-target runs as sustainable capacity.

A defensible report needs to state, for the tested workload:

- the exact scenario and workload unit (virtual users or scheduled journey starts/s), kept distinct from requests/s and in-flight requests;
- the target and declared environment, including generator build/runtime identity and effective CPU entitlement where measured;
- the predeclared latency/error criteria, required journey/step/window coverage, and observation duration;
- observed PASS/FAIL/INCONCLUSIVE results at each tested level, without extrapolating beyond them;
- measurement quality, uncertainty method and assumptions, repeat spread, and missing evidence;
- source artifact identity and an offline replay path that verifies the saved inputs before writing a new assessment.

Observed symptoms must remain separate from root-cause hypotheses. A local reference fixture validates the generator and reporting path; it does not establish application capacity, target identity, workload representativeness, or independent-host behavior.

## Implementation versus acceptance

| Area | Merged baseline evidence | Remaining acceptance gap |
| --- | --- | --- |
| G0 measurement | The scheduler, engine, calibrator, report validator, and acceptance lab reconcile bounded workload starts, client outcomes, process health, and independent loopback ledgers. Master acceptance manifest v5 covers 39 exact profiles and ten transport cases; CI and local evidence are recorded in [G0 status](G0_STATUS.md). | Ordinary target runs remain LIMITED. Loopback acceptance does not prove target identity, physical-host independence, bare-metal Linux, broad generator headroom, or behavior under host pressure. |
| G1 workload | Scenarios support weighted workflows, per-actor cookies, optional per-workflow cookie scopes, dependent response extraction, bounded synthetic identifiers, cleanup suffixes, and seeded between-step timing. | Declared mix and cookie separation do not prove representative user behavior, account identity, or session realism. A partly open session model and source-validated workload mix remain unimplemented. These are not the immediate report-reliability gate. |
| G2 campaign | Master closed-loop campaigns persist and replay declared journey objectives, delivery requirements, window coverage, and bounded stability policy. Campaigns checkpoint completed phases and preserve incomplete trials conservatively. | Longer representative observations, independent environments, application data reset, and authenticated-session independence remain open. A passing short campaign is still only an observation under its declared conditions. |
| G3 decision/report | Master run schema v31 records path-free generator identity; campaign collections report observed tested-level boundaries; offline assessments recompute phase evidence and render executive/technical Markdown from the same assessment. | Dependence-aware uncertainty, source authenticity, representative workload evidence, and ordinary-target capacity qualification remain open. The report must not turn conditional IID intervals or repeat spread into a capacity-confidence claim. |
| G4/G5 diagnosis | Read-only host/snapshot adapters, comparisons, and demand arithmetic exist. | Time-aligned target telemetry, controlled alternatives, and causal root-cause evidence remain open; absent telemetry must stay unknown. |

### PR #149 candidate

The candidate groups related reliability and validation work in one PR:

- Run schema v32 adds bounded exact order-statistic intervals for p99 alongside p95; the labels retain IID/stationarity assumptions and do not create a p99 objective.
- Campaign auditing binds reports to exact source bytes and auditor identity, rejects duplicate JSON keys and unsupported embedded report schemas, and preserves campaign v2/v4 compatibility without filling absent fields with zero.
- Historical phase labels are checked using their original schema rules. Conflicts with current recomputation remain inconclusive; partial terminal failures have v2/v4 fixtures.
- The open-arrival objective contract is persisted and replayed. The local acceptance matrix grows to manifest v6 with 41 profiles, including exact 20 and 40 starts/s trusted HTTPS profiles.
- Capacity Markdown no longer embeds its local output path; the loopback CLI integration test requires byte-identical JSON and Markdown when replayed to a second destination.

Local full Go tests, vet, build, formatting, and diff checks passed on the candidate snapshot. CI results were attached to earlier commits and therefore do not establish validation for `ac7184b`.

## Reliability-first work order

1. Close PR #149 validation on the exact head SHA. Resolve the branch/PR-head mismatch, wait for the platform acceptance jobs, and report only checks attached to the final commit as final-head CI evidence.
2. Reproduce one bounded report end to end using existing campaign and audit commands: run a synthetic loopback scenario, retain its exact campaign artifact, generate JSON plus executive/technical Markdown, and repeat the audit with the same source and auditor. The CLI integration test now compares both outputs byte for byte; changed source bytes or a different auditor must fail before output is written. Label this as reporting-pipeline validation, not application-capacity evidence.
3. Pause unrelated capability work. Continue only when validation identifies a concrete gap that prevents an engineer from reproducing or defending the report. Any report presented as application capacity still needs a representative authorized target workload and environment; the repository's loopback fixtures cannot supply that evidence.

The roadmap remains a useful inventory, but code and replayable evidence are the acceptance sources. No single report, passing test, or local calibration run certifies a universal maximum or sustainable capacity outside the tested conditions.
