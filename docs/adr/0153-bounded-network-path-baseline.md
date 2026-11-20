# ADR 0153: Bounded network-path baseline before load

## Status

Accepted.

## Context

On a pilot, the load generator ran on a Wi-Fi client. Single-request latency
to the target measured on the server host was p95 9 ms, but the generator
observed p95 2.6 s (ping max 1.1 s). Damocline reported the application as
failing at 2 VUs with quality `LIMITED` only because no generator preflight
was attached. Nothing indicated that the generator's network path, not the
application, dominated latency. A reader could conclude that "the system
cannot handle N users" when no application evidence supported it.

## Decision

1. Traffic-generating CLI commands (`run`, `explore`, `explore-arrival`,
   `campaign`, `arrival-campaign`, `start`) request a network-path baseline
   through `config.Options.NetworkBaseline`. Library callers, including the
   calibrator-owned loopback fixture with its independent server ledger, do
   not; their reports carry no `network_baseline` field.
2. The engine sends the baseline **inside every native run**, before the load
   observation starts (before `started_at`, the generator sampler and every
   load counter). Explore and campaign trials therefore each carry their own
   baseline. This costs about 2-4 s per trial but makes every embedded run
   report self-contained and detects path changes between trials. A single
   baseline per exploration would not be attached to the run reports that
   readers, goal assessors and audits consume.
3. Bounds: exactly 20 sequential requests, one at a time, spaced by
   `max(100 ms, 1 s / max_rps)`; per-request timeout `min(request_timeout,
   5 s)`; total wall budget 30 s (remaining requests are recorded as
   `not_attempted`). A dedicated transport with one connection is used; the
   first sample includes connection setup. Latency is time to response
   headers; at most 64 KiB of body is drained.
4. Safety: the request is `GET` or `HEAD` only, with no body, cookies or
   custom headers (so no credentials are forwarded), on the scenario origin.
   The default target is the first `GET`/`HEAD` step whose path has no
   template variables and no body. `baseline_path` may name an explicit
   literal same-origin path (no templates, fragments, `//` or `..`). A
   scenario without an eligible step must set `baseline_path` or an explicit
   `network_baseline_skip_reason`; Parse otherwise fails before any traffic.
5. Opt-out is only explicit: `network_baseline_skip_reason` (safe 1-80
   character label) is recorded as `{"status":"SKIPPED","skip_reason":...}`.
   A skip does not change quality; `insights` reports it as a CAUTION.
6. Run report schema v37 adds `network_baseline` with status, source,
   method, query-free path, planned/completed/failures/not_attempted,
   unexpected-status count, interval, timeout and nearest-rank
   min/p50/p95/max plus `max_to_p50_ratio`. Pre-v37 reports must not carry it.
7. Screening thresholds (`report.NetworkBaselineIssue`), relative to the
   configured request `p95_limit_ms`:
   - no request completed: quality `INVALID`;
   - any failure or not-attempted request: `LIMITED`;
   - baseline p95 > 50% of `p95_limit_ms`: `LIMITED` (dominant path);
   - max/p50 > 10 **and** max > 25% of `p95_limit_ms`: `LIMITED`
     (unstable path; the absolute floor prevents sub-millisecond loopback
     jitter from tripping the ratio).
   Each finding states that the generator network path may dominate latency
   and that no application-capacity conclusion is supported.
8. Consumers: the run prints a baseline line and warning; `insights` adds a
   LIMIT finding and marks a FAIL as "OBJECTIVES NOT MET (NETWORK PATH MAY
   DOMINATE)"; closed-loop explore marks a non-PASS probe `INCONCLUSIVE`
   instead of reporting `NO_PASSING_LEVEL`/`BRACKETED`; arrival explore marks
   the probe `INSUFFICIENT_EVIDENCE` instead of `NO_PASSING_RATE`;
   `report.GeneratorCapacityIssue` returns the issue, so campaign phases are
   classified `INVALID_DELIVERY` and capacity-goal assessment rejects the
   run as inadequate generator evidence.

## Consequences

- A generator on a slow or jittery path can no longer silently produce an
  application-limit conclusion.
- Every CLI run sends 20 additional lightweight requests before load. They
  are not counted in any load metric, window, or rate.
- Campaign and goal screens are conservative: a passing run with a
  dominating baseline is also treated as inadequate evidence.

## Limitations

- The baseline runs only before load, not after; path degradation during
  the run is not detected by it.
- Baseline latency includes server time for the baseline path and is not a
  pure network RTT. A slow baseline endpoint can trip the screen; choose a
  cheap `baseline_path` in that case.
- 20 samples give a coarse p95 (the 19th order statistic); thresholds are
  conservative screens, not calibrated statistical tests.
- An unexpected HTTP status still counts as a completed round trip.
- Reports hand-built without the field at v37 are not penalized; the engine
  always emits it when the CLI requested it.
