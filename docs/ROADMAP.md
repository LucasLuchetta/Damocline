# Roadmap — evidence-based application capacity

**Project stage:** pre-alpha; no stable release or universal user-capacity certification.  
**Scope of this roadmap:** Damocline's **native, local-first Go HTTP capacity engine**, including workload models, capacity exploration, per-workflow analysis, optional infrastructure observability, reports, and an accessible CLI. Browser recording and distributed execution are **post-core extensions**, not prerequisites for the first trustworthy product.

**Product question:** *Given a specified workload, environment, business demand, observation horizon and performance/error objectives, what load was demonstrably supported, how certain is that finding, what constrained it, and does the evidence warrant optimization or additional infrastructure?*

Read [measurement definitions](METRICS.md), [scientific foundations and limitations](SCIENTIFIC_FOUNDATIONS.md), [local reference-validation protocol](REFERENCE_VALIDATION.md), and [architecture](ARCHITECTURE.md) before implementing a milestone.

## How to read and maintain this plan

- `[x]` = implemented in `master` and covered by existing tests **only within its documented scope**. It does not mean scientifically certified for arbitrary hardware/workloads.
- `[ ]` = not implemented, not sufficiently tested, or not yet accepted. An idea documented elsewhere does not count as completed.
- **P0:** incorrect measurements, unsafe behavior, or a misleading capacity decision; **P1:** required for the primary HTTP capacity product; **P2:** stronger diagnostic, forecasting and usability capabilities; **P3:** optional advanced integrations.
- Each `ID` is a future, independent issue/PR-sized deliverable. IDs are identifiers **inside this roadmap**, not existing GitHub issue or PR numbers.
- **Verification** following each task is a minimum acceptance condition. A checkbox is marked only after code, automated tests, evidence and limitations are in the merged PR.
- Tasks may be split further during implementation. Prioritize a thin vertical slice over adding unconnected framework layers. Do not lift safety ceilings or make production-grade claims because a checkbox was checked.
- English is the repository language; never commit actual customer hostnames, production IPs, tenant IDs, paths, credentials, private reports or recorded session secrets.

## 0. Current baseline: already delivered, with limits

These are **capabilities**, not a declaration of end-to-end production readiness.

### B0 — Core and calibration

- [x] **B0-01:** Native Go HTTP runner; GET/HEAD and explicitly authorized writes; bounded rate, VUs, timeout and duration. Verified by existing engine/config tests; currently up to 50 VUs, 100 request starts/s and 3 minutes for an individual run.
- [x] **B0-02:** Closed-loop actors, configurable think times and open-loop journey starts, with explicitly accounted offered/admitted/busy-dropped/late-dropped arrivals.
- [x] **B0-03:** Bounded graceful-drain logic and incomplete request/journey accounting; cancelled journeys cannot silently produce PASS.
- [x] **B0-04:** Independent cookie jars with optional environment-backed per-VU cookies, named HTTP steps and response/status assertions. Real-browser rendering remains out of scope; bounded response-derived values and dependent HTTP steps are tracked in B2 below.
- [x] **B0-05:** Separate-process loopback reference check with known delays/errors, 4 KiB bodies, a 30-second soak and extended 2–100 arrivals/s stages, plus scheduler-stall, overload and recovery checks.
- [x] **B0-06:** Go runtime and Windows/Linux process CPU/memory sampling for the **generator**, not the application server; collected alongside run metrics.
- [x] **B0-07:** CI test, race/vet/build checks and extended bounded loopback calibration on Linux and Windows. See [reference validation](REFERENCE_VALIDATION.md) for its actual tolerances and limitations.

### B1 — Existing exploration, reporting and CLI

- [x] **B1-01:** Short, bounded VU exploration with step-up, refinement after failure and repeated stage observations; highest observed passing / first observed failing levels are not sustained-capacity certificates.
- [x] **B1-02:** Explicit business demand target, growth and reserve calculations; optional exact-target probe inside exploration bounds; offline `goal` command.
- [x] **B1-03:** Aggregate request, named-step and completed-journey p50/p95/p99, failures, scheduled/dropped arrivals, JSON reporting and human-readable insights.
- [x] **B1-04:** Conditional, one-sided 95% upper error-rate bound under independent/stationary Bernoulli assumptions; an assumption-dependent P95 rank interval and empirical repeat spread are tracked in B2, but dependence-aware inference remains open.
- [x] **B1-05:** Optional Linux **host-wide** CPU/RAM observations and timestamp-based correlation; B2 adds a one-time container snapshot and bounded application-metric collection, not continuous per-service correlation or causal diagnosis.
- [x] **B1-06:** Guided CLI, temporary browser login for locally held cookies, saved scenarios, report comparison and source-based installation/update flow.

**Evidence boundary at baseline:** the local fixture checks detect gross measurement/accounting failures under specific conditions. They do **not** determine universal generator capacity, remote network timing accuracy, realistic population behavior, sustainable application capacity, or the benefit of a server upgrade. Every campaign needs a host-local calibration preflight for the proposed load envelope.


### B2 — Additional experimental slices in PR #27

The following implementation slices extend the baseline without meeting every verification requirement of G1–G6. **Keep a gate unchecked until all acceptance conditions in its own task are met.** These capabilities remain scoped to bounded, authorized workloads and must not be described as general production-capacity certification.

