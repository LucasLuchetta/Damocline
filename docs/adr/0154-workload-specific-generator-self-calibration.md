# ADR 0154: Workload-specific generator self-calibration

## Status

Accepted (pre-alpha).

## Context

The fixed reference calibration (ADR 0006 and successors) measures exact
profiles: specific VU levels, one 4 KiB route, zero think time. An ordinary
scenario (for example five GET steps, 2 s think time, 50 closed-loop VUs,
~26 KiB bodies, per-VU cookies) can never match those profiles, so every run
against a real target stayed `LIMITED` with "No local generator preflight is
attached", and `audit` could not move past measurement-limited evidence even
when every trial was clean.

## Decision

Add `damocline calibrate --config scenario.json --users 2,4,8 --out FILE`
(or `--arrival-rates` for open-arrival scenarios).

- It lives in the main `damocline` executable, not `damocline-calibrate`, so
  the artifact binds to the same executable identity that later runs record.
- It replays the exact declared shape (mode, users or arrival rate, steps,
  methods, paths, headers, think-time model, ramp, connection policy, max_rps,
  timeout, request body sizes, per-VU and per-workflow session counts) through
  the normal engine against a bounded mirror bound to `127.0.0.1` in the same
  process. The scenario target is never contacted.
- Mirror response sizes and delays come from `--response-bytes` and
  `--server-delay`, or from `--from-report` (a recorded run of the real
  target: per-step mean complete body size and request p50). Reports with a
  different workload fingerprint are rejected. The report is identified only
  by its SHA-256.
- Environment-backed secrets (cookies, header values, request bodies, the
  synthetic-data prefix) are never sent. Only their byte length is read, and
  the mirror receives synthetic values of the same length. The recorded
  per-level workload fingerprint is recomputed from the original declarations
  and equals the real scenario's fingerprint at that level.
- The mirror keeps an independent ledger: received, completed, unmatched
  routes, request and response body bytes, and requests that carry cookies.
- Each level passes only if every check holds: run completed; no client
  failures; mirror ledger equals client request count; response bytes
  reconcile; per-VU cookies delivered on every request; journey conservation;
  open-arrival conservation with no dropped starts and scheduler lag p95 at most
  50 ms and max under 100 ms; closed-loop delivery of at least 85% of the bound
  implied by users, mirror delay, think time and max_rps; generator overhead
  (client latency minus mirror delay) p50 at most 25 ms and p95 at most 50 ms;
  observed think time within the declared mean plus 10% plus 20 ms; generator
  process CPU and RSS under the generatorhealth guardrails (default 80% /
  1 GiB, 3 samples), and no guardrail trigger. Levels run in ascending order
  and the first failure stops the invocation.
- Bounds: at most 12 levels, 5 s to 2 min per level, and a 15 min wall-time
  cap that is checked before any traffic and enforced by context.
- The artifact (`kind: workload_self_calibration_v1`, schema 1) records the
  generator executable identity, the host CPU entitlement (cores, source,
  GOMAXPROCS, logical CPUs), the controls, the mirror model, the guardrails and
  per-level evidence. Readers decode it strictly and recompute every passing
  level's checks from the recorded numbers.
- `run`, `explore` and `campaign` accept it through `--calibration` and
  `--require-calibration`. The preflight checks status, the 24-hour age limit,
  runtime and CPU entitlement, the workload fingerprint and every planned level
  (the explore ladder; campaign base and spike levels) before traffic. Each run
  then embeds the artifact as `workload_self_calibration` (run schema v38).
- `AssessQuality` recomputes applicability from the report alone: valid
  artifact, passed, age at most 24 h relative to the run start, identical
  executable identity and CPU entitlement, identical controls, a known
  workload fingerprint, and a passed level equal to the run's users or arrival
  rate. When it applies and the run has no other finding, quality becomes
  `VALID_WITHIN_ENVELOPE`. Otherwise the run stays `LIMITED` with the specific
  reason. Attaching both calibration kinds is invalid. `audit` recomputes
  quality per phase, so it consumes the artifact with no audit-specific code.

## What this proves and what it does not

It proves, for this host and executable at the recorded time, that the
generator delivered the declared workload shape at the calibrated levels on
loopback, with a matching independent ledger, bounded scheduling and overhead,
and resource headroom.

It does **not** prove target identity, network-path equivalence, application
behavior, workload representativeness, or capacity. It does not certify run
durations longer than the calibrated level duration. The mirror runs in the
generator process, so its CPU cost counts against generator headroom
(conservative). Mirror content is synthetic padding (status, size,
`must_contain` text and delay only). Scenarios with response extraction or
JSON assertions are rejected because the mirror cannot reproduce them. Ramp-up
and duration are recorded but not matched. Explore bracket-refinement levels
outside the calibrated ladder stay `LIMITED`. Per-level fingerprints differ
only by session count, so any calibrated level's fingerprint identifies the
workload.

## Alternatives considered

- A subcommand of `damocline-calibrate`: rejected, because the executable
  identity would never match `damocline run` reports.
- A separate mirror process: more isolated, but it adds process management.
  Running in the same process is the conservative choice for headroom.
- Contacting the real target for sizes: rejected. Recorded reports give the
  same information without new traffic.
