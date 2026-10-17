# Research foundations for evidence-based capacity assessment

Status: research and implementation plan, **not** a claim that Damocline has scientifically validated capacity predictions.

## What decision are we engineering?

A system owner needs to know whether a defined application version and infrastructure can support a specified *business demand* (not just a synthetic user count), how much load has actually been demonstrated, where degradation begins, and what evidence supports optimizing software versus changing infrastructure.

The evidence chain is:

1. Translate business demand into measured workloads (arrival patterns, sessions, workflow mixes, think times and data size).
2. Reproduce the workload with a calibrated, independent load generator.
3. Measure response distributions, errors, incomplete work, offered and delivered load, plus generator health.
4. Observe server, application, database, queue and infrastructure constraints with synchronized timestamps.
5. Repeat sustained trials and quantify uncertainty; validate any predictive model against withheld load points.
6. Compare the measured target demand with demonstrated capacity and evaluate infrastructure/software alternatives. Label unsupported conclusions **INCONCLUSIVE**.

## Academic foundations and the exact role each may play

| Foundation | Evidence and source | Proposed application | Essential limitation |
| --- | --- | --- | --- |
| Little's Law | J. D. C. Little, *A Proof for the Queuing Formula: L = λW*, Operations Research, 1961. https://doi.org/10.1287/opre.9.3.383 | Relate average work in the system, observed completed throughput and mean time in the system; cross-check telemetry under stable conditions. | L counts work in a defined system, **not** logged-in people. Requires consistent boundaries and long-run stable averages. Do not use it as a universal capacity predictor. |
| Capacity planning methodology | V. Almeida & D. A. Menascé, *Capacity Planning: An Essential Tool for Managing Web Services*, IEEE IT Professional, 2002. https://doi.org/10.1109/MITP.2002.1046642 ; V. Almeida, *Capacity Planning for Web Services*, 2002. https://doi.org/10.1007/3-540-45798-4_7 | Business demand → workload characterization → system/model calibration → performance/cost alternatives → actionable recommendations. | A method must be calibrated with deployment-specific observations, not generic hardware multipliers. |
| Open vs closed loads | B. Schroeder, A. Wierman & M. Harchol-Balter, *Open Versus Closed: A Cautionary Tale*, NSDI, 2006. https://www.usenix.org/conference/nsdi-06/open-versus-closed-cautionary-tale | Select appropriate models; run both independent-arrival and interactive closed-VU tests; track planned versus delivered arrivals. | A closed-loop workload can self-throttle while the application slows. Neither model alone is representative of every product. |
| Scalability curve | N. J. Gunther, *A General Theory of Computational Scalability Based on Rational Functions*, 2008. https://arxiv.org/abs/0808.1431 | Fit USL to throughput versus offered concurrency to explore contention and coherence; label projections and compare with withheld observations. | Fitted extrapolation is not proof of physical capacity. Reject poor fits and regimes dominated by client rate limits. |
| Tail latency | J. Dean & L. A. Barroso, *The Tail at Scale*, Communications of the ACM, 2013. https://research.google/pubs/the-tail-at-scale/ | Add per-operation and end-to-end journey p95/p99/tail analysis, with explicit sample counts and uncertainty. | Do not report an apparently accurate p99 from a handful of requests; synchronous multi-service paths need additional tracing. |
| Statistical experimental design | A. Georges, D. Buytaert & L. Eeckhout, *Statistically Rigorous Java Performance Evaluation*, OOPSLA, 2007. https://biblio.ugent.be/publication/417084 | Repeated independent trials, warm-up, explicit sources of variance, confidence intervals and reproducible configurations. | Its experiments concern Java benchmarks; apply experimental principles, not unverified Java-specific assumptions to HTTP workloads. |
| Realistic sessions | D. A. Menascé, *TPC-W: A Benchmark for E-Commerce*, IEEE Internet Computing, 2002. https://doi.org/10.1109/MIC.2002.1003136 ; TPC-W archive https://www.tpc.org/tpcw/ | Mixed weighted transactions, user think times, seeded data, authentication, read-write workflows and integrity assertions. | The official TPC-W benchmark is obsolete; emulate its modeling principles, not its bookstore load ratios. |
| Queueing-system models | M. Harchol-Balter, *Performance Modeling and Design of Computer Systems*, Cambridge University Press, 2013. https://www.cs.cmu.edu/~harchol/PerformanceModeling/book.html | Optional model library for queueing, bottleneck sensitivity and high-variance job sizes. | Models need appropriate queueing assumptions and measured service demand. |
| Service operations (industry evidence, **not** an academic proof) | Google SRE, *Service Best Practices*. https://sre.google/sre-book/service-best-practices/ | Forecast-and-measure cycles, resilience to outages, capacity margin, and regular load tests after software changes. | Operational recommendations depend on system architecture; they cannot certify a given app. |
| Coordinated omission (engineering caution, **not** a peer-reviewed theorem cited here) | Gil Tene's practitioner discussion https://groups.google.com/g/mechanical-sympathy/c/icNZJejUHfE/m/BfDekfBEs_sJ | Preserve intended arrival timestamps, measure schedule lateness, and count missed arrivals; separate queueing delay from actual HTTP execution. | Avoid claiming histogram correction can reconstruct all unobserved requests. |