- **G1-01–G1-03/G1-09 (partial):** Strictly validated, named weighted HTTP profiles with deterministic closed-loop actor assignment, weighted open-loop journey selection, per-profile accounting and a shared safety budget. Production-derived mix validation, partly-open sessions and a complete workload-representativeness protocol remain open.
- **G1-07/G1-11–G1-13 (partial):** Opt-in per-journey synthetic identifiers and bounded JSON Pointer/response-header extraction for subsequent request substitutions. This is not browser-based login recording, full credential lifecycle management, or application-specific write-data cleanup.
- **G2-01–G2-05/G2-10–G2-17 (partial):** Bounded campaigns with warm-up, separate observation windows, optional VU spikes, recovery/repeats and request-window SLO checks; the initial observation/soak bound is 10 minutes. Longer validated soaks, stabilization evidence, full journey-level gates and reproducible end-to-end capacity qualification remain open.
- **G3-04/G3-05/G3-09/G3-16 (partial):** Conditional IID-assumption P95 rank interval, empirical repeat spread, a bounded Little's Law consistency signal, revision/environment labels for cautious comparisons, and offline business-goal assessment from completed campaign evidence. Dependence-aware coverage, strict experiment equivalence and general decision certification remain open.
- **G4-02/G4-03/G4-06 (partial):** Explicitly authorized Docker container CPU/memory **snapshot** and bounded collection of configured Prometheus-text application metrics. No ongoing Docker/DB correlation, full adapter contract, clock-skew validation or causal bottleneck proof is implied.
- **G6/compatibility (partial):** Loopback-only read-only report dashboard and cross-compiled source CLI targets for Linux, Windows and macOS. Scenario-building web UI, signed releases and runtime compatibility guarantees are still open.

See the corresponding ADRs 0007–0012, implementation tests and measurement documentation for precise assumptions and limitations.

## 1. Implementation order and decision gates

| Gate | Priority | User-visible outcome | Depends on | Acceptance evidence |
| --- | --- | --- | --- | --- |
| **G0 — Measurement credibility** | P0 | A run indicates whether its generated load and timing are trustworthy in that envelope | Current B0 | Repeatable controlled fixtures, calibrated delivery/clock thresholds and generator-health evidence |
| **G1 — Representative HTTP workload** | P1 | Multiple independent business journeys with a defined mix, data and authentic identities | G0 | Synthetic oracle for journeys, proportions, auth isolation and read/write effects |
| **G2 — Sustained capacity protocol** | P1 | Passing load maintained throughout declared observation windows, not just 20–30-second aggregates | G0–G1 | Warm-up/steady/recovery separation, repeat trials, SLO windows and aborts |
| **G3 — Statistical validity and reports** | P1 | Clearly supported, unsupported or inconclusive target demand, with traceable measurement evidence | G2 | Run-level uncertainty, reproducible verdicts, complete data and executive/technical replay |
| **G4 — Optional bottleneck evidence** | P2 | Service-level resource observations and controlled before/after comparisons | G2 | Validated adapters, time sync, telemetry completeness, comparable campaigns |
| **G5 — Decision-quality capacity planning** | P2 | Evidence-backed maintain/optimize/investigate/scale scenarios, cost trade-offs and growth outlook | G3–G4 when required | Identical-workload alternatives, model holdouts and no invented capacity |
| **G6 — Stable core release** | P1/P2 | New users complete a safe and reproducible scenario without needing deep performance expertise | G0–G3 + security | Real-user usability, multi-platform CI, upgrade compatibility and public sanitized validation evidence |

The *first functional release* need not wait for every P2 adapter or optional browser feature. However, a **validated sustainable-capacity claim** requires G0–G3 evidence **for that workload, host and observation interval**, not just that the corresponding features exist in the code.

## 2. G0 / P0 — Make the measurements defensible

**Owned areas:** `internal/engine`, `internal/scheduler`, `internal/reference`, `internal/generatorhealth`, `internal/report`, `cmd/damocline-calibrate`.

### G0-A — Calibrate the generator beyond one loopback case

- [ ] **G0-01:** Record a versioned preflight artifact containing generator OS/arch, logical CPU, GOMAXPROCS, Go version, runner version, time window, network mode, scenario fingerprint, payload sizes and observed delivery limits. **Verify:** schema round-trip, compatibility, scrubbed snapshots.
- [ ] **G0-02:** Add explicit preflight acceptance for a requested workload (not just a global calibration PASS), comparing required arrivals/s, request mix, connections and body sizes to **validated fixture ranges**. **Verify:** outside-range cases yield `INCONCLUSIVE`.
- [ ] **G0-03:** Extend the reference matrix to fixed response sizes, variable bodies, slow streaming responses, connection reuse/off, TLS setup, DNS/connect faults and controlled latency tails. **Verify:** independently counted expected requests and expected outcome distributions.
- [ ] **G0-04:** Calibrate offered/started/completed/rejected arrival counts across stage transitions, forced scheduler pauses, OS load, worker exhaustion and cancellation. **Verify:** no silently lost arrivals; all count invariants tested.
- [ ] **G0-05:** Distinguish binding `max_rps` cap, unavailable synthetic worker, missed scheduler deadline, process CPU pressure, connection pool exhaustion and server refusal as **different findings**. **Verify:** one isolated fixture for each and an honest unknown state.
- [ ] **G0-06:** Measure generator clock/scheduling lag distribution during each workload phase, and record the timing method and error tolerances. **Verify:** injected known delays produce a reported delay within stated, tested tolerances.
- [ ] **G0-07:** Calibrate CPU and RSS sampling overhead; detect unavailable, stale or implausible OS/process measurements rather than replacing with zeros. **Verify:** synthetic missing/zero/overflow/short-interval cases.
- [ ] **G0-08:** Detect when generator preflight is missing, from a different host, expired, or from a non-equivalent workload; enforce a **measurement-quality warning/gate** before an owner-facing capacity decision. **Verify:** mismatched artifacts cannot certify headroom.
- [ ] **G0-09:** Keep fixture and production run memory bounded with configured output limits, bounded time-series buffers and safe interruption. **Verify:** stress/fuzz runs, leak checks and clear resource-limit failures.
- [ ] **G0-10:** Add a small, reproducible benchmark runner for Linux and Windows that writes machine-readable sanitized evidence; compare repeated results to a recorded *expected fixture behavior*, not a universal throughput number. **Verify:** CI and documented local reproduction.
- [ ] **G0-11:** Recheck the safety ceiling after new generator validation; raise caps only through an **explicit opt-in** feature with independently validated new envelopes. **Verify:** existing production-safe defaults and rate ceilings remain unchanged otherwise.

