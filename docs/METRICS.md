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

**Weighted profiles:** a scenario can define two to ten named workflows, each with a positive integer weight, steps, and optional think time. Closed-loop VUs are allocated deterministically across profiles; arrival mode selects each planned journey with smooth weighted round-robin. The report records configured weights, assigned closed-loop VUs, and actual started/completed/cancelled journeys. `profile_mix_basis` records `assumption` (the default), `sanitized_analytics`, or `business_estimate`; non-assumption bases require a safe short `profile_mix_reference` identifier. These are user declarations only: they do not validate the source, freshness, or representativeness of the proportions. Reports never accept raw event data in these fields.

**Dependent steps:** a step may extract a string, number, or Boolean through a JSON Pointer, or a bounded response-header value. Later steps in the same journey may use it in complete path segments, headers, or `body_env` content. Extracted values are kept only in the VU's current journey and are never written to the report. Extraction failure makes the step fail; it is not silently replaced with a prior value.

A **partly open session model**—independent session arrivals followed by dependent requests and think times within each session—is not implemented. Open-loop journey arrivals and closed-loop VUs answer different questions and are not interchangeable.

## Evidence-aware bottleneck signals (G0-05 partial)

`diagnostic_signals` identifies observable symptoms separately from causal conclusions. A client-side `max_rps` ceiling, missed journey starts, exhausted synthetic workers or high sampled generator CPU are not evidence of server saturation. `HTTP_ERROR_RESPONSE` means an observed 4xx/5xx status, not proof that CPU, a database or connection pool caused the failure. Missing telemetry is flagged explicitly; where no supported signal exists, `BOTTLENECK_NOT_ESTABLISHED` indicates that root cause is unknown. Full connection-pool and infrastructure-level attribution remains unimplemented.

## Measurement data quality (initial G0-16 slice)

New run reports include `data_quality.state` and a bounded `data_quality.findings` list. The state is distinct from `outcome` (observed request/step SLO PASS/FAIL/INCONCLUSIVE). Arithmetic contradictions between scheduled/started/dropped work, negative counts, impossible request or journey totals, or invalid durations are `INVALID`. Missing generator process samples, dropped/cancelled work, client-rate saturation and the absence of workload-equivalent preflight evidence are described as `LIMITED`. Invalid accounting cannot remain an observed PASS.

The `VALID_WITHIN_ENVELOPE` enum is **reserved** until an independently validated, equivalent generator envelope can be linked to the tested application workload. This stage does not emit that state; a run can legitimately have an SLO `PASS` and measurement evidence `LIMITED` simultaneously. Data quality findings never embed requested URLs, secrets, step names, or response content. Previously recorded reports may lack the optional quality field; missing is **not** equivalent to validated.

## Statistical interpretation and generator health

The error-rate assessment includes a one-sided 95% upper bound. With zero observed errors it uses an exact binomial bound; with observed errors it uses a Wilson upper score bound. Both treat outcomes as independent, stationary Bernoulli trials. Real requests may be correlated within a VU, time window, or deployment, so the bound is conditional evidence rather than a reliability guarantee.

Reported p50/p95/p99 values are point estimates. With enough samples (at least ten expected observations on either side of the requested percentile), the report also includes a normal-approximation 95% interval from the P95 order-statistic ranks. That interval assumes independent, stationary latency observations and is omitted when the tail sample is too small. It is not dependence-aware and does not replace time-separated trials. A low sample count or correlated samples can make tail estimates especially unstable.

When `window_duration` is configured, the report also contains request metrics for each completed observation window. Every non-empty window is checked against the configured request p95 and error-rate SLOs. A window with fewer completed requests than `min_samples_per_step` is marked inconclusive. This is a temporal guard against a good aggregate hiding a late degradation; it is not a percentile confidence interval or proof of stationarity.

Completed phased campaigns summarize the observed minimum and maximum observation p95, error rate, and throughput across independent trials. This describes run-to-run spread and makes instability visible; it is not a confidence interval, bootstrap result, or dependence-aware statistical guarantee.

The offline `plan` demand assessment requires adequate conditional statistics before it can say `GOAL_MET_UNDER_TESTED_CONDITIONS`: the one-sided 95% binomial upper error bound and the existing approximate two-sided 95% P95 rank interval must fit the configured objectives for each required phase aggregate, named step, and every completed SLO window. It also requires the windows to cover the full observation horizon and descriptive spread to be present for every independent observation trial. Before accepting `VALID_WITHIN_ENVELOPE`, it recomputes each required phase's quality from the saved run fields and rejects a persisted state that disagrees; current run reports cannot recompute as valid because workload-equivalent calibration is not yet available. For a 1% error budget, zero failures among 50 requests still leaves a one-sided upper bound above 1%, so it is insufficient. These checks assume independent, stationary request failures and latency samples; the assessment states that dependence and stationarity were not established. They are not a dependence-aware confidence analysis or a capacity certificate. Missing or undersampled evidence remains `INSUFFICIENT_EVIDENCE`.

Run reports also sample average in-flight HTTP requests over the workload window and compute throughput multiplied by mean request time. The two values support a bounded Little's Law consistency check only when they share the same request boundary and observation window. They are not a queueing model, a capacity prediction, or evidence about browser work.

