# Evidence-based capacity engineering plan

**Status:** proposed architecture and implementation plan, **not** a claim that these capabilities are currently available.  
**Goal:** answer an application's owner: (1) Does the present deployment meet the expected workload and SLOs? (2) What *tested and sustained* load can it handle? (3) Is growth likely to breach the requirement? (4) Which additional measurements or controlled alternatives would justify optimization or more infrastructure?

Do not conflate registered users, logged-in/idle sessions, **active** synthetic users (with defined think times), in-flight HTTP operations, transactions per second, or arrival rate. Damocline's reported capacity is conditional on a named workload, application/data/deployment revision, test duration, environment, acceptance criteria, and uncertainty. It is not a universal server benchmark.

## 1. Evidence hierarchy and research basis

| Type | Source | Established or documented result | Damocline use / limitations |
| --- | --- | --- | --- |
| Mathematical result | Little, 1961, *A Proof for the Queuing Formula: L = λW* ([DOI](https://doi.org/10.1287/opre.9.3.383)) | In a stable system satisfying the paper's assumptions, average in-system work L = throughput/arrival rate λ times mean time W. | Sanity-check average *in-flight operations* or another consistently defined population. **Never** turn this directly into distinct logged-in users or promise stability from an unstable interval. |
| Peer-reviewed experimental research | Schroeder, Wierman & Harchol-Balter, 2006, *Open Versus Closed: A Cautionary Tale* ([NSDI](https://www.usenix.org/conference/nsdi-06/open-versus-closed-cautionary-tale)) | Open/closed workload models can yield materially different performance behavior. | Require explicit workload-model selection; compare both models under controlled demand. Do not use closed-loop throughput alone to determine externally imposed request capacity. |
| Peer-reviewed capacity-planning methodology | Almeida & Menascé, 2002, *Capacity Planning: An Essential Tool for Managing Web Services* ([DOI](https://doi.org/10.1109/MITP.2002.1046642), [author-hosted paper](https://cs.gmu.edu/~menasce/papers/IEEE-ITProfessional-July2002.pdf)) | Characterize workloads, model the system, calibrate models and compare cost/performance alternatives. | End-to-end decision workflow, including business demand and change-of-infrastructure experiments. A model must be calibrated against real tests. |
| Model and mathematical analysis | Gunther, 2008, *A General Theory of Computational Scalability Based on Rational Functions* ([arXiv](https://arxiv.org/abs/0808.1431)) | Universal Scalability Law (USL) models contention and coherency-driven throughput behavior for suitable systems. | Fit *conditionally* to measured throughput versus active load; use holdout levels, bounds and residuals. Fitted coefficients do **not** prove a hardware root cause, and extrapolation outside measured loads is explicitly labeled uncertain. |
| Peer-reviewed experimental methodology | Georges, Buytaert & Eeckhout, 2007, *Statistically Rigorous Java Performance Evaluation* ([UGent](https://biblio.ugent.be/publication/417084)) | Warm-up, runtime variability, repeated measurements, and sound statistical treatment matter. | Independent trial repetitions and controlled comparisons. Transfer principles to HTTP benchmarking while explicitly distinguishing Java-specific effects. |
| Industry-research guidance | Dean & Barroso, 2013, *The Tail at Scale* ([Google Research](https://research.google/pubs/the-tail-at-scale/)) | Tail latency has disproportionate impact on large services. | Evaluate journey p95/p99, timeouts and worst affected steps; do not claim p99 accuracy from tiny samples. |
| Industry benchmark specification | [TPC-W](https://www.tpc.org/tpcw/) | Session-oriented web workloads, transaction mixes, data sizes, correctness, response constraints and cost/throughput measurements. | Adopt *modeling ideas*, not the obsolete TPC-W score; TPC-W has been obsolete since 2005 and the test is **not** TPC-certified. |
| Engineering reference, **not** academic validation of our code | Grafana [k6 open vs closed](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/), [dropped iterations](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/dropped-iterations/), [thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/), [HTTP timings](https://grafana.com/docs/k6/latest/using-k6/metrics/reference/), [graceful stop](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/graceful-stop/) | Published semantics and tested implementation behaviors. | Write our own implementation and independently validate it against synthetic ground truth and an external reference. **Not** a dependency, fork or source-code port. |
| Statistics handbook | [NIST/SEMATECH e-Handbook](https://www.nist.gov/programs-projects/nistsematech-engineering-statistics-handbook) | General experimental design and statistical interpretation techniques. | Predeclare confidence procedures; treat trial-to-trial dependence, nonstationarity and multiple comparisons explicitly. |

Research evidence supports **methods under assumptions**, not the assertion that Damocline's current code is scientifically certified.

## 2. Audit of today's master (pre-alpha)

| Implemented now | Gap / consequence |
| --- | --- |
| Native Go HTTP GET/HEAD and opt-in writes, guarded scenarios, concurrent VUs, fixed journey steps | No independently calibrated workload-delivery accuracy; dynamic values, session-level user modeling and safe mixed scenario traffic are limited |
| Open-arrival journeys and a rate limiter | `internal/engine/engine.go` uses `time.NewTicker`; late ticks can be coalesced or undercounted. No per-arrival planned-vs-actual timestamp, scheduler lateness distribution or generator saturation signal |
| Requests + journey p50/p95/p99 and failure counts | HTTP timing currently surrounds `client.Do` plus response read; it is **not** k6's `http_req_duration` (which excludes initial connect/TLS). No DNS/TCP/TLS/TTFB phase breakdown or per-observation timeseries. Cancelled boundary work excluded from completion percentiles |
| Closed-loop `explore` doubles VUs, refines bracket, repeats 2–4 times | Candidate stage 10 s–3 min, total budget at most 15 min; no steady-state/soak criterion, quantitative uncertainty, load generator health or holdout validation. A passing level is not a certified sustainable level |
| Optional Linux host CPU/RAM monitor with UTC correlation | No Docker/service metrics, DB waits/connections, Redis data, generator profiling, experiment/environment fingerprints or causal comparisons |
| Human-readable CLI report | Correctly refuses server-upgrade claims based on a single HTTP PASS, but cannot yet evaluate the customer's business capacity target |

Keep existing conservative ceilings (50 VUs, 100 RPS, maximum run duration) until the measurement and safety gates below justify a **separate, opt-in, tested** increase. Never raise production load caps as a documentation-only change.

## 3. Scientific validation gates: mandatory order

### G0 — Accurate and safe load generation (P0, blocks all capacity claims)

**Build:** extract `internal/scheduler` with monotonic logical scheduling, independent requested and actual timestamps, configurable schedules, a bounded queue policy and separate `planned`, `admitted`, `started`, `completed`, `rejected` and `dropped` counters; `scheduled = admitted + dropped` should hold for each phase, with definitions recorded. Distinguish scheduler/worker shortage, rate-governor suppression, app failure and operator stop. Record scheduling lag, actual concurrency, generator CPU/RAM, thread/goroutine counts, socket pool and measured generator overhead. Preserve fast cancellation and authorization safeguards.

**Measure:** design a reproducible `internal/fixtures` test server with deterministic configurable latency, queue size, status/errors, redirects, connection churn, body size and throughput saturation. Use loopback only. Build benchmarks at multiple rates, client machine sizes and warm/cold transports. Cross-check selected comparable metrics against k6/Vegeta in *development tooling only*. Document differences in metric definitions.

**Initial candidate quality gates** (engineering criteria, not published scientific constants): scheduled/accounted difference zero for every deterministic fixture; loss never hidden; observed offered load within ±5% on an **unsaturated, calibrated** generator for specified durations; percentiles correct for a fixed synthetic set; no material resource exhaustion at the advertised test ceiling. For overload, explain why target load could not be delivered instead of failing only the application.

**Evidence to retain:** fixture version, software hashes, machine OS/CPU, requested and achieved rate, timing error distributions, CPU/RAM, repeated trials, external reference output (no sensitive real targets). Tests must be deterministic or tolerance-bounded; record runner limits.

### G1 — Semantically correct metrics and SLO decisions (P0)

**Build:** `internal/metrics` using monotonic clocks and Go `httptrace`, with documented timing boundaries: end-to-end client latency, DNS, connect, TLS, time to first response byte, body reception, wait for rate tokens, queueing, request timeout, journey time **with/without think time**. Do not mislabel current measurements as `http_req_duration`. Add timestamped bounded time-series output with aggregation, error families, cancellation/timeout classification and safety redaction; design bounded-memory streaming histograms/quantiles with documented precision.

**SLOs:** per-workflow and business-operation error budgets, latency objectives for complete journeys, completion ratio, arrival-delivery completeness, percentile support only with adequate effective sample sizes, and stop conditions checked at defined intervals. Separate `FAIL` (measured SLO breach), `INCONCLUSIVE` (insufficient valid data/uncalibrated generator) and `PASS` (the measured stage meets a predeclared objective).

**Statistical sanity check:** With zero observed failures in `n` *independent* Bernoulli trials, a 95% one-sided upper bound on the error probability is `1 - 0.05^(1/n)`. About **299 independent observations** with zero failures are needed even to get the upper bound below 1%. HTTP requests from the same session and adjacent time windows are generally correlated, so this is **not** a sufficient pass rule in practice. Report the effective basis and avoid claiming 1% reliability from 20–50 requests.

**Acceptance:** golden vectors for boundaries/percentiles; timeouts and cancelled journeys accounted separately; SLO tests with deliberately failing operations; fuzz tests for parser/redaction; all result fields backward-compatible or schema-versioned.

### G2 — Workload characterization and representative actors (P1)

**Build:** `internal/workload` typed and versioned schema for weighted independent user groups, closed and open actors, think-time distributions grounded in observations, dynamic response-to-request extraction, per-user variables, isolation of distinct test accounts, CSRF/session refresh where explicitly authorized, per-action business assertions and read/write safety. Use synthetic data, idempotent fixtures, permitted cleanup steps and opt-in writes. A temporary browser login is **not** a many-user identity generator.

**Inputs:** actual peak-active-user estimates, arrival trace/histogram, business action proportions, read/write ratio, data-set size, hot/cold cache, geographic/network context and SLOs. Prefer private client logs or telemetry processed locally; offer documented assumptions when unavailable.

**Validation:** verify workload distributions and sequence statistics against a private reference trace or synthetic oracle. No production PII in public fixtures or PRs. TPC-W is an inspiration for data scale + transaction correctness, not a copy of its retired benchmark or publication of its protected specification.

**Output:** a versioned `WorkloadFingerprint` with target-specific elements stored **locally**, plus a public-safe anonymous scenario hash. Do not expose client URLs or exact paths in public examples.

### G3 — Repeatable sustained capacity experiments (P1/P2)

**Build:** `internal/experiment` orchestrates baseline, warm-up, steady state, controlled ramp, spike, recovery and soak tests; separates steady-state samples from warm-up and boundary; offers finite graceful-completion window and reports interrupted journeys. Stage plans may be longer/higher **only after G0 safety validation**, with explicit staging-first approval and abort controls.

**Statistical approach:** pre-register primary SLOs, comparisons and stopping rules. Use repeated independent trial runs; account for within-run correlation (e.g. block bootstrap or run-level uncertainty rather than naive bootstrap across adjacent HTTP requests). Report per-stage variation and uncertainty. Do not optimize the same model and declare it validated on the same observations; hold out repeated levels, including a run near the putative limit.

**Capacity claim gate:** stable target traffic delivery, no masked client bottleneck, representative sessions/workload, SLOs met through a sustained window, acceptable uncertainty, multiple independent passing trials, an explicitly tested limit or a declared upper search ceiling. If the highest tested stage passes, say *at least the highest validated stage under tested conditions*, **not** an estimated hard maximum. If a candidate fails, separate application degradation from client load-generation failure.

### G4 — Performance modeling and forecasting (P2, after G0–G3)

**Build:** `internal/model` that:
- Uses Little's Law solely as a consistency check over compatible, near-steady-state definitions (`L = λW`), and reports when assumptions fail.
- Fits USL throughput versus concurrent **active** workload with constraints and uncertainty, compares against simpler models and holds out measured levels for error evaluation.
- Computes client demand profiles (current peak, growth, seasonality, concurrent/idle split) and sensitivity scenarios without pretending to know future user behavior.
- Distinguishes `observed capacity`, `in-range interpolated estimate`, `out-of-range exploratory forecast` and `unknown` with uncertainty and no invented precision.

**Acceptance:** synthetic curves with known parameters, noise, phase changes, workload mix shifts and nonlinear resource saturation. Show failure to fit and conservative fallback to `INSUFFICIENT EVIDENCE`. No prediction is allowed as a verified user limit merely from a USL fit.

### G5 — Root-cause evidence and infrastructure alternatives (P2/P3)

**Build:** `internal/telemetry` permissioned opt-in adapters: generator first, Linux, Docker per service (CPU throttling, memory, restarts, networking), PostgreSQL metrics (connections, waits, pool, slow query summaries with no SQL literals/PII), Redis stats, application RED metrics and traces where already enabled. Align with experiment windows and document collector overhead. External HTTP-only use must remain fully functional with `HOST NOT OBSERVED`.

**Decision method:** correlate observed inflection points across HTTP, app, DB and host. Treat correlation as a hypothesis; reproduce with at least two controlled alternatives: same workload on a tuned build/index/configuration and, if appropriate, a larger resource configuration. Compare SLO compliance and cost per completed useful transaction, not just CPU utilization. If the bottleneck remains unexplained, recommend investigation rather than an upgrade.

### G6 — Evidence-based Capacity Planner & executive decision (P3/P4)

**Build:** `internal/planner` computes a *load target to validate*, then checks it against G3's validated capacity under equivalent workload assumptions. Store business goals separately from technical measurements and use an auditable rules engine, never an unqualified AI-generated verdict.

Inputs: peak **active** demand and observation source, action mix, growth horizon/scenarios, geographic scope, critical SLOs, planned safety reserve, uptime expectations, cost of current and candidate configurations.

**Decision taxonomy:**
- `MEETS_TARGET`: the business target **and requested reserve** fall within a validated passing workload at the required SLOs and evidence quality.
- `BELOW_TARGET`: a relevant failing workload is at or below the business target, with trustworthy load delivery; diagnose before recommending changes.
- `TEST_RANGE_TOO_LOW`: no sufficiently high stage tested to support or reject the customer goal.
- `UNKNOWN_OR_INCOMPARABLE`: missing generator calibration, sessions, representative mix, duration, environment, uncertainty or SLO data.
- `CHANGE_RECOMMENDED_FOR_EVALUATION`: a controlled alternative passed where the baseline failed, with measured cost/performance evidence. A larger VPS is only one alternative, not the default.

Example report statements (synthetic numbers only):
- “The specified workflow sustained 120 active modeled users for the declared observation window; demand at 100 equivalent active users was validated under this scenario.”
- “The proposed 250-user peak has not been tested with representative traffic; upgrade recommendation: insufficient evidence.”
- “At 180 modeled users, the primary journey breached the 1.5s p95 criterion; database wait time rose during the same period. This is a **hypothesis**, requiring controlled comparison before deciding whether to optimize queries or scale hardware.”

Deliver both machine-readable provenance and accessible CLI/HTML summaries. A private client report may contain the client's own target details; public repository fixtures remain generic.

## 4. Planned PR sequence (each PR independently reviewable)

| PR / priority | Main scope | Exact pass conditions | Depends on |
| --- | --- | --- | --- |
| P0-01: scheduler accounting | `internal/engine` → `internal/scheduler`; event counters/timestamps | Synthetic delayed ticker and saturated workers never hide planned starts; invariants and bounded lag statistics pass | None |
| P0-02: reference workload suite | Local configurable HTTP fixture, benchmark harness, generator self-monitoring, cross-tool methodology | Golden rate/latency/errors verified across supported platforms and repeated trials; no false application-limit labels | P0-01 |
| P0-03: timing and metric semantics | `httptrace` + metrics schema v3; SLO judgement, cancelled-completion accounting | Deterministic phase/percentile/SLO tests and redaction; unchanged files can be read under versioned schema | P0-01 |
| P1-01: workload actor schema | Weighted scenarios, isolated sessions, data extraction, real-world distributions | Stable synthetic population mix and safe account isolation; no unsafe writes by default | P0-03 |
| P1-02: campaign executor | Warm-up, steady, recovery, graceful finish, controlled stop | Correct windowing, truthful incomplete/failed journey accounting, finite limits | P0-02 / P1-01 |
| P2-01: statistics and stage confidence | Trial repeatability, effective sample/uncertainty, controlled comparison | Known synthetic stable/unstable examples classified appropriately; no unvalidated capacity claims | P1-02 |
| P2-02: modeling | Little consistency, measured throughput curve, cautious USL fit, holdout comparisons | Model rejects bad fits, extrapolation flagged, synthetic parameters/recovery within declared tolerance | P2-01 |
| P2-03: telemetry adapters | Generator/Linux/Docker/Postgres/Redis, optional collectors | Clock alignment, collection limits, private data redaction, offline replay, resource-pressure fixtures | P1-02 |
| P3-01: business targets | Workload demand profile + growth/reserve + equivalence checks | All five taxonomy states correct on fixtures; target calculation never represented as measured capacity | P2-01 / P2-02 |
| P3-02: alternatives & report | Compare baseline, optimization and bigger machine; executive+technical evidence | Recommendations trace to run IDs/SLOs/cost; missing evidence yields “unknown” and no auto-upgrade | P2-03 / P3-01 |

PR numbers above are **planned identifiers, not existing GitHub PRs**. Implementation can be broken into smaller submissions without changing dependency gates.

## 5. Operational validation campaign and public evidence

1. CI unit/property tests for scheduling/accounting, percentiles, redaction and state transitions (Linux and Windows).
2. Deterministic local HTTP fixtures: instant, fixed delay, mixed long-tail, timeouts, failures, redirects, large bodies, connection churn, bounded server concurrency and overload recovery.
3. Independent generators and reference-tool A/B on **local fixtures only**; same planned arrivals, status assertions, response-size and HTTP metric semantics.
4. Warm-up/steady/soak/recovery campaigns in an **isolated, authorized staging** environment with telemetry. Use one factor at a time for controlled CPU/memory/database investigations.
5. Publish a sanitized `docs/VALIDATION_RESULTS.md` with environment fingerprints, methodology, observed errors, tool version, confidence method and known failures. No live customer domains or traffic reports.
6. Run regression benchmarks on major engine changes; block a release if G0 or G1 previously demonstrated invariants regress.

## 6. Security, licensing and project boundary

- Damocline is Apache-2.0. The [grafana/k6 repository](https://github.com/grafana/k6) is AGPL-3.0. Study **concepts**, documentation and public test behavior; do **not** copy source, executable subcomponents, tables or protected documentation into Damocline without explicit compatibility/licensing review. Legal advice may be needed for proposed reuse.
- Runtime remains a native Go load engine. k6/Vegeta may be invoked by an optional **developer-only benchmark comparison harness**, never as a hidden product dependency.
- No public repository customer URLs, credentials, internal endpoints, tenant IDs, private screenshots, test session cookies or private benchmark datasets.
- Default to safe low-load tests, authorization, bounded workload and local telemetry. Stress, sustained, writes or large generator capabilities need explicit acknowledgement of risk and abort criteria.
- Do not equate passing unit tests, mathematical plausibility or matching k6 output with a certified customer-capacity claim.

## 7. Product release gates

- **Research-backed experimental:** cites assumptions and can demonstrate correctness on controlled fixtures; may say “observed at this level,” not “supports your target.”
- **Measurement validated:** G0 and G1 passed and published with reproducible evidence; suitable for qualified workload observations.
- **Workload-qualified capacity:** G2 and G3 passed for this workload and environment, with confidence and observed stable ceiling; suitable for carefully delimited capacity statements.
- **Planning-qualified recommendation:** G4–G6 completed, telemetry/alternatives when relevant, and run evidence tied to business target; suitable for defensible advice, not guarantees of future load or future cost.

**First next implementation:** P0-01 + P0-02. Without correct offered-load accounting and a calibrated generator, no statistical or business-facing capacity model can be trusted.