### G0-B — Standardize HTTP measurements and correctness

- [ ] **G0-12:** Define exact timestamp boundaries for request queue wait, rate-token wait, DNS, TCP, TLS, first response byte, body receive, transport errors and full client-observed latency (e.g. Go `httptrace`). **Verify:** local deterministic phase fixtures and published metric semantics.
- [ ] **G0-13:** Record per-step request latency separately from journey elapsed time and configured think time; keep canceled/incomplete journeys distinct from completed percentile samples. **Verify:** scripted multi-step fixture with known pauses and aborts.
- [ ] **G0-14:** Retain counters for HTTP status families, assertion failures, network timeouts, TLS failures, redirects, rate-limit suppression and unexpected response size. **Verify:** injected failure matrix with exact counts.
- [ ] **G0-15:** Validate against a server-side request ledger at every test boundary, including admitted-but-incomplete work after graceful drain. **Verify:** conservation equations and failure-path integration tests.
- [ ] **G0-16:** Introduce explicit run **data quality states** (`VALID_WITHIN_ENVELOPE` / `LIMITED` / `INVALID`), independent of whether the application met its SLO. **Verify:** invalid generator cannot yield a server-limit conclusion.
- [ ] **G0-17:** Establish schema migrations and golden fixtures for current report v4 and prior supported report versions, preserving read compatibility. **Verify:** round-trips, backward-compat tests, no silently misread old fields.

**G0 exit test:** calibration is repeatable on the actual generator class; the proposed workload lies within its validated local envelope; scheduling/cancellation/timing/counts reconcile; process health is present or explicitly unavailable. A skipped, failed, stale or non-equivalent preflight must **never** be described as a scientifically validated capacity result.

## 3. G1 / P1 — Model realistic HTTP workload without a browser recorder

**Owned areas:** new `internal/workload` (proposed), `internal/config`, `internal/engine`, guided CLI.

### G1-A — Workload mix and human-like activity

- [ ] **G1-01:** Add a versioned multi-workflow scenario format: named journey, ordered HTTP steps, optional weight, workload model and independent authentication context. **Verify:** backward-compatible single-workflow JSON migration and strict parser tests.
- [ ] **G1-02:** Allow at least two concurrent workflows with independent weights, e.g. 60% read dashboards, 25% search, 15% save. **Verify:** large seeded synthetic run reaches expected ratios with statistical tolerance; not exact fixed counts unless requested.
- [ ] **G1-03:** Define whether weights apply to **journey starts, time spent, HTTP requests or VUs**; support precisely one declared unit per mix. **Verify:** differing journey lengths do not silently change the intended ratios.
- [ ] **G1-04:** Support selectable closed-loop actors, independently scheduled journey arrivals and (later in G1) partly-open **session** arrivals. **Verify:** slowing the target changes closed-loop throughput but not the intended open schedule.
- [ ] **G1-05:** Add deterministic/seeding support for scenario selection, think-time distributions and reproducible actor assignment. **Verify:** same seed recreates workload decisions under a controlled schedule.
- [ ] **G1-06:** Add configurable *bounded* think-time distributions (fixed, uniform, empirically sampled), preserving a deterministic test mode. **Verify:** generated times conform to specified units/ranges/distribution.
- [ ] **G1-07:** Support varied payload sizes, request parameters and synthetic data fixtures without embedding production identifiers. **Verify:** all generated combinations are bounded and sanitized.
- [ ] **G1-08:** Support session start/end, arrival-rate distribution and dependent actions within a partly-open session; do not call concurrent active sessions "independent people" without calibrated activity assumptions. **Verify:** controlled arrivals and think-time distribution against known traces.
- [ ] **G1-09:** Add per-workflow budget allocation and a shared global limit so concurrent flows cannot bypass `max_rps` / VU safety controls. **Verify:** combined traffic never exceeds explicitly permitted bounds.
- [ ] **G1-10:** Add scheduled mix changes (e.g. peak workday vs normal) within a campaign, labeled as **different workload phases**. **Verify:** each phase's mix and delivered load separately accounted.

### G1-B — Sessions, state and business correctness

- [ ] **G1-11:** Isolate cookies, per-VU variables, synthetic record IDs and login state across actors; prevent accidental cross-user request replay. **Verify:** two-user synthetic fixture rejects leaked state.
- [ ] **G1-12:** Add opt-in response-variable extraction (JSON field, header or constrained HTML token), with strict size/secret controls. **Verify:** create → returned ID → read/update workflow, negative cases, no secret serialization.
- [ ] **G1-13:** Add request templates that substitute **previously extracted** values with encoding and allowed-host checks. **Verify:** invalid substitutions cannot redirect to unintended hosts or inject headers.
- [ ] **G1-14:** Add configurable session expiry and explicitly authorized refresh/login flows using test accounts; default remains manual/environment-backed credentials. **Verify:** valid/expired/rotated token fixture cases and no passwords persisted.
- [ ] **G1-15:** Validate complete business outcomes, not just HTTP 200: expected status, response assertion and optional state postcondition. **Verify:** false-positive 200/login redirect and unsuccessful write are counted as failures.
- [ ] **G1-16:** Add write-safety controls: opt-in writes, unique synthetic data per worker, idempotency keys when applicable, cleanup strategy, and dry-run preview of mutating routes. **Verify:** no writes without permission; cleanup failure is visible.
- [ ] **G1-17:** Import **sanitized aggregate** visit/action rates or accept explicitly labeled estimates; store source and validity horizon of workload-mix assumptions. **Verify:** no raw user events/identifiers required.
- [ ] **G1-18:** Tag each result with a workload fingerprint (mix, session model, timing, dataset and auth independence) to prevent invalid before/after comparisons. **Verify:** changing mix invalidates automatic "same workload" conclusions.

