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

- **G1-01–G1-03/G1-09/G1-17 (partial):** Strictly validated, named weighted HTTP profiles with deterministic closed-loop actor assignment, weighted open-loop journey selection, per-profile accounting, shared safety budget, and schema-v9 self-declared mix provenance retained in later report schemas. Source validity/freshness and production-derived mix validation, partly-open sessions and a complete workload-representativeness protocol remain open.
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

See the [G0 acceptance matrix](G0_STATUS.md) for the implementation, test, validation and remaining-gap audit across G0-01–G0-17.

**Owned areas:** `internal/engine`, `internal/scheduler`, `internal/reference`, `internal/generatorhealth`, `internal/report`, `cmd/damocline-calibrate`.

### G0-A — Calibrate the generator beyond one loopback case

- [x] **G0-01:** Record versioned loopback preflight metadata (generator OS/arch/logical CPUs/GOMAXPROCS/effective CPU entitlement/Go/source revision or exact executable digest, time boundaries, network mode, fixed 4 KiB fixture description and fingerprint, observed stage arrival delivery, bounds). **Verified within its fixture-only scope:** schema-v7 round-trip, incompatibility, redaction tests, and Linux/Windows calibration CI; workload-specific equivalence and aging remain G0-02/G0-08. Linux quota evidence walks the process cgroup hierarchy; cpuset constraints remain unverified.
- [ ] **G0-02:** Add explicit preflight acceptance for a requested workload (not just a global calibration PASS), comparing required arrivals/s, request mix, connections and body sizes to **validated fixture ranges**. **Verify:** outside-range cases yield `INCONCLUSIVE`. **Partial implementation:** offline declared-dimension assessment plus `run --calibration` validates and attaches bounded preflight evidence. Calibration schema v6 executes 30 exact workload shape/connection-policy/rate pairs through the native engine and compares arrival/body observations and accepted TCP connections with independent child-server counters: GET responses 0/1/4/64 KiB, plus POST request/response bodies of 4/4 KiB, each at exactly 2, 5, and 10 arrivals/s under both reusable HTTP (5-second duration) and new-per-request (2-second duration), with 10 workers, max_rps 50 and 1-second timeout. The exact shape and complete control tuple are required; untested rates and ceilings do not interpolate. Schema-v3 through v5 artifacts remain readable with their historical narrower profiles. Deterministic exact-pair, altered-tuple, connection-ledger and compatibility tests exercise both policies. Repeated schema-v6 runtime acceptance passed twice on Windows AMD64, Go 1.26.3, 12 logical CPUs; each run reconciled 30/30 profiles, exact per-profile independent accepted TCP counts and 980/980 aggregate independent server requests. Environment-backed request bodies are compared only by byte length and their content is not persisted. This still does not validate TLS, other body sizes, variable response distributions, multi-step profiles, physical host identity, cross-host equivalence or external application capacity; ordinary application runs remain inconclusive. The schema-v7 repeated acceptance now exercises this exact profile matrix in a quota-bounded Docker Desktop/WSL2 container; bare-metal Linux and cross-host equivalence remain unvalidated.
- [ ] **G0-03:** Extend the reference matrix to fixed response sizes, variable bodies, slow streaming responses, connection reuse/off, TLS setup, DNS/connect faults and controlled latency tails. **Verify:** independently counted expected requests and expected outcome distributions. **Partial:** bounded loopback transport matrix with 0/1/4/64 KiB responses, 503, keep-alive vs close, local fixture TLS, stream and 40ms tails; actual native-engine fixtures with independent ledgers; and a controlled engine test that reconciles reusable versus new-per-request connection events. Trusted local HTTPS and deliberately truncated body responses through the native engine now reconcile request counts against independent server ledgers. Native-engine fixtures also exercise resolver-injected NXDOMAIN and DNS timeout, local refusal, and TCP reset before an HTTP response; reset counts accepted TCP connections without claiming application receipt, while DNS fixtures independently count resolver queries. Schema-v12 reports preserve per-step complete, interrupted and oversized response-body read counts and complete-body min/max/total byte lengths; the empty-body engine fixture also checks exact zero-byte accounting. These establish behavior only for the named fixtures. SERVFAIL, broader arrival-rate/payload calibration, calibrated TLS performance, additional transport combinations and remote network behavior remain outstanding.
- [ ] **G0-04:** Calibrate offered/started/completed/rejected arrival counts across stage transitions, forced scheduler pauses, OS load, worker exhaustion and cancellation. **Verify:** no silently lost arrivals; all count invariants tested. **Partial slices in PRs #33, #34, #68 and this increment:** bounded interruption reconciles scheduled/admitted/dropped arrivals, incomplete client requests/journeys, and an independent loopback ledger; worker exhaustion separately reconciles local busy drops against the server's single admitted request. Schema v17 persists scheduler admissions and verifies planned = admitted + dropped and admitted = worker-started iterations; schema v16 separates planned arrivals cancelled before admission from scheduler-late and worker-busy drops. Existing scheduler-stall and full-delivery matrix tests cover additional cases. OS-load calibration and multi-stage transitions remain open.
- [ ] **G0-05:** Distinguish binding `max_rps` cap, unavailable synthetic worker, missed scheduler deadline, process CPU pressure, connection pool exhaustion and server refusal as **different findings**. **Verify:** one isolated fixture for each and an honest unknown state. **Partial slice in this PR:** deterministic report signals now distinguish client max_rps proximity, workers busy, late arrivals, sampled generator CPU pressure, HTTP error responses and missing telemetry. Connection-pool exhaustion and root cause remain unmeasured/UNKNOWN; isolated end-to-end fault fixtures remain to close the gate.
- [ ] **G0-06 (partial):** Record per-run scheduler lag summaries (campaign phases are separate runs) and calibrate monotonic planned-slot-to-processing lag with nine bounded injected-pause observations at 20 arrivals/s, checked against the known pause minus one slot with 50 ms absolute tolerance. The positive 10-arrival/s soak additionally requires 300 lag samples, p95 <= 50 ms and max < 100 ms; these are fixture-specific qualification bounds. This retains the earlier PR #33 injected-stall and quantile coverage. Broader OS-load, clock-error, and host-class accuracy remain open.
- [ ] **G0-07 (partial):** Record generator sampler interval, maximum observed sample gap, and total/maximum snapshot collection wall time. Invalid zero/non-finite process readings are excluded and exposed as LIMITED; schema-v10 quality checks reject malformed counters/timings and mark gaps over two configured intervals LIMITED. Window-mean process CPU at or above 90% of detected CPU capacity is LIMITED sustained headroom; Linux walks cgroup v1/v2 quota ancestors and uses the tightest finite quota, bounded by runtime logical CPUs and GOMAXPROCS, separately retained in report schema v19. Unknown quota evidence is not interpreted as unlimited. Short-interval peaks remain visible diagnostics and values above 100% are not automatically invalid. Unavailable RSS remains explicit. Collection wall time at or above 5% of the observation duration now makes quality LIMITED; this is an operational screen, not CPU-cost calibration. Deterministic parent-cap and missing-ancestor fixtures cover both cgroup versions. Cpuset and dynamic affinity constraints, direct CPU-cost calibration, host-load repeatability, stale-source detection beyond measured gaps, and sampler-overhead stress tests remain open.
- [ ] **G0-08:** Detect when generator preflight is missing, from a different host, expired, or from a non-equivalent workload; enforce a **measurement-quality warning/gate** before an owner-facing capacity decision. **Verify:** mismatched artifacts cannot certify headroom. **Partial implementation:** offline comparison and run attachment mark stale/future-dated or known runtime-mismatched evidence outside the reference and retain unknown endpoint/host identity. The run path revalidates artifact ledgers and scheduler-lag evidence. Host attestation and enforceable campaign quality gate remain open.
- [ ] **G0-09 (partial):** Enforce exact 1 MiB scenario-input and 16 MiB report-output byte caps, alongside existing bounded workload, response-body and rate/time-series envelopes. Oversized inputs/outputs fail clearly; output rejection happens before file creation/replacement. Stress/fuzz coverage, peak-memory profiling, leak checks and broader interruption/resource-failure evidence remain open.
- [ ] **G0-10 (partial):** The bounded loopback calibration runner writes versioned machine-readable evidence on Linux and Windows. Acceptance manifest v3 executes two or three independent calibrator processes, verifies schema-v7 effective CPU entitlement, exact schema-v6 profile/rate/connection-policy tuples and aggregate/per-profile request/body/TCP-connection ledgers, recomputes controlled-soak quality, checks overload and induced HTTP-failure controls, runs the transport matrix, and preserves state snapshots, reports, hashes and bounded logs through failure or interruption. Failed stages identify exact delivery, outcome, quality, response shape, latency, generator health, scheduler lag, or independent-server-ledger mismatches. Missing, unknown, or INVALID measurement quality cannot pass calibration; non-finite latency is rejected. Repeated schema-v6 acceptance passed twice on Windows AMD64 as previously recorded. Schema-v7 repeated acceptance passed twice in Docker Desktop/WSL2 Linux amd64 using the hierarchical cgroup reader, with finite quota 4, 12 logical CPUs and GOMAXPROCS=12: each run reconciled 30/30 profiles and 980/980 aggregate requests, recomputed the soak as `VALID_WITHIN_ENVELOPE`, and passed 10/10 transport cases plus negative controls. This is container runtime evidence, not bare-metal Linux validation. Broader hardware classes, stable-runner tolerance bands and dependence-aware run variability remain open; repeat spread is descriptive, not a confidence interval.
- [ ] **G0-11:** Recheck the safety ceiling after new generator validation; raise caps only through an **explicit opt-in** feature with independently validated new envelopes. **Verify:** existing production-safe defaults and rate ceilings remain unchanged otherwise.