## Statistical validity example

Suppose a run completes 50 independent requests with zero errors. The observed failure fraction is 0%, but the one-sided 95% binomial upper confidence bound is `1 - 0.05^(1/50) ≈ 5.82%`. **That sample does not establish a <1% underlying failure probability at 95% confidence.** About 299 zero-failure independent observations would be required for that specific bound to fall below 1%. Real requests can be correlated and nonstationary, in which case naïve binomial inference may be too optimistic.

The tool should report both empirical failure rate and uncertainty, including explicit sampling assumptions. Percentile uncertainty needs an appropriate approach with enough samples and temporal dependence considered. No universal fixed test duration magically makes a result scientifically valid.

## Defensible evaluation lifecycle

1. **Plan**: owner-specified peak business goals, deployment identity, environment and workload equivalence, SLOs, required safety reserve.
2. **Calibrate generator**: known-latency/known-capacity local fixtures, timestamp fidelity, scheduler lag, CPU/memory/network/connection pressure and control-tool comparisons.
3. **Baseline and warm-up**: explicitly separate warm-up observations, avoid counting first-time startup/cold-cache effects as steady state without labeling them.
4. **Progressive probes**: independent rate and closed-VU tests; multiple workload profiles; stop on unacceptable errors or resource safety triggers.
5. **Sustain and repeat**: independently repeat candidate loads over periods justified by workload dynamics, including warm-up, steady-state and recovery phases; measure tail and time-window violations.
6. **Correlate**: host + container + application traces + DB pool/locks/query metrics; identify a bottleneck only when supported by evidence, not CPU proximity alone.
7. **Compare alternatives**: unchanged representative workload and environment factors controlled, e.g. new index vs additional CPU; rerun the same campaigns.
8. **Report**: objective demand, demonstrated passing range, first observed failure (if any), confidence/limitations, recommendation, and upgrade trigger conditions.

**Important distinction**: throughput in requests/s, concurrently executing requests, interactive synthetic VUs, independently authenticated users, active human users, and registered accounts are not interchangeable units.

## Recommended acceptance gate before an executive-facing capacity claim

- An externally repeatable synthetic fixture suite exercises saturation, connection limits, slow requests, request cancellation, sustained overload, recovery, long-tail delays and generator CPU starvation.
- All scheduled arrivals and late/missed starts are accounted for; independent scheduling is validated at load.
- Generator has measured headroom. A generator-limited result yields INCONCLUSIVE, not a server capacity ceiling.
- Business journey mix and dataset sizes are recorded and representative; identities are genuine or the limitations are explicit.
- Measured service-level limits have uncertainty estimates and multiple stable replications, including incomplete work.
- Long-running tests and regression comparisons include environmental revisions and synchronized supporting infrastructure metrics.
- Model-based forecasts are labeled estimates and cross-validated; no upgrade or no-upgrade declaration is inferred from one short test.

This document is a research map, not third-party code. Original implementations must respect third-party software licenses.