**G1 exit test:** at least three concurrent named HTTP journeys run with stated proportions and per-actor isolation; completed business actions are asserted; mix, arrival process and think times match synthetic reference distributions; every request remains authorized and bounded. **Visual click recording is NOT required to pass G1.**

## 4. G2 / P1 — Sustainable Capacity Explorer

**Owned areas:** `internal/explorer` and proposed `internal/campaign`, `internal/engine`, `internal/report`.

### G2-A — Campaign execution

- [ ] **G2-01:** Define a versioned capacity campaign: baseline, warm-up, measured steady stage, ramp, spike, recovery and optional soak. **Verify:** schema, safe defaults and a complete dry-run schedule.
- [ ] **G2-02:** Separate warm-up/steady/recovery samples with immutable time-window boundaries; ensure warm-up does not influence steady-state percentiles. **Verify:** injected cold-start latency fixture.
- [ ] **G2-03:** Add bounded *long-running* stages behind explicit staging-first approval and safe abort controls; do not silently remove current 3-minute run / 15-minute exploration ceilings. **Verify:** wall-time, load and cancellation limits.
- [ ] **G2-04:** Support a bounded soak at a candidate **previously tested passing** load, with periodically evaluated windows rather than a single aggregate. **Verify:** synthetic memory leak / latency creep must cause failure.
- [ ] **G2-05:** Support targeted spikes and recovery probes; preserve each phase's planned/achieved load, error rate, latency, unfinished work and generator health. **Verify:** reference server with controlled saturation/recovery.
- [ ] **G2-06:** Detect stabilization using a documented, configurable rule (time-window variance, throughput and backlog), or label the stage **NON_STATIONARY**. **Verify:** steady and intentionally drifting fixture cases.
- [ ] **G2-07:** Stop admission at a precise boundary, drain in-flight journeys for a bounded period, and preserve censored/incomplete counts. **Verify:** slow request, cancel and timeout fixtures.
- [ ] **G2-08:** Abort with a named reason if error/latency limits, lost arrivals, generator CPU/RSS, request ceilings, customer-specified resource safety limits or user interruption trigger. **Verify:** each abort path test.
- [ ] **G2-09:** Validate that the **exact business demand target** is probed whenever safely reachable; do not infer a passing goal solely from nearby stage levels. **Verify:** non-power-of-two target and untested-target cases.
- [ ] **G2-10:** Store every attempted stage and every independent run, including failures, dropped workloads and aborts; prohibit "best run only" summaries. **Verify:** forced first-run failure cannot be hidden by later successes.
- [ ] **G2-11:** Add repeated campaigns across independently separated runs/sessions, with explicit cool-down and bounded test data resets. **Verify:** distinct runs retain independent environment/run IDs.
- [ ] **G2-12:** Add optional step-wise *arrival-rate* exploration as distinct from the current closed-loop VU explorer. **Verify:** RPS and VUs are never compared as interchangeable capacity units.
- [ ] **G2-13:** Make search-budget exhaustion, generator-limit binding, auth/session limitations and safety abort result in **INCONCLUSIVE / NOT_REACHED**, not an inferred server ceiling. **Verify:** injected budget, missing-auth and rate-cap fixtures.
- [ ] **G2-14:** Preserve stable observation windows and metrics on resume after interruption only if equivalence can be demonstrated; otherwise restart with a new run identity. **Verify:** interrupted/restarted workload cannot silently merge data.

### G2-B — Definition of a supported workload level

- [ ] **G2-15:** Declare required journey-level SLOs, valid sample coverage, acceptable errors, completed-work ratio, dropped-work policy and time-window requirements **before** a campaign starts. **Verify:** pre-run validation rejects vague/missing objectives.
- [ ] **G2-16:** Classify each stage separately as `PASS_OBSERVED`, `FAIL_OBSERVED`, `INVALID_DELIVERY` or `INSUFFICIENT_EVIDENCE`. **Verify:** deterministic fixture cases for each category.
- [ ] **G2-17:** Require repeated passing steady-state stages plus adequate generator evidence before labeling a workload `SUSTAINED_AT_TESTED_CONDITIONS`. **Verify:** brief spike/cold-start/unstable-trial examples cannot pass.
- [ ] **G2-18:** Report **highest validated tested level** and **first observed failing level** separately, including an explicit unknown upper bound when every stage passes. **Verify:** no extrapolated "maximum users" from untested traffic.

**G2 exit test:** a reproducible synthetic workload passes a sustained level across repeated stable windows; a deliberately degraded server fails; a saturated generator yields **invalid/inconclusive**, and no unlimited workload or invented hard capacity is presented.

## 5. G3 / P1 — Statistics, workflow analysis and evidence-grade reporting

**Owned areas:** `internal/statistics`, `internal/report`, `internal/insights`, `internal/capacitygoal`, `internal/correlate`.

### G3-A — Correct statistical reasoning

- [ ] **G3-01:** Add timestamped, bounded window aggregates for each HTTP step, journey, workflow and complete campaign. **Verify:** windows add up to the exact observation interval and preserve incomplete counts.
- [ ] **G3-02:** Add per-workflow latency and error budgets, including full-journey completion p95/p99 and explicit sample adequacy. **Verify:** one slow critical workflow fails despite globally healthy P95.
- [ ] **G3-03:** Define percentile estimators and quantile approximation error (if using streaming sketches). **Verify:** known synthetic distributions and nearest-rank regression.
- [ ] **G3-04:** Estimate percentile uncertainty from independent runs or dependence-aware blocks, with tested statistical coverage on simulated correlated data. **Verify:** synthetic iid vs correlated ground-truth experiments.
- [ ] **G3-05:** Aggregate trial-to-trial variability without treating thousands of adjacent HTTP requests as independent experiments. **Verify:** clustered-failure examples must have wider uncertainty than naive per-request estimates.
- [ ] **G3-06:** Preserve current conditional binomial error bound, label independence/stationarity assumptions, and add an appropriate run-level/dependence-aware alternative. **Verify:** 0/50 errors must not establish a 1% budget at 95% confidence.
- [ ] **G3-07:** Add a workload adequacy check before reporting p99 or declaring a highly reliable tail objective. **Verify:** small-sample reports say `INSUFFICIENT_EVIDENCE` without arbitrary numeric precision.
- [ ] **G3-08:** Keep separate **measurement confidence**, **workload representativeness**, and **decision confidence** dimensions. **Verify:** excellent generator timing plus unrepresentative workload is not deemed a capacity certification.
- [ ] **G3-09:** Add run equivalence validation for before/after comparisons: matching SLOs, workload fingerprints, data shape, app version, duration, environment and client calibration. **Verify:** mismatched runs yield `NOT_COMPARABLE`.
- [ ] **G3-10:** Add regression thresholds with documented error bars for comparing versions/infrastructure; keep effect size and practical business impact visible. **Verify:** known small/noisy/no-effect cases do not claim improvement.

