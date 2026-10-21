# Technical audit: evidence-grade capacity reporting

## Audit scope

The merged baseline is `master` at `439de70`, after PR #145. This audit was prepared while PR #149 was open and distinguishes that candidate from the merged baseline. The exact PR head and CI state change as commits are pushed, so the PR is the authoritative source for current review status; candidate changes below remain unmerged until accepted.

PRs #142–#145 added path-free generator runtime/executable provenance (run schema v31), explicit highest-passing and first-failing tested campaign levels, and a predeclared closed-loop objective contract (campaign schema v13). PR #143 only removed changelog conflict markers. PR #149 remains open against this baseline; PR #150 was closed as superseded by #149. The campaign boundary remains exact-tested-level evidence: it does not infer untested load or establish a universal capacity limit.

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
| G3 decision/report | The PR #149 candidate records declared workload/application/environment identity in closed-loop and open-arrival assessments, pins exact source bytes and auditor identity, and replays JSON/Markdown deterministically. Multi-level aggregation requires matching declared scenario identity. | Dependence-aware uncertainty, source authenticity, representative workload evidence, and ordinary-target capacity qualification remain open. Matching declarations still do not authenticate the deployment or host. |
| G4/G5 diagnosis | Read-only host/snapshot adapters, comparisons, and demand arithmetic exist. | Time-aligned target telemetry, controlled alternatives, and causal root-cause evidence remain open; absent telemetry must stay unknown. |

### PR #149 open candidate and next cohesive validation block

The candidate groups related reliability and validation work in one PR:

- Run schema v32 adds bounded exact order-statistic intervals for p99 alongside p95; the labels retain IID/stationarity assumptions and do not create a p99 objective.
- Campaign auditing binds reports to exact source bytes and auditor identity, rejects duplicate JSON keys and unsupported embedded report schemas, and preserves campaign v2/v4 compatibility without filling absent fields with zero.
- Historical phase labels are checked using their original schema rules. Conflicts with current recomputation remain inconclusive; partial terminal failures have v2/v4 fixtures.
- The open-arrival objective contract is persisted and replayed. The local acceptance matrix grows to manifest v6 with 41 profiles, including exact 20 and 40 starts/s trusted HTTPS profiles.
- Capacity Markdown no longer embeds its local output path; the loopback CLI integration test requires byte-identical JSON and Markdown when replayed to a second destination.
- Individual audits summarize workload/application revisions and declared environment, missing/changing values become UNKNOWN/CHANGED and prevent positive interpretation, and multi-level collections expose the same consistency state before aggregating. Reports render those fields.
- Open-arrival goal assessments record auditor identity and pin the source campaign bytes and exact auditor executable in generated replay commands. Mismatches fail before output writes; the integrated fixture rejects unsupported auditor-pin use on other artifact families.
- A native open-arrival campaign can have passing phase SLOs while remaining below the positive capacity measurement-quality bar. Offline assessment preserves and reports this valid evidence as `INSUFFICIENT_EVIDENCE` instead of rejecting the saved campaign. The bounded integration builds the real CLI executable, executes a loopback campaign in a subprocess, generates assessment JSON/Markdown in separate subprocesses, replays both byte-for-byte without traffic, and verifies mismatched source bytes write no output.
- A schema-v32 fixture with omitted aggregate request counters was accepted and rendered as zero completed requests; removing `target` also produced an assessment. The shared artifact validator now rejects missing `total`, `success`, or `failures` before typed decoding in standalone reports and embedded campaign/exploration runs, requires each token to be a non-negative JSON integer, and requires success plus failures to equal total. For schema versions that define them, step/profile request or journey counters must also reconcile to run/window aggregates; legacy variants without that ledger relationship retain their recorded semantics. Run readers require a valid HTTP(S) origin target across all supported schemas. Coverage spans every supported run schema plus step, workflow, window, campaign phase, and exploration probe ledgers; report, campaign, goal, dashboard, and export entry points use these checks. Explicit zero remains valid. Other versioned fields and non-report artifact families still need equivalent presence checks.

The earlier code head `f0000af` passed the full local Go test, vet, build, formatting, and diff-check gates. GitHub Actions run #392 passed all six jobs on that exact head: Linux race/fuzz/tests/build and two bounded G0 acceptance repeats, Windows package tests/launcher and repeated G0 acceptance, and all four cross-platform builds. Current code head `38c2697` passed `go test ./...`, `go vet ./...`, `go build ./cmd/damocline`, and diff checks locally; the final test-only head `43d2f51` passed focused reader and CLI regression suites. CI run #396521 for that head is in progress. The earlier run #389 had a failed Windows G0 acceptance job; its cause remains unresolved because the accessible job log omitted the failing profile. Run #390 exposed the replay integration's 10-second, 40-starts/s observation as too sensitive to Windows window-level p95 variation: all 400 scheduled requests succeeded, but the stability screen correctly marked one phase `NON_STATIONARY`. The fixture now uses a bounded 30-second observation at 40 starts/s with two completed repeats; the stability gate remained required and passed on Windows in #392. The local end-to-end loopback test validates report behavior only; neither it nor CI acceptance is application-capacity evidence.

## Reliability-first work order

1. Preserve both the passing exact-head Windows/Linux G0 acceptance and the prior failed Windows attempt in the PR record. The earlier failure remains useful evidence of the hosted-runner envelope even though the current exact-head gate passed.
2. Keep the loopback artifact as reporting-pipeline evidence only. For an application-capacity conclusion, Damocline still needs a representative authorized target workload and independently established environment evidence; the available fixtures cannot supply those facts.
3. Continue with the next cohesive reliability gap found by that validation. Keep unrelated capability expansion paused while the report cannot be defended from exact inputs, phase evidence, measurement quality, and stated uncertainty.

The roadmap remains a useful inventory, but code and replayable evidence are the acceptance sources. No single report, passing test, or local calibration run certifies a universal maximum or sustainable capacity outside the tested conditions.
