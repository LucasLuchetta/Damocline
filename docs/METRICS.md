# Measurement model

## Vocabulary

| Term | Meaning |
| --- | --- |
| Connected session | Authenticated or transport session; may be idle. |
| Virtual user (VU) | Isolated synthetic actor with its own state and journey. |
| Active virtual user | VU currently executing a defined workflow, including its modeled think times. |
| In-flight request | Started but not yet completed, failed, or timed out. |
| Arrival rate | New request or iteration starts per second, not a number of users. |
| Throughput | Completed operations per second; success and failures reported separately. |
| Response time | Measured duration from a defined boundary to completion; protocol breakdown when available. |
| Journey time | Total elapsed time for specified business steps, including the configured timing rules. |
| Sustainable capacity | A claim reserved for a workload level that passes a defined sustained protocol with adequate measurement quality and uncertainty; the current short-run explorer does not establish this. |

Do not describe a request rate, open TCP connection count, or total logins as "simultaneous users" without qualifications. A real browser page includes client rendering and third-party resource work that HTTP-only simulations may not measure.

## Workload types

**Closed-loop VUs:** actors complete a journey, optionally pause, and repeat. When the target slows down, the operation arrival rate typically drops. This models a fixed population whose next journey depends on prior completion.

**Open-loop journey arrivals:** planned journey starts are independent of previous response completion. Damocline reports scheduled, started, completed, dropped-late and dropped-busy arrivals, plus scheduler lag. A dropped or delayed start means offered demand was not fully delivered; a worker-busy drop is not evidence that the target itself saturated. Client-side limits can invalidate the intended load, so inspect generator observations too.

**Weighted profiles:** a scenario can define two to ten named workflows, each with a positive integer weight, steps, and optional think time. Closed-loop VUs are allocated deterministically across profiles; arrival mode selects each planned journey with smooth weighted round-robin. The report records configured weights, assigned closed-loop VUs, and actual started/completed/cancelled journeys. A weight is an intended demand mix, not evidence that the mix represents production use; derive it from sanitized usage evidence and review the observed counts.

**Dependent steps:** a step may extract a string, number, or Boolean through a JSON Pointer, or a bounded response-header value. Later steps in the same journey may use it in complete path segments, headers, or `body_env` content. Extracted values are kept only in the VU's current journey and are never written to the report. Extraction failure makes the step fail; it is not silently replaced with a prior value.

A **partly open session model**—independent session arrivals followed by dependent requests and think times within each session—is not implemented. Open-loop journey arrivals and closed-loop VUs answer different questions and are not interchangeable.

## Statistical interpretation and generator health

The error-rate assessment includes a one-sided 95% upper bound. With zero observed errors it uses an exact binomial bound; with observed errors it uses a Wilson upper score bound. Both treat outcomes as independent, stationary Bernoulli trials. Real requests may be correlated within a VU, time window, or deployment, so the bound is conditional evidence rather than a reliability guarantee.

Reported p50/p95/p99 values are point estimates. Counts accompany them, but the report does not calculate percentile confidence intervals or dependence-aware run-to-run uncertainty. A low sample count or correlated samples can make tail estimates especially unstable.

When `window_duration` is configured, the report also contains request metrics for each completed observation window. Every non-empty window is checked against the configured request p95 and error-rate SLOs. A window with fewer completed requests than `min_samples_per_step` is marked inconclusive. This is a temporal guard against a good aggregate hiding a late degradation; it is not a percentile confidence interval or proof of stationarity.

Completed phased campaigns summarize the observed minimum and maximum observation p95, error rate, and throughput across independent trials. This describes run-to-run spread and makes instability visible; it is not a confidence interval, bootstrap result, or dependence-aware statistical guarantee.

Run reports also sample average in-flight HTTP requests over the workload window and compute throughput multiplied by mean request time. The two values support a bounded Little's Law consistency check only when they share the same request boundary and observation window. They are not a queueing model, a capacity prediction, or evidence about browser work.

Generator health samples Go heap allocation, goroutines, garbage-collection activity, Go runtime CPU occupancy, and (on Windows/Linux) process RSS and CPU time. HTTP run report schema v5 includes these process fields and separates `observation_seconds` (the configured workload window) from `elapsed_seconds` (including the bounded drain period for in-flight work). Throughput uses the observation window; time-series correlation uses the full elapsed interval. Process CPU is sampled between intervals and normalized to GOMAXPROCS; it is not host utilization, and RSS is not total host memory. Sampling may miss brief peaks. These metrics describe the load generator, not the application under test. Current 90% runtime/process CPU guards and request-rate-ceiling checks are provisional safeguards, not calibrated universal saturation thresholds.

Arrival accounting and scheduler lag are also measurement-quality signals. Any unexplained reconciliation mismatch invalidates interpretation. A late scheduler start indicates generator timing error; a busy-worker drop indicates configured worker capacity prevented delivery. Neither should silently be counted as target capacity.

## What is not yet measured

- No predeclared long soak protocol for checking whether SLOs held throughout a production-representative steady state.
- No uncertainty interval for latency percentiles or dependence-aware variability analysis across independent runs.
- No latency estimate for incomplete journeys; they are counted as cancelled, and an otherwise passing run becomes inconclusive. Journey percentiles describe completed journeys only.
- No fitted queueing or Universal Scalability Law model. Do not extrapolate beyond tested levels.
- No partly open session arrivals, synthetic-data lifecycle controls, or production-derived validation of workload weights.

Closed-loop and open-loop models answer different questions and should be selected from the intended demand process.

## Minimum report contents

- Authorized target identifiers and sanitized scenario revision.
- Environment assumptions (machine size, application version, data size, deployment topology where known).
- Test duration and configured ramp-up. Campaign reports additionally preserve warm-up, observation, optional spike, and recovery phases; their bounded duration is not a substitute for a predeclared long soak protocol.
- Requested and achieved load; scheduled, started, completed, and dropped work.
- Request and journey p50/p95/p99 latency, counts, error rates, and timeouts.
- Criteria for passing, including latency and business-operation error budgets.
- Generator health and limitations, including resource use and timing accuracy.
- Repetition count and the range actually tested. Campaign reports provide observed min/max spread across observation trials, not run-to-run variance, a confidence interval, or a dependence-aware analysis.
- Telemetry correlation only when the relevant evidence was collected.

Never assert a precise bottleneck if the available evidence only suggests one.

## Discovering a passing level

1. Define a journey/workload mix, observation duration, and acceptance thresholds.
2. Measure a baseline; verify generator health.
3. Increase load in bounded steps until a criterion fails or the configured upper ceiling is reached.
4. Refine between passing and failing levels.
5. Repeat candidate levels to capture variability and stability.
6. Report the highest repeatedly observed passing tested level and first observed failing level (if one exists), with explicit conditions. Current short runs do not certify sustainable capacity.

The result must not be marketed as the absolute number of concurrent people who can ever log in. A capacity claim requires sustained performance evidence for a specific workload under specific conditions; current explorer results are observations from short trials.

## Mixed workloads

A result for "Dashboard" at 150 VUs and one for "Search" at 100 VUs **cannot be summed** into a 250-user system capacity. Shared CPU, database, network, and external services can change both results. System-level estimates require a stated workload distribution and dedicated mixed tests.

## Illustrative example only

> Scenario: 60% search journeys and 40% approval journeys. Criteria: p95 below 1.5 s, business errors below 1%, 15-minute steady-state window. The 80-VU level passed repeated tests; the 100-VU level breached the p95 criterion. This is not an estimate for idle sessions or a different infrastructure configuration.

No numbers above are actual Damocline measurements.