### G3-B — Reporting that an owner can use

- [ ] **G3-11:** Introduce a typed evidence record linking a capacity conclusion to immutable local run IDs, scenario fingerprint, host type, measured time windows and quality findings. **Verify:** every conclusion traceable to underlying JSON.
- [ ] **G3-12:** Build a single offline summary over the entire campaign: demand target, observed passing/failing levels, tested duration, confidence, limiting workflow and known unknowns. **Verify:** golden human-readable + machine-readable outputs.
- [ ] **G3-13:** Explain latency as user-relevant journey times; distinguish HTTP step time from browser rendering, network setup and think time. **Verify:** no combined "page load time" inferred from one HTTP request.
- [ ] **G3-14:** Display achieved request and **useful business transaction** throughput separately from configured max RPS, offered arrivals and incomplete work. **Verify:** artificially binding rate limit cannot appear as validated server capacity.
- [ ] **G3-15:** Add timeline views of P95/P99, errors, success/completion, dropped starts and resource samples aligned to stages. **Verify:** regression fixtures demonstrate intermittent failures are not hidden by overall averages.
- [ ] **G3-16:** Explain `MEETS_TARGET`, `BELOW_TARGET`, `NOT_TESTED`, `INCONCLUSIVE` and `NEEDS_INVESTIGATION` using deterministic evidence rules, **without** making an automatic hardware recommendation. **Verify:** end-to-end scenario fixtures for all branches.
- [ ] **G3-17:** Produce distinct nontechnical executive and technical diagnostic reports from the same local evidence artifact; include caveats and exact workload definition. **Verify:** snapshot tests and no contradictory verdicts.
- [ ] **G3-18:** Support anonymized report export that automatically redacts hostnames, private route details, cookie/token values, customer IDs and environment secrets. **Verify:** golden secret-leak and fuzz tests; opt-in export only.
- [ ] **G3-19:** Keep old report schemas readable and explicitly distinguish "missing" from genuine zero values. **Verify:** back-compat fixtures for v2/v4 and migration/unsupported schema errors.
- [ ] **G3-20:** Add an offline comparison view for target release before/after optimizer change, showing workload-equivalent confidence and cost-per-useful-transaction inputs when available. **Verify:** unknown cost yields `NOT_ESTABLISHED`, not invented savings.

**G3 exit test:** a nontechnical reader can answer **"Does the defined workload meet our explicit objectives for the observed duration?"**; a reviewer can reproduce the verdict from anonymized artifacts. The report does not claim real-user capacity from raw VU counts or statistical certainty from an invalid sample.

## 6. G4 / P2 — Optional infrastructure diagnostics and controlled experiments

**Owned areas:** `internal/hostmon`, `internal/correlate`, proposed `internal/telemetry` adapters.

- [ ] **G4-01:** Define a read-only adapter contract with UTC timestamp, clock-offset estimate, collector overhead, permissions and missing-value semantics. **Verify:** offline fake adapter/clock-skew fixtures.
- [ ] **G4-02:** Capture CPU/RAM for the **application container/process** separately from machine-wide load and load-generator use. **Verify:** isolated CPU-bound container case with resource totals.
- [ ] **G4-03:** Add optional read-only Docker metrics: per-service CPU, throttling, memory, memory limit/OOM, restarts, network and block I/O. **Verify:** CPU throttling and OOM fixtures, least-privilege access tests.
- [ ] **G4-04:** Add optional PostgreSQL adapter for pool pressure, connections, wait categories, deadlocks, cache hits and query duration aggregates **without SQL parameters or row contents**. **Verify:** synthetic lock/wait workload and redaction.
- [ ] **G4-05:** Add optional Redis adapter for memory/evictions, CPU, client connections, blocked clients, hit ratio and command latency aggregates. **Verify:** read-only fixture and synthetic eviction/load case.
- [ ] **G4-06:** Support application request rate, errors and duration from existing metrics endpoints (optional, bounded, local configuration). **Verify:** sampled metric alignment and no external credentials in reports.
- [ ] **G4-07:** Add optional OpenTelemetry/trace correlation using pre-existing application instrumentation, sampling constraints and no automatic PII collection. **Verify:** synthetic spans connect correct journey window/route.
- [ ] **G4-08:** Add cloud/Kubernetes adapters only after the generic adapter contract is validated; keep HTTP-only operation independent of them. **Verify:** missing adapter does not block core load test.
- [ ] **G4-09:** Quantify collection overhead on small VPSs and automatically flag missing/out-of-window resource data. **Verify:** agent idle/load overhead tests and clock-offset checks.
- [ ] **G4-10:** Rank **supported hypotheses** (CPU saturation, DB waits, network limits, worker concurrency) with evidence links; label correlation as noncausal. **Verify:** ambiguous synthetic traces yield `UNKNOWN`.
- [ ] **G4-11:** Allow user-approved identical-workload A/B trials for software tuning vs CPU/RAM/worker changes; retain configuration cost and SLO deltas. **Verify:** randomized/repeated comparisons avoid mixing workload and version changes.
- [ ] **G4-12:** Provide a separate **no telemetry available** diagnostic path: HTTP observations and limitations, not invented root cause. **Verify:** remote URL-only fixture cannot produce a fake DB/CPU diagnosis.
- [ ] **G4-13:** Stop monitors on cancel; never open unauthenticated public monitoring ports or require database admin rights to run the core engine. **Verify:** permission, timeout and cleanup tests.