Generator health samples Go heap allocation, goroutines, garbage-collection activity, Go runtime CPU occupancy, and (on Windows/Linux) process RSS and CPU time. Schema-v10 run reports also record configured sampler interval, maximum observed sample gap, and total/maximum wall time spent collecting each snapshot. Process CPU is normalized by recorded GOMAXPROCS but includes OS process CPU time; values above 100% are not inherently invalid because this is not Go scheduler P occupancy. At or above 90%, generator headroom is LIMITED. Schema-v11 adds a SHA-256 fingerprint of declared workflow and request-shape fields for strict offline comparison. It includes only the byte length of an environment-backed body, not the body content; it does not contain resolved credentials, extracted values or generated synthetic identifiers. It therefore does not prove equality of those runtime values. Collection durations are wall-time proxies, not direct CPU-overhead measurements. A gap over two configured intervals makes evidence LIMITED; negative, non-finite or internally inconsistent sampler fields make it INVALID. Missing interval/sample coverage or a reported process sample with zero peak RSS remains LIMITED, as does a zero runtime heap/goroutine peak. These checks describe sample quality, not target health. Reports before schema v7 do not include HTTP phase evidence; reports before schema v8 do not include structured HTTP outcome diagnostics or observed think-time totals; reports before schema v10 do not include sampler cadence/collection fields; reports before schema v11 do not include workload fingerprints and cannot be treated as controlled workload comparisons. Treat absent fields as unknown.

Arrival accounting and scheduler lag are also measurement-quality signals. For open-arrival runs, the report records one monotonic planned-slot-to-admission lag sample per planned start, with p50, p95, maximum, sample count, and timing method. The lag includes timer wake-up and callback admission overhead; it does not separate OS timer delay from scheduler work, measure request/network latency, or establish clock accuracy. No validated accuracy tolerance is currently claimed, so these values are diagnostic evidence only. Any unexplained reconciliation mismatch invalidates interpretation. A late scheduler start indicates generator timing error; a busy-worker drop indicates configured worker capacity prevented delivery. Neither should silently be counted as target capacity.

The local reference fixture maintains an independent request ledger. Tests compare client-reported cancellations with server-observed cancellations and reconcile received requests as completed, cancelled, or rejected. A separate drain test verifies all six admitted requests complete at the server after scheduling ends; another cancels six in-flight requests after the observation window has ended but before the drain timeout. These checks validate controlled loopback success, cancellation, and external-abort-during-drain paths; they do not prove ledger coverage for arbitrary handlers or cancellation caused by the natural drain deadline.

## HTTP client timing boundaries (G0-12 partial)

Each named step reports additive client-observed phase totals in `http_trace`. `rate_wait_ms` runs from the start of that step's rate-token wait until the token is admitted, so it is outside HTTP request latency. `request_attempt_count` counts calls that reached `http.Client.Do`; `complete_count` counts their returned transport/body outcomes and must reconcile with attempts. `dns_ms`, `connect_ms`, and `tls_ms` sum completed attempts seen through Go `httptrace`; their counts indicate how many attempts were observed. Start counts are also reported so interrupted callbacks remain visible. A zero count means no such attempt was observed (commonly due to a reused connection), not a zero-duration phase. Failed attempts are included when their completion callback fires; an attempt interrupted before its callback is not assigned an invented duration. A response whose body read fails after first byte increments `body_incomplete_count`, preserving the distinction between a complete body and an interrupted receive.

The schema-v8 `http_diagnostics` block records exact response status codes and their 1xx–5xx family counts, redirects not followed, assertion failures, timeouts, TLS/transport failures, and oversized response bodies. `rate_limited_responses` counts HTTP 429 responses received from the application. The local `max_rps` governor delays admitted requests and reports its wait separately; it does not suppress those requests. Consequently these 429 observations are not a count of local rate limiting. Status and failure counters are observations, not cause attribution.

`first_byte_ms` sums time from immediately before `http.Client.Do` to the first response byte callback. It includes client scheduling, connection work, network transit and target response delay; it is not server processing time. `body_ms` sums time from first response byte to the end of the bounded body read. `complete_ms` sums full client-observed request duration from immediately before `Do` through body read completion, or through a transport error. Counts distinguish phase observations from requests. These totals are diagnostic sums, not percentiles and not additive disjoint slices: DNS/connect/TLS overlap may occur and first-byte already includes earlier phases. Request queue waiting internal to `net/http.Transport` is not exposed by this API; `request_write_ms` records time until `WroteRequest` and must not be interpreted as a pure queue duration. Clock/trace callback accuracy has not been calibrated under host load.

Completed journey latency includes rate-token waits between steps, network/response time for each step, and configured think-time intervals. It is reported independently from per-step request latency. `observed_think_time_ms` and `think_time_observations` sum wall time spent in configured between-step sleeps; an interrupted sleep is recorded as a partial interval. They do not represent user think time outside the modeled workflow. Cancelled journeys increment the cancellation count and their partial elapsed duration is excluded from completed journey percentiles.

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
- For open-arrival workloads, scheduler lag sample count, p50/p95/maximum, and timing method; these are not a calibrated clock-accuracy guarantee.
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
