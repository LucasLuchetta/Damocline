# RQ-3 controlled independent-host reference — 2026-11-21

**Evidence status:** operator-reported console outputs from a controlled two-machine
LAN trial. Not an independently archived/reviewed raw evidence bundle.
The source artifacts remain private on the operator's devices. No public host
address, username, credentials, cookie, request bodies or client identifiers
are included here. **Not** a production capacity result and **not** completion
of RQ-3 / `PROVEN_REPRESENTATIVE`.

## Experimental setup

- Generator: Windows 11 PC, Go 1.26.3, Damocline from PR #236.
- Target: separate Ubuntu home server, Python 3.12.3 reference fixture.
- Network: private LAN, Windows generator connected by Wi-Fi.
- Fixture: GET `/work`, 50 ms synthetic delay, 4 processing slots, 503
  on saturation; GET `/healthz` for pre-load path probing.
- Workload: closed loop, 30 seconds, 100 requests/second configured **ceiling**
  (not a target rate), expected HTTP 200 and body marker, 1% maximum error
  rate, 1,000 ms p95 objective, no writes.
- `crosscheck`: independent server-owned NDJSON ledger; count relative
  tolerance 0, count absolute tolerance 0; window ±2 seconds.

## Reported results

| Evidence | 2 VUs (smoke) | 8 VUs (overload) |
| --- | ---: | ---: |
| Damocline HTTP requests | 459 | 1,438 |
| Target ledger records | 459 | 1,438 |
| Target ledger lines unparsed | 0 | 0 |
| Unattributed in-window entries | 0 | 0 |
| HTTP 2xx (client / server) | 459 / 459 | 1,176 / 1,176 |
| HTTP 5xx (client / server) | 0 / 0 | 262 / 262 |
| Error rate | 0.00% | 18.22% |
| Client request p95 | 205.345 ms | 290.533 ms |
| Target server p95 | 50.807 ms | 50.885 ms |
| HTTP requests/s | 15.30 | 47.93 |
| Network baseline p50 / p95 / max | 8.4 / 215.1 / 523.4 ms | 18.8 / 95.3 / 96.8 ms |
| `crosscheck` verdict | MATCH | MATCH |
| Run outcome | PASS | Failed criteria (exit code 1) |

The exact request/status equality has **zero numerical tolerance**.
At 8 VUs the 18.22% error rate violated the declared 1% budget,
while independent target evidence agreed on all 262 HTTP 5xx responses.
The first run met its observed thresholds, but its network-path baseline
was unstable and measurement quality was `LIMITED`.
The overloaded run was likewise marked `LIMITED` due to absent workload-
specific generator calibration. Neither result establishes sustainable capacity.

Server p95 near 51 ms was consistent with the fixed 50 ms synthetic delay.
The larger client p95 values are **not** measurements of pure network latency:
percentile differences across different latency distributions cannot
isolate connection, scheduling, TCP, body-reading and other effects.

## Remaining gaps / next falsifications

1. Repeat per configuration across time, preserving run-level raw artifacts
   privately and immutable hashes publicly. Prefer a wired generator to
   isolate variable Wi-Fi latency.
2. Reconcile fault and recovery runs at the target ledger, including a
   deliberate 503 burst and target unavailability (controlled lab only).
3. Run k6 with the same offered workload and request/body success rules, and
   independently reconcile its server ledger. Record run order to control for
   target drift.
4. Add host resource evidence and a second environment; representative,
   authenticated application journeys remain outstanding under the RQ plan.

**Interpretation:** these are two useful controlled cross-host falsification
attempts whose request/status counters reconciled exactly. They are not
statistically sufficient to certify generator accuracy, latency equivalence,
a production capacity ceiling, or RQ-3 acceptance.