**G4 exit test:** trusted adapter values align with HTTP windows and can support falsifiable bottleneck hypotheses; comparable tuning/resource alternatives are recorded. A CPU chart alone must never imply that a larger VPS fixes the problem.

## 7. G5 / P2 — Science-grounded capacity planning and investment decisions

**Owned areas:** `internal/capacitygoal` plus proposed `internal/model` and `internal/planner`.

- [ ] **G5-01:** Record peak active-user **definition** and source (measured sessions, concurrency, arrival trace or declared estimate), not just an integer input. **Verify:** unknown human→VU equivalence stays explicit.
- [ ] **G5-02:** Represent growth horizon, peak multiplier, seasonality and safety reserve as **demand scenarios**, separate from tested-capacity evidence. **Verify:** deterministic computations and uncertainty/sensitivity boundaries.
- [ ] **G5-03:** Relate user activity, session duration, actions/minute, think time and backend requests per action when estimating workload units. **Verify:** synthetic trace reproduces derived arrivals, with assumptions shown.
- [ ] **G5-04:** Add Little's Law (`L = λW`) only as a consistency check for matching workload boundaries and stable intervals. **Verify:** queueing fixture satisfies relation within experimentally justified tolerance; transient data rejects check.
- [ ] **G5-05:** Fit a constrained Universal Scalability Law to **repeated measured throughput** across stable load levels; retain coefficients, residuals and applicability range. **Verify:** synthetic known curves + counterexamples.
- [ ] **G5-06:** Hold back one or more actual measured stages for model validation; reject unstable/bad fits and mark beyond-range forecasts explicitly speculative. **Verify:** noisy/poor fit does not generate a trusted max-VU prediction.
- [ ] **G5-07:** Provide sensitivity intervals for projected business demand and capacity, not false point precision. **Verify:** different assumptions propagate into an explicit range/unknown state.
- [ ] **G5-08:** Compare observed sustainable **tested** level and required demand-plus-reserve under the **same workload fingerprint**. **Verify:** workload mismatch cannot yield `MEETS_TARGET`.
- [ ] **G5-09:** Distinguish "current target passed," "reserve passed," "target failed," "target untested" and "test generator inadequate." **Verify:** fixture-driven decision table with all outcomes.
- [ ] **G5-10:** When a demand target fails, recommend **investigation** before recommending CPU/RAM; software, database, workers and network alternatives remain eligible. **Verify:** target failure with no telemetry never automatically recommends upgrading.
- [ ] **G5-11:** Compute optional marginal cost per **successfully completed business transaction** for observed comparable configurations. **Verify:** missing prices or non-equivalent deployments produce `UNKNOWN`.
- [ ] **G5-12:** Require a matched controlled experiment before ranking tuning vs infrastructure upgrades, and report effect size, uncertainty, cost and reversal/rollback risk. **Verify:** simulation where larger server does not help yields no unjustified upgrade.
- [ ] **G5-13:** Define a review trigger for future demand drift, SLO regression, data growth, deployment change or shrinking margin. **Verify:** alert rules use measured changes and stated assumptions, not unsupported hard limits.
- [ ] **G5-14:** Maintain traceable, deterministic recommendation rules so the same observations always yield the same conclusion; optional AI explanation must not modify verdicts. **Verify:** golden evidence-to-decision tests.

**G5 exit test:** for a business goal, the tool can say **what was demonstrated**, **what is estimated**, **what remains unknown**, and **which intervention was shown to improve outcomes in comparable tests**. It does not advertise a universal number of humans supported.

## 8. G6 / P1–P2 — Finish the accessible local-first CLI

**Owned areas:** `internal/cli`, guided flow, source installation and local artifact handling.

- [ ] **G6-01:** Choose goal in plain language: quick health check, specified peak target, search for observed limit, compare a change, or validate sustained load. **Verify:** terminal interaction tests including no-color/redirected mode.
- [ ] **G6-02:** Offer a safe "one URL / one GET" starting path with meaningful status/content checks and strong explanation of what cannot be learned from it. **Verify:** local fixture, no false user-capacity statement.
- [ ] **G6-03:** Configure multiple weighted workflows by selecting names, routes, weights, think time and auth profile; preview the actual proposed workload before generation. **Verify:** integration tests for option serialization.
- [ ] **G6-04:** Ask for authorized staging/production status, max offered rate, duration and write operations separately; warn about operational risk and prohibit unsafe defaults. **Verify:** explicit confirmation at every workload escalation.
- [ ] **G6-05:** Integrate calibration preflight directly into a **capacity** campaign, including warning/stop if current generator cannot produce the requested load; permit offline report browsing without preflight. **Verify:** calibration missing/mismatched host behavior.
- [ ] **G6-06:** Save workload, budget, repeats, all trial results and assumptions in private local profiles with versioned migrations. **Verify:** round-trip across upgrade and cleanup.
- [ ] **G6-07:** Explain real units in the UI: active modeled VUs, request rate, arrival rate, RPS ceiling, completed useful journeys and p95 SLO. **Verify:** help and snapshot tests are consistent with measurement docs.
- [ ] **G6-08:** Show a live **safety/status dashboard** in the terminal: phase, offered/achieved rate, errors, generator limits, next action and stop hotkey. **Verify:** no command invokes hidden network traffic.
- [ ] **G6-09:** Offer finish/report navigation with executive summary first and a clear path to technical evidence or JSON. **Verify:** CLI integration tests for PASS, FAIL and INCONCLUSIVE.
- [ ] **G6-10:** Add a guided "compare before/after" campaign that prevents inequivalent workload/environment comparisons. **Verify:** mismatch message and no fabricated performance gain.
- [ ] **G6-11:** Implement crash-safe local report persistence (atomic writes/versioned history) with retention limits and explicit deletion; never commit local results. **Verify:** interrupted process leaves previous reports intact.
- [ ] **G6-12:** Test installation, Unicode/ASCII output, keyboard navigation, non-interactive invocation and source updater on Windows and Linux. **Verify:** cross-platform CI and an offline mode.
- [ ] **G6-13:** Conduct small, documented usability sessions with target developers (new user sets up scenario, interprets validity, compares target) and address observed misunderstandings. **Verify:** task completion notes in sanitized public summary.
- [ ] **G6-14:** Add integration checks that guided paths and direct commands share the same safety, validation and underlying engine behavior. **Verify:** no CLI-only bypass to limits or writes.

