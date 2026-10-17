# k6 reference behavior and Damocline validation matrix

**Purpose:** reusable *engineering validation cases* grounded in [k6's published docs](https://grafana.com/docs/k6/latest/using-k6/) and [open test source](https://github.com/grafana/k6/tree/master/lib/executor). This document does not claim that k6's unit tests certify our code, nor does it authorize copying k6 source.

| External reference / source | Behavior to reproduce and validate independently | Current Damocline gap | Owned acceptance test |
| --- | --- | --- | --- |
| [constant arrival rate tests](https://github.com/grafana/k6/blob/master/lib/executor/constant_arrival_rate_test.go) | Requested arrival rate, start-time spacing, insufficient VU warning, dropped starts, cancellation | `time.NewTicker` lag and coalesced ticks not explicitly reconciled against ideal schedule | Monotonic logical arrival clock; all planned arrivals accounted; lag and drop reasons recorded |
| [ramping arrival rate tests](https://github.com/grafana/k6/blob/master/lib/executor/ramping_arrival_rate_test.go) | Independent user arrival target changes and sensible ramp boundaries | No gradual arrival-rate scenarios | Deterministic step/linear ramps with expected integrated schedule count and jitter bounds |
| [ramping VUs tests](https://github.com/grafana/k6/blob/master/lib/executor/ramping_vus_test.go) | Graceful ramp-down and finalization; no unexpected VU oscillation | Fixed VU exploration only; cancellation at window boundary | Requested vs running VUs per epoch; graceful drain and correct interruption handling |
| [per-VU/shared iterations tests](https://github.com/grafana/k6/tree/master/lib/executor) | Counts per worker, incomplete iteration reporting and time-budget termination | No explicit bounded-iteration execution modes | Synthetic fixture checks allocation, completion, incomplete count and fairness |
| [open/closed model](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/) + [NSDI 2006](https://www.usenix.org/conference/nsdi-06/open-versus-closed-cautionary-tale) | Slow targets reduce closed-loop throughput; independent arrivals still *request* the same arrival rate | Models exist but need delivery-accuracy reference testing | With an injected server delay, closed-loop throughput drops and open-loop scheduled count stays correct |
| [dropped iterations](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/dropped-iterations/) | Differentiate generator VU exhaustion from target degradation | Dropped count alone cannot establish root cause | Saturated generator vs saturated fixture cases have separately classified limitations |
| [graceful stop](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/graceful-stop/) | Drain outstanding journeys for bounded time | Cancellation discards boundary completions | Correct complete/interrupted status, wall clock and no accidental extra work |
| [HTTP timings](https://grafana.com/docs/k6/latest/using-k6/metrics/reference/) | Separate waiting, connecting, TLS, receive duration; report exactly what is timed | Current HTTP duration includes connection setup unlike k6 duration | HTTP trace timings under controlled transport; unit definitions explicitly documented |
| [thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/) | Per-tag, per-group and metric thresholds; fail on breached SLO | Global/per-step request p95 only; no full-journey SLO | Business journey and status/assertion threshold tests, valid sample counts and outcome states |
| [scenario groups](https://grafana.com/docs/k6/latest/using-k6/scenarios/) | Independent mixed workflows under one shared capacity campaign | One sequential journey per execution | Weighted scenario ratios verified with random-seed deterministic fixtures |
| [TPC-W](https://www.tpc.org/tpcw/) | Varied synthetic sessions, reads/writes, data volume and business transactions | HTTP page GET-only campaigns misrepresent business mix | Synthetic dataset and full transaction success requirements, no TPC-W certification claim |

## Comparison hygiene

- Explicitly state which metrics have different measurement boundaries: Damocline's current request latency is *not* k6's `http_req_duration`.
- Compare scheduled vs started transactions, not just throughput.
- Compare the same synthetic dataset, concurrency, network/protocol, caching, time window, assertions, retry and think time.
- Re-run each reference comparison at least across independent trials and retain the raw anonymized fixture outputs.
- Do not use production domains. Never port or vendor k6 AGPL-3.0 source code without specialist license review.
- Passing comparisons with k6 does not on its own prove any field-study result, vendor neutrality or capacity guarantee.

See [research and implementation plan](RESEARCH_TO_CAPACITY_PLAN.md).