### G0-B — Standardize HTTP measurements and correctness

- [ ] **G0-12:** Define exact timestamp boundaries for request queue wait, rate-token wait, DNS, TCP, TLS, first response byte, body receive, transport errors and full client-observed latency (e.g. Go `httptrace`). **Verify:** local deterministic phase fixtures and published metric semantics. **Partial:** bounded client trace totals/counts for rate-token wait, DNS, TCP, TLS, first byte, complete/incomplete body reads and full completion/error; body read failures increment only the incomplete count, preserving per-request phase conservation. Schema-v13 records per-step request metrics in each configured observation window and screens window p50/p95 against the fixture coarse 15-150 ms guardrail when each has at least 100 successful observations. The guardrail is descriptive, not an SLO or confidence interval. Pure Transport queue time and callback accuracy under host pressure remain unvalidated.
- [ ] **G0-13:** Record per-step request latency separately from journey elapsed time and configured think time; keep canceled/incomplete journeys distinct from completed percentile samples. **Verify:** scripted multi-step fixture with known pauses and aborts. **Partial slice in this PR:** per-run and per-profile observed between-step sleep totals/counts, independent per-step request latency, completed journey elapsed time, and a multi-step abort fixture that excludes partial journey duration from completed percentiles. Distributional think-time models and clock-accuracy calibration remain open.
- [ ] **G0-14:** Retain counters for HTTP status families, assertion failures, network timeouts, DNS resolution, connection establishment, TLS failures, redirects, rate-limit responses and unexpected response size. **Verify:** injected failure matrix with exact counts. **Partial implementation:** schema-v20 DNS and connection-establishment counters are versioned transport subcategories, classified from wrapped Go resolver/dial errors and reconciled with failure reasons across aggregate, step and window ledgers. Native-engine fixtures induce NXDOMAIN, a local resolver blackhole timeout, local refusal and TCP reset; reset after connection establishment is not labeled as connect failure. The report reader requires the v20 marker and preserves older subcategory counts as unknown. Full Windows and quota-bounded Docker Linux suites pass after serial gate execution. Resolver response subtypes and TLS certificate causes remain open. Error phase does not establish root cause or request receipt. The local rate limiter delays arrivals and does not suppress work.
- [ ] **G0-15 (partial):** Validate against a server-side request ledger at every test boundary, including admitted-but-incomplete work after graceful drain. Existing loopback matrices reconcile successful, overloaded and failed work; independent reference-ledger fixtures cover one interrupted request, six admitted arrivals finishing after scheduling stops, a three-step journey with think time finishing after the observation window, six in-flight requests externally cancelled during the drain interval, and a child generator forcibly terminated after one request reaches the server. The forced-stop fixture reconciles one server-side cancellation and verifies that no final client report is written. Natural deadline expiry and client-side persistence after forced termination remain open.
- [ ] **G0-16:** Introduce explicit run **data quality states** (`VALID_WITHIN_ENVELOPE` / `LIMITED` / `INVALID`), independent of whether the application met its SLO. **Verify:** invalid generator cannot yield a server-limit conclusion. **Partial implementation:** reports include bounded accounting/generator quality, per-step response-size, request-latency, workload-shape, and per-step/per-profile observation-window evidence; current schema recomputation rejects iteration/journey and scheduler-admission conservation mismatches and requires known, matching CPU entitlement for the controlled calibrator soak. The controlled 30-second soak requires complete scheduler-lag samples within its fixed p95/max bounds before positive quality can be recomputed. Windowed `plan` conclusions require per-step and configured profile SLO evidence in every window. The 100-observation latency floor is a screen, not inferential sufficiency. Ordinary target runs remain LIMITED; physical host identity, broad response distributions, dependency-aware inference and external workload headroom remain unverified.
- [ ] **G0-17:** Maintain bounded read compatibility and golden fixtures for supported report schemas v1-v20, preserving missing-versus-zero semantics; no file-rewriting migration is implemented. **Verify:** round-trips, backward-compat tests, explicit schema-v20 classification marker, and no silently misread old fields.

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
- [ ] **G3-04:** Estimate percentile uncertainty from independent runs or dependence-aware blocks, with tested statistical coverage on simulated correlated data. **Verify:** synthetic iid vs correlated ground-truth experiments. **Partial slice in PR #35:** offline demand planning requires the existing IID-assumption P95 rank interval to fit the SLO for aggregates, steps, and every SLO window; correlated-data coverage and dependence-aware intervals remain open.
- [ ] **G3-05:** Aggregate trial-to-trial variability without treating thousands of adjacent HTTP requests as independent experiments. **Verify:** clustered-failure examples must have wider uncertainty than naive per-request estimates. **Partial slice in PR #35:** demand planning requires descriptive observation spread from every independent trial; it is not presented as confidence, and a cluster-aware run-level interval remains open.
- [ ] **G3-06:** Preserve current conditional binomial error bound, label independence/stationarity assumptions, and add an appropriate run-level/dependence-aware alternative. **Verify:** 0/50 errors must not establish a 1% budget at 95% confidence. **Partial slice in PR #35:** the planner checks the existing one-sided bound for aggregates, steps, and each SLO window; 0/50 cannot establish 1%. It explicitly labels IID/stationarity assumptions; dependence-aware bounds remain open.
- [ ] **G3-07:** Add a workload adequacy check before reporting p99 or declaring a highly reliable tail objective. **Verify:** small-sample reports say `INSUFFICIENT_EVIDENCE` without arbitrary numeric precision. **Partial slice in PR #35:** the planner requires the sample count needed by the existing P95 rank method and complete per-window coverage; broader P99 and business-journey adequacy remain open.
- [ ] **G3-08:** Keep separate **measurement confidence**, **workload representativeness**, and **decision confidence** dimensions. **Verify:** excellent generator timing plus unrepresentative workload is not deemed a capacity certification.
- [ ] **G3-09 (partial):** Add run equivalence validation for before/after comparisons: matching SLOs, workload fingerprints, data shape, app version, duration, environment and client calibration. **Verify:** mismatched runs yield `NOT_COMPARABLE`. Schema-v11 reports include a SHA-256 fingerprint of declared profile and step structure (without resolved environment secrets, body values or extracted values); the comparator checks matching workload revision, environment, target, controls, duration and objectives, and identifies the result only as `MATCHED_DECLARED_WORKLOAD`. Application revision is recorded as a treatment and changed revisions receive a distinct warning. Run reports do not embed loopback preflight or authenticated host identity, so generator equivalence remains unknown; actual environment-backed payload equivalence also remains open.
- [ ] **G3-10:** Add regression thresholds with documented error bars for comparing versions/infrastructure; keep effect size and practical business impact visible. **Verify:** known small/noisy/no-effect cases do not claim improvement.