**G6 exit test:** a first-time developer can safely create, run and understand a bounded HTTP scenario, distinguish a tested workload from a real-user capacity guarantee, and reproduce the conclusion locally.

## 9. Cross-cutting safety, CI and release quality

**These tasks gate the relevant feature; do not defer them to one large security PR.**

- [ ] **Q-01:** Maintain a threat model for source install, updater, secrets, target authorization, redirect handling, local browser session cookies, report export and optional telemetry. **Verify:** abuse-case security tests.
- [ ] **Q-02:** Fuzz URL/path, header, token/body substitution, time-series decoding, report schema and config parsing, with resource caps and clear validation errors. **Verify:** CI smoke fuzz corpus.
- [ ] **Q-03:** Prevent target-auth credentials, cookies, request bodies, private hostnames and collected database values from being logged/exported by default. **Verify:** secret-marker regression fixture across errors, reports and logs.
- [ ] **Q-04:** Restrict destructive operations to explicit opt-in with preview, immutable audit metadata and safe test-data teardown where possible. **Verify:** unauthorized or malformed write requests never run.
- [ ] **Q-05:** Verify shutdown semantics for Ctrl+C, task timeout, partial worker failure, monitor failure, network outage and OS signal. **Verify:** no leaked worker/process/collector or unreported incomplete work.
- [ ] **Q-06:** Gate releases with `go test ./...`, race detection on supported platforms, `go vet ./...`, deterministic local-reference matrix, build, and CLI smoke/installer tests. **Verify:** CI artifacts and independent reruns.
- [ ] **Q-07:** Add benchmark regression envelopes for scheduler throughput, memory growth and correctness; fail CI only on calibrated stable invariants, not arbitrary cloud-runner P95 jitter. **Verify:** noisy CI case remains informative.
- [ ] **Q-08:** Validate JSON migration rules from previous supported versions, reject future unknown incompatible versions, and document behavior changes. **Verify:** historical regression fixtures.
- [ ] **Q-09:** Document supported Go/OS versions and test platform variations including limited CPU, slow disk and low memory. **Verify:** explicit compatibility matrix.
- [ ] **Q-10:** Publish **sanitized** validation methodology and measured limitations for each core release. **Verify:** reproducible fixture scripts, exact host fingerprint categories and no private workloads.
- [ ] **Q-11:** Add explicit warnings for active production testing and proof of authorization; never use a customer environment as the default CI target. **Verify:** all continuous checks use loopback.
- [ ] **Q-12:** Track field failures, statistical false positives and surprising load-generation behavior and turn every demonstrated bug into a deterministic local regression fixture. **Verify:** triaged issues with reproduce/expected/observed sections.

## 10. Post-core / P3 — Advanced features, intentionally deferred

These must not block the safe, useful HTTP capacity product.

- [ ] **A-01:** Optional browser interaction recorder that extracts **reviewable** network journey definitions rather than raw secrets.
- [ ] **A-02:** Optional browser-based UI experience measurements, clearly segregated from Go HTTP backend timing.
- [ ] **A-03:** Optional visual recording/replay with locators, screenshots and sanitization, only after the core HTTP scenario model handles dynamic sessions.
- [ ] **A-04:** Distributed generators with synchronized schedules, partitioned VU identities, skew estimates and merge-time conservation checks; no duplicate arrived work.
- [ ] **A-05:** Kubernetes/cloud telemetry provider integrations beyond the initial read-only adapter contract.
- [ ] **A-06:** Local web UI for charts, workflow design and report history with the same safety/evidence rules as the CLI.
- [ ] **A-07:** Automated scenario suggestions and natural-language explanations as optional assistance, without automatic authority to execute traffic or certify results.

## 11. Validation matrix — tests that must exist before capacity claims

