# k6 engineering practices to independently validate in Damocline

This is a feature and **test-method** gap map based on the public k6 code and project documentation. It does **not** claim k6 has independently or scientifically certified every behavior. Damocline uses its own Go engine; no k6 runtime dependency is intended.

Reference implementation: https://github.com/grafana/k6 (reviewed at a public `master` snapshot).
License notice: k6's repository contains an **AGPL-3.0** license at https://github.com/grafana/k6/blob/master/LICENSE.md ; Damocline currently uses Apache-2.0. **Learn from behavior/specification and build original code and tests; do not transplant AGPL implementation without a specific license review.** Avoid lifting tests verbatim; write equivalent independent fixtures and assertions.

## Evidence-to-work table

| Feature/test contract visible upstream | Upstream reference | Current Damocline status | Independent implementation and acceptance criteria | Phase |
| --- | --- | --- | --- | --- |
| Closed, constant-VU scheduling | [constant_vus_test.go](https://github.com/grafana/k6/blob/master/lib/executor/constant_vus_test.go) | Basic closed loop exists | Verify exact VU lifetime, ramp timing, think-time handling, complete and cancelled iterations under reproducible simulated clocks. | P0 |
| Independent rate and timing | [constant_arrival_rate_test.go](https://github.com/grafana/k6/blob/master/lib/executor/constant_arrival_rate_test.go) | Arrival scheduler exists but `time.NewTicker` can coalesce ticks; not all planned timestamps are recorded | Timestamp the theoretical schedule separately from dispatcher wakeups; detect late, lost and dropped arrivals; test generator pause/starvation and clock drift using deterministic scheduling. | P0 |
| Ramped arrival rate and slow-rate edge cases | [ramping_arrival_rate_test.go](https://github.com/grafana/k6/blob/master/lib/executor/ramping_arrival_rate_test.go) | Not an independent arrival ramp | Implement bounded rate stages with exact cumulative planned starts; validate stage transitions and offered/achieved rates under saturation. | P0–P1 |
| Graceful stop, ramp down, and started-work accounting | [ramping_vus_test.go](https://github.com/grafana/k6/blob/master/lib/executor/ramping_vus_test.go) | Fixed duration cancels requests and excludes cancelled samples from duration percentiles | Separate admission cutoff from drain timeout; report completed, failed, timed out, dropped and incomplete journeys; test both hard-stop and graceful drain. | P0 |
| Per-VU state, cancellation and race behavior | [vu_handle_test.go](https://github.com/grafana/k6/blob/master/lib/executor/vu_handle_test.go) | Independent cookie jars optional; `-race` exists | Validate complete VU state isolation, cookies and dynamic variables, concurrent cancellation, connection reuse and no secret leakage. | P0–P1 |
| Fixed iteration workloads and incomplete work | [per_vu_iterations_test.go](https://github.com/grafana/k6/blob/master/lib/executor/per_vu_iterations_test.go), [shared_iterations_test.go](https://github.com/grafana/k6/blob/master/lib/executor/shared_iterations_test.go) | Iterations per scenario only counted | Support repeatable per-VU and shared-work iteration tests for calibration, while disclosing these are not representative traffic unless justified. | P1 |
| HTTP timing phase definitions | [k6 HTTP metrics](https://grafana.com/docs/k6/latest/using-k6/metrics/reference/) | One end-to-end request timer; not identical to k6 `http_req_duration` | Distinguish DNS, connection, TLS, send, TTFB, receive; publish exact semantics and compare only equivalent latency boundaries. | P0 |
| Threshold and journey assertions | [k6 thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/) | Single global/per-step p95 and error budget; string assertion | Per-journey SLOs, p99, minimum samples, business correctness checks, bounded error-rate uncertainty, validity gates. | P0–P1 |
| Multiple independent scenarios | [k6 scenarios](https://grafana.com/docs/k6/latest/using-k6/scenarios/) | One sequential journey; no weighted groups | Represent application user behavior with independent activity groups, mix, distributions, setup/teardown and read/write data hygiene. | P1 |
| Output and performance diagnosis | [k6 metrics and outputs](https://grafana.com/docs/k6/latest/using-k6/metrics/) | Aggregate JSON, simple host CPU/RAM, limited correlation | Periodic measurements and resource utilization of generator and server, immutable run manifest, repeatability data; trace integration later. | P1–P2 |

## Scientific and industrial anchors

Read [Research foundations](RESEARCH_FOUNDATIONS.md) for publications that justify **why** these contracts matter: Little's Law, open/closed workload research, statistically designed experiments, realistic transactional benchmarks, queueing, tail latencies and capacity planning.

## Benchmark validation matrix (P0 issue #5)

| Controlled fixture | Reference truth | Damocline must show |
| --- | --- | --- |
| Constant response latency | Calibrated local deterministic service time | Request timing within established tolerance, allowing measured host/network overhead. |
| Poisson / fixed rate arrival schedule | Intended timestamp sequence produced independently of target | Scheduled starts, admission delay, actual starts and drops reconcile; no silent ticker omissions. |
| Slow target with limited workers | Known service bottleneck and worker limit | Worker saturation, timeouts, queueing and undelegated arrivals, not falsely increasing sustainable capacity. |
| Sudden pause of generator | Synthetic scheduler stall | Explicit scheduler lag and missing starts, yielding INCONCLUSIVE where offered load was not delivered. |
| Early cancellation and graceful stop | Fixed admission cutoff and drain budget | Honest completed/failed/incomplete accounting and latency treatment. |
| Multiple identities with dynamic state | Controlled fixture returning per-user tokens | Isolation across VUs and faithful state transitions. |
| Saturated generator | Intentionally constrained generator CPU/network | Self-identified client bottleneck, refusal to claim server ceiling. |
| Different database and application bottlenecks | Artificial resource-constrained services with telemetry | Correlation identifies observations and hypotheses, avoids asserting unobserved root causes. |
| Repeated, noisy campaigns | Seeded reference service with variability and outliers | Stable intervals/uncertainty under stated assumptions; catches tail and SLO violations. |

## Executive output: what the scientific pieces unlock

Report four distinct answers, never conflating them:

- **Meets objective?** Measured defined workload against explicit business/SLO targets, with uncertainty.
- **Capacity demonstrated?** Highest stable **tested and passing workload** with evidence and an observed upper bound if reached.
- **Will future demand fit?** Compare a calibrated demand model against that measured region, with safety reserve and forecast uncertainty.
- **Need a larger server?** Only when repeatable telemetry + controlled alternatives justify resizing; otherwise specify investigation or optimization.

No fictitious target domains, private reports, secret cookies, or production account data belong in this repository.