### G3-B — Reporting that an owner can use

- [ ] **G3-11:** Introduce a typed evidence record linking a capacity conclusion to immutable local run IDs, scenario fingerprint, host type, measured time windows and quality findings. **Verify:** every conclusion traceable to underlying JSON.
- [ ] **G3-12:** Build a single offline summary over the entire campaign: demand target, observed passing/failing levels, tested duration, confidence, limiting workflow and known unknowns. **Verify:** golden human-readable + machine-readable outputs.
- [ ] **G3-13:** Explain latency as user-relevant journey times; distinguish HTTP step time from browser rendering, network setup and think time. **Verify:** no combined "page load time" inferred from one HTTP request.
- [ ] **G3-14:** Display achieved request and **useful business transaction** throughput separately from configured max RPS, offered arrivals and incomplete work. **Verify:** artificially binding rate limit cannot appear as validated server capacity.
- [ ] **G3-15:** Add timeline views of P95/P99, errors, success/completion, dropped starts and resource samples aligned to stages. **Verify:** regression fixtures demonstrate intermittent failures are not hidden by overall averages.
- [ ] **G3-16:** Explain `MEETS_TARGET`, `BELOW_TARGET`, `NOT_TESTED`, `INCONCLUSIVE` and `NEEDS_INVESTIGATION` using deterministic evidence rules, **without** making an automatic hardware recommendation. **Verify:** end-to-end scenario fixtures for all branches. **Partial slices in PRs #34 and #35:** offline `plan` refuses a positive state when required phase quality, statistical sample adequacy, conditional SLO bounds, continuous window coverage, or per-trial variability is missing or insufficient. A new defensive slice recomputes phase quality from report fields and rejects a persisted `VALID_WITHIN_ENVELOPE` claim that cannot be reproduced. Current engine reports remain `LIMITED`, and IID/stationarity assumptions are not validated; positive sustainable-capacity certification remains unavailable.
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