| Fixture / experimental condition | Main failure to detect | Expected evidence or verdict | Required gate |
| --- | --- | --- | --- |
| Deterministic server with fixed response time and known status | Incorrect timing, counting or percentile | Correct scheduled/delivered/completed ledger; latency within declared fixture tolerance | G0 |
| Controlled latency tails and connection churn | Aggregated p95 hides outlier and setup cost | Request/step/journey timing and explicit phase boundaries | G0, G3 |
| Paused/saturated generator; binding client rate cap | Mistaking missing offered load for a target limit | Generator-related limitation; **not** server capacity failure | G0, G2 |
| Server with bounded concurrency and long queue | Artificial closed-loop self-throttling or masked dropped arrivals | Correct open-vs-closed behavior and incomplete work | G0, G1 |
| Three weighted synthetic HTTP workflows | Incorrect traffic mix, shared identity or state | Per-workflow realized mix, state correctness, shared global cap | G1 |
| Dependent create → read → update with synthetic IDs | HTTP 200 returned despite incorrect business outcome | Journey assertion fails; data isolated and cleaned up | G1 |
| Warm-up slowdown then stable service | Transient response incorrectly considered steady | Disjoint warm-up/steady/recovery metrics | G2 |
| Gradual memory leak / latency drift | Aggregate PASS despite sustained degradation | Time-window failure or non-stationary/inconclusive result | G2, G3 |
| High load, recovery and graceful drain | Lost or unreported incomplete work | Exact cancelled/dropped/completed counts and recovery evidence | G2 |
| 0/50 observed request failures with 1% error budget | Small sample misrepresented as reliability proof | Bound shown as insufficient under stated assumptions | G3 |
| Correlated bursts and noisy independent trials | Artificially narrow confidence interval | Dependence-aware uncertainty or explicit unknown | G3 |
| Different application version, dataset or workload mix | Invalid A/B cost/capacity comparison | `NOT_COMPARABLE` with concrete mismatch | G3, G4 |
| CPU-constrained container vs DB lock fixture | One generic CPU chart misdiagnoses cause | Distinct supported bottleneck hypotheses, telemetry alignment | G4 |
| Missing Docker/DB metrics on remote service | Speculative infrastructure diagnosis | HTTP-only conclusion; root cause **unknown** | G4 |
| Measured plateau, held-out load level and poor curve fit | Unsafe extrapolation of model-based maximum | Fit rejected or forecast labeled conditional/unverified | G5 |
| Target 200 modeled actors + 30% reserve, target not reached | Growth formula misrepresented as supported workload | "Required 260; not validated" (illustrative), no upgrade automatically | G5 |
| Same workload on baseline and tuned/expanded configurations | Upgrade recommended without controlled evidence | Compare uncertainty, SLO and costs for *observed* variants only | G4, G5 |
| Windows/Unix terminal, offline and interrupted source workflow | UX or safety paths bypass engine constraints | Equivalent validation/authorization and recoverable local artifacts | G6, Q |

Acceptance thresholds must be justified by the **specific fixture, hardware class and business SLO**. No fixed 90% CPU rule, 30-second soak, p95 from a tiny sample or default benchmark rate is a universally scientifically established capacity threshold.

## 12. Definition of Done and release readiness

**Every substantive PR** must satisfy all applicable items:

- [ ] Scope corresponds to a roadmap ID; document whether implementation, controlled measurement or real-environment validation changed.
- [ ] User story includes a concrete owner/developer question and the correct measurement units.
- [ ] Test(s) cover success, failure, boundary, cancellation, invalid input and generator-limited conditions.
- [ ] Preserve existing safety ceilings unless the new envelope is independently revalidated, reviewed and explicitly opted into.
- [ ] Keep code, fixture tests and public docs free of actual customer identities and secrets.
- [ ] Update report schema/version and backwards reader tests for any output changes.
- [ ] Update user-visible descriptions, limitations and `CHANGELOG.md` as applicable.
- [ ] Link CI results and reproducible **local-only** validation steps; do not present a CI green check as field capacity certification.

**Release gates (not scheduled dates):**

| Release track | Minimum passing criteria | Permitted public statement |
| --- | --- | --- |
| **Experimental / current** | Existing bounded runner, reference tests and initial explorer | "Observed synthetic HTTP behavior at specified load and duration." |
| **Measurement-qualified core** | G0, G1 and Q measurement/auth gates | "Workload delivered and measured within validated host/workload conditions." |
| **Sustained-capacity qualified core** | G0–G3, G6 and Q; repeated representative stable campaigns | "This defined workload was sustained at the highest **tested passing** level over this observation period, with stated uncertainty." |
| **Decision-support qualified** | Sustained core + G4/G5 evidence **for the claim actually made** | "Observed alternatives and bottleneck evidence support this recommendation; missing evidence is explicitly inconclusive." |
| **Advanced optional** | Relevant A-series checks | Separate browser/distributed features, never used to retroactively claim HTTP engine accuracy. |

**Primary success criterion:** the owner can ask **"Will this deployment handle my defined peak demand and desired reserve?"** and receive one of: **supported under tested conditions**, **observed SLO breach**, or **insufficient evidence**. If improvement is needed, the product distinguishes **investigate**, **optimize software/data**, and **evaluate larger infrastructure** based on *controlled evidence*, rather than prescribing a bigger VPS from an HTTP PASS/FAIL alone.

## 13. Suggested next PRs, in dependency order

These are remaining acceptance milestones, **not a claim that each listed item lacks any code**. PR #27 provides partial slices of several milestones as noted in B2; the outstanding verification and integration criteria still apply. Work on the smallest vertical slice that produces verifiable user value.

1. **Next 1 — G1-01 to G1-03 + G1-09:** multi-workflow schema, weighted journeys and global rate/actor budget. Keep existing single-workflow JSON working; add a three-workflow controlled fixture.
2. **Next 2 — G1-05 to G1-07 + G1-11:** repeatable timing/data variability and actor isolation. Add safe fixture assertions before enabling independent write-heavy profiles.
3. **Next 3 — G2-01/G2-02/G2-07 + G3-01:** campaign phases and bounded time-window metrics. Correct windowing before adding longer stages.
4. **Next 4 — G2-03 to G2-06 + G2-10/G2-15/G2-16:** sustained/soak/recovery campaign and per-window SLO decisions with safe aborts.
5. **Next 5 — G3-02 to G3-08:** workflow-level statistical validity, adequate tail samples, repeat variance and evidence-quality assessment.
6. **Next 6 — G3-11 to G3-19 + G6-01 to G6-09:** coherent executive/technical output and a complete, user-tested CLI path.
7. **Next 7 — G4-01 to G4-04 + G4-12:** optional infrastructure adapters (Docker and PostgreSQL first), with strict read-only access and no forced dependency.
8. **Next 8 — G5-01 to G5-14:** science-grounded business goals, queueing/USL checks only when data qualifies, and controlled cost/performance alternatives.

Other G0 tasks proceed as correctness prerequisites whenever a new workload envelope or measurement class is introduced. CI/security/compatibility tasks in Q run **throughout** all eight steps, not as a final afterthought.

---

**North star:** *Find the limit before it finds you* means discovering **validated, workload-specific limits** with reproducible evidence and actionable interpretation — not promising an unmeasured number of users.
