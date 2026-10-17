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
| Sustainable capacity | Highest validated passing workload level for *this* scenario, environment, observation period, and pass/fail criteria. |

Do not describe a request rate, open TCP connection count, or total logins as "simultaneous users" without qualifications. A real browser page includes client rendering and third-party resource work that HTTP-only simulations may not measure.

## Workload types

**Closed-loop VUs:** actors complete a journey, optionally pause, and repeat. When the target slows down, the operation arrival rate typically drops.

**Open-loop arrivals:** the planned start times are independent of previous response completion. The implementation must expose dropped or delayed starts and warn when client resource limits invalidate the intended load. This helps avoid *coordinated omission*.

Both are essential and answer different questions.

## Minimum report contents

- Authorized target identifiers and sanitized scenario revision.
- Environment assumptions (machine size, application version, data size, deployment topology where known).
- Warm-up, observation, and cool-down windows.
- Requested and achieved load; scheduled, started, completed, and dropped work.
- Request and journey p50/p95/p99 latency, counts, error rates, and timeouts.
- Criteria for passing, including latency and business-operation error budgets.
- Generator health and limitations, including resource use and timing accuracy.
- Repetition count, variance or confidence, and the range actually tested.
- Telemetry correlation only when the relevant evidence was collected.

Never assert a precise bottleneck if the available evidence only suggests one.

## Discovering a passing level

1. Define a journey/workload mix, observation duration, and acceptance thresholds.
2. Measure a baseline; verify generator health.
3. Increase load in bounded steps until a criterion fails or the configured upper ceiling is reached.
4. Refine between passing and failing levels.
5. Repeat candidate levels to capture variability and stability.
6. Report the highest *validated passing tested level* and the first observed failing level (if one exists).

The result must not be marketed as the absolute number of concurrent people who can ever log in. It is the sustainable performance of a specific workload under specific conditions.

## Mixed workloads

A result for "Dashboard" at 150 VUs and one for "Search" at 100 VUs **cannot be summed** into a 250-user system capacity. Shared CPU, database, network, and external services can change both results. System-level estimates require a stated workload distribution and dedicated mixed tests.

## Illustrative example only

> Scenario: 60% search journeys and 40% approval journeys. Criteria: p95 below 1.5 s, business errors below 1%, 15-minute steady-state window. The 80-VU level passed repeated tests; the 100-VU level breached the p95 criterion. This is not an estimate for idle sessions or a different infrastructure configuration.

No numbers above are actual Damocline measurements.
