# Local reference validation

Damocline includes a bounded HTTP fixture with known response delay, concurrency, response codes, and counters. The calibration command runs that fixture in a separate child process over loopback and compares server-side counters with the engine report. Both processes still share the same host.

Run the default smoke matrix from the repository root:

```powershell
go run ./cmd/damocline-calibrate --out .damocline/calibration/reference.json
```

The command has a 240-second hard deadline (the child fixture lifetime is capped at 235 seconds) and includes a 30-second sustained stage at 10 journey arrivals per second. It runs three five-second healthy stages at 2, 5, and 10 arrivals per second, then 35 exact arrival profiles through the native engine: GET responses of 0, 1, 4, and 64 KiB plus POST with a 4 KiB synthetic body and 4 KiB response, each at exactly 2, 5, and 10 arrivals/s under both `reuse` and `new_per_request`; five additional trusted HTTPS GET profiles use a fixed 4 KiB response at 2, 5, 10, 20, and 40 arrivals/s with connection reuse. Schema v9 added six closed-loop profiles at 1, 2, 5, 10, 20, and 50 VUs. Schema v10 adds the two higher-rate HTTPS profiles while preserving the schema-v9 matrix. Each closed-loop profile uses GET `/work`, a 4 KiB response, a two-second window, a one-second timeout, `max_rps=100`, connection reuse, and zero ramp-up and think time. Arrival profiles use 10 workers, `max_rps=50`, and a one-second timeout; reuse profiles run for five seconds and new-per-request profiles for two seconds. Client bodies and an independent child-server request/body/TCP ledger reconcile each profile; closed-loop completion is counted by completed iterations, not arrival slots. Repeated and extended modes remain bounded by the same 240-second hard deadline. The soak must reconcile all 300 arrivals and retain at least 100 generator/process samples. The controlled error stage returns two HTTP 503 responses followed by eight successes. The overload stage must report worker-busy drops, refuse PASS, and reconcile requests received, completed, or cancelled at the run boundary.

To run the automated Go suite and all four local calibration artifacts with
fail-fast status checking, use:

```powershell
.\scripts\validate-local.ps1
```

The script prints `LOCAL VALIDATION PASSED` only after every command succeeds
and each report has the expected status. A failure stops the script before
that final message.

The reference response delay is 20 ms with a 4 KiB body. The check accepts observed request and journey p50/p95 values between 15 and 150 ms, scheduler p95 lag below half of the arrival interval, and maximum lag below one interval. It rejects Go runtime CPU or window-mean process CPU at or above 90%, heap above 256 MiB, or process RSS above 512 MiB. Maximum 250 ms process CPU ratios are recorded as diagnostics and do not alone establish sustained saturation. These broad bounds catch gross timing/accounting mistakes and an obviously saturated generator; they are not calibrated precision tolerances or universal performance guarantees. Go-runtime and OS process CPU/RSS samples must be present on Windows/Linux. Their readings describe only the generator. Whole-window mean process CPU is normalized to GOMAXPROCS; the maximum adjacent 250 ms interval ratio is retained for diagnosis and does not alone establish sustained saturation.

The calibration also injects scheduler-thread pauses of 90, 130, and 210 ms, three times each at 20 planned arrivals/s. It measures monotonic planned-slot-to-scheduler-processing lag and checks the maximum observed lag against the known pause minus one 50 ms slot interval, with an absolute tolerance of 50 ms. Arrival counts must reconcile in all nine observations. This verifies the timing calculation under bounded injected stalls on the host running the command; it does not characterize arbitrary OS load, clock error across machines, or physical scheduler accuracy outside these tested cases. Each campaign phase is reported as a separate run, so its scheduler-lag summary remains scoped to that phase.

`REFERENCE_SMOKE_PASSED` means only that these bounded local cases reconciled on this host. It does not establish maximum generator throughput, cross-platform accuracy, sustained capacity, or application capacity. Keep the JSON private under the gitignored `.damocline/` directory; it records local environment metadata and timing evidence.

## Versioned local preflight metadata (G0-01)

The same calibration report embeds a versioned `preflight` object, preserving the existing top-level calibration schema and traffic limits. Current preflight schema v11 records generator OS/arch, logical CPUs, GOMAXPROCS, Go version, runner source revision or executable digest, effective CPU entitlement and its evidence source; UTC test start/end; same-host, separate-process loopback fixture mode; healthy GET reference and exact rate stages; 35 arrival profiles (30 HTTP and five trusted HTTPS) plus six closed-loop VU profiles, with client/server counter observations. It records server-accepted TLS sockets independently from request-carrying TCP connections, so HTTPS handshakes reconcile when a concurrent idle connection carries no request. Historical schema-v1 through v10 artifacts remain readable with their original evidence scope.

The `scenario_fingerprint_sha256` hashes **only** this fixed, non-secret fixture configuration. It is not a tamper-proof signature or a fingerprint of a customer workload. The `highest_passing_tested_arrival_rate` appears as a positive value only when **the entire** reference check passed, and describes the highest successful **tested** rate—not the generator's maximum capacity.

The new metadata excludes temporary loopback ports, customer hostnames, request paths, credentials, headers, tokens, environment-variable values, and bodies. The local artifact must remain private: other sections of the existing report still include host measurements. Regression tests cover schema round-trips, unknown/incompatible versions, invalid fixture fingerprints, unsafe version metadata, failed-stage handling, and secret-marker absence.

**Not a client-workload readiness certificate:** a passing 4 KiB loopback reference cannot verify unrelated HTTPS traffic, payload sizes, multi-step sessions, or real client networks. Workload equivalence, artifact freshness and host matching remain explicitly open under G0-02/G0-08.

## Compare a requested scenario with the measured reference (G0-02)

Run this **offline** command after generating a local reference JSON. It reads a bounded calibration file and a scenario file, but does not contact the application's URL, launch test traffic, or access its credentials:

```powershell
go run ./cmd/damocline-calibrate --check-scenario examples/weighted-profiles-readonly.json --calibration .damocline/calibration/reference.json
```

The output is structured JSON with `status: "INCONCLUSIVE"`, a `request_shape` summary, exact `tested_arrival_rates`, and per-dimension `MATCHED`, `OUTSIDE_REFERENCE`, or `UNVERIFIED` findings. The CLI exits with code 2 when inconclusive (note that `go run` itself may report this as `exit status 2` and exit with code 1). Malformed scenarios produce an input error, not synthetic traffic.

This is **not a capacity-pass command**. It checks explicitly whether the declared VU/worker count, exact arrival rate and rate ceiling, workload model, request shape, duration, timeout, connection policy, and URL transport match a tested profile. The arrival matrix covers GET `/work` (4 KiB), GET `/matrix/get-empty` (0 bytes), GET `/matrix/get-1k` (1 KiB), GET `/matrix/get-64k` (64 KiB), POST `/matrix/post-4k` (4 KiB request and response), and trusted HTTPS GET `/matrix/https-4k` (4 KiB response). The 35 arrival profiles use exact 2, 5, and 10 arrivals/s for HTTP, plus 2, 5, 10, 20, and 40 arrivals/s for trusted HTTPS, 10 workers, `max_rps=50`, and 1 second timeout for reusable HTTP connections (5 seconds), new connections per request (2 seconds), and HTTPS reuse (5 seconds). Schema v10 also includes six exact closed-loop profiles at 1, 2, 5, 10, 20, and 50 VUs using GET `/work`, a 4 KiB response, a two-second window, a shared `max_rps=100` ceiling, a 1 second timeout, reuse, and no ramp-up or think time. Server request/body ledgers reconcile independently; TCP counts are exact for the serialized arrival profiles and bounded by received requests for concurrent closed profiles. Other rates or controls do not interpolate. Historical schema-v3 through v9 artifacts retain their original evidence scope. A POST body source may be read locally only to compare its byte length; body content is never copied into the fingerprint, calibration artifact, or report. Multi-step workflows, variable response distributions, other body sizes, and real target identity remain unqualified. A closed-loop workload cannot be equated to open-loop arrivals.

Reports and scenarios also record the HTTP `connection_policy`. Schema v6 calibrates `reuse` and `new_per_request` against each fixed request shape and exact rate, with an exact duration/control-tuple check and a separately observed per-profile TCP connection count. A policy declaration does not establish which behavior a remote endpoint or intermediary used.

Schema-v10 profile observations are produced by the actual engine and compared with independent child-process request, byte, and per-profile TCP connection counters; shape assessment matches only a precisely measured method/path/body-length/connection shape and exact profile controls. A fingerprint or shape match establishes only equality of declared request shape and the generator's observed behavior against this local fixture. It does not establish endpoint identity, response timing distribution, representative payload behavior, resolved secret or synthetic values, or generator headroom on another host. Therefore an ordinary target run remains **INCONCLUSIVE**; HTTPS/remote/authenticated/multi-workflow scenarios retain explicit mismatches or unknowns. Schema-v1/v2 records remain readable without profile matrix evidence; schema-v3 through v9 retain their historical evidence. No passing fixture rate, including 100 arrivals/s, authorizes an unmeasured target or implies a universal maximum. Other matrix dimensions and the G0-08 host/evidence-quality gates remain open.

A missing, failed, inconsistent, oversized, or unsupported calibration produces an inconclusive comparison, never acceptance. The output deliberately omits scenario names, URLs, routes, headers, credentials and extracted values. Treat the input and local calibration files as private.

## Attach local preflight evidence to a run

An authorized run may attach the locally saved calibration record:

```powershell
go run ./cmd/damocline run --config scenario.local.json --authorized --calibration .damocline/calibration/reference.json --out .damocline/reports/run.json
```

The run command revalidates the bounded artifact, its stage ledgers and all nine scheduler-lag observations, then compares only declared workload controls with the fixed reference. It records the sanitized calibration metadata and per-dimension findings in the report. A mismatch or unusable artifact is not attached and is reported generically; the run itself remains subject to its normal authorization and bounds.

This association is an evidence improvement, **not acceptance**. Calibration schema v2 stores an opaque fingerprint for the one-step GET `/work` journey exercised by healthy stages. A matching declared workload-shape dimension proves only that the run declares the same step shape; the JSON fixture artifact is not cryptographically signed, runtime metadata does not authenticate a physical host, and a scenario file cannot prove the endpoint is the reference server or that live responses match the fixture's 4 KiB/20 ms shape. Those dimensions remain `UNVERIFIED`, as does workload-specific generator headroom, so this command cannot produce `VALID_WITHIN_ENVELOPE` or certify application capacity. Legacy schema-v1 artifacts remain readable but their workload shape is unverified. Do not treat an attached artifact as a certificate.

For an opt-in pre-traffic gate on open-arrival `run`, add
`--require-calibration`; it requires `--calibration` and accepts only exact
passing schema-v6-or-later profile coverage for the declared rate, request
shape, worker budget, request ceiling, timing and runtime. `arrival-campaign`
accepts the same optional `--calibration` gate and checks every planned base
and spike rate before starting phases. Missing, stale, malformed, runtime
mismatched, or non-exact coverage blocks traffic. The campaign dry-run performs
the same check. These gates assess local fixture delivery only. They do not
verify remote host identity, target response behavior or campaign-duration
headroom; ordinary target reports remain LIMITED. `explore-arrival` accepts
the same optional calibration artifact and restricts adaptive probes to exact
compatible matrix rates, recording requested and effective bounds for offline
assessment. Closed-loop campaigns remain outside this gate because the matrix
measures open-arrival workloads.

## Expanded transport-only reference matrix (G0-03 partial)

Use the optional bounded fixture matrix to exercise Go HTTP transports with an **independent** client/server ledger, without contacting an application or using actual user data:

```powershell
go run ./cmd/damocline-calibrate --transport-matrix --out .damocline/calibration/transport.json
```

It executes ten small local HTTP(S) fixture variants (30 total HTTP GETs): 0/1/4/64 KiB responses, normal keep-alive, explicitly disabled connection reuse, 40 ms delayed responses, streaming with flush, known HTTP 503 errors, and ephemeral **fixture-only** TLS. All requests use `httptest` loopback servers with no outbound DNS or proxies. Each case independently reconciles the request count and response status/size. The JSON report uses a separate schema (`http_transport_reference` version 1) and does not contain dynamic endpoint URLs, certs, customer paths or secrets. CI runs this matrix on Linux and Windows.

**Native-engine regression coverage:** the automated Go test suite additionally runs the actual Damocline engine/scheduler against independently counted 1/4/64 KiB HTTP fixtures, a controlled latency-tail fixture and a connection-close fixture, with a bounded 5 journey starts/s for two seconds each. These are regression cases and are not an independently calibrated throughput envelope. The transport matrix CLI remains an independent check and does not run the scheduler.

**Scientific boundary:** the separate CLI transport matrix checks Go transport and fixture semantics; it does **not** run the scheduler. The schema-v11 preflight separately runs trusted HTTPS GETs through the native engine at exact 2/5/10/20/40 starts/s profiles and reconciles client handshake counts with independently accepted TLS sockets, request-carrying TCP connections, and request/body counters. The same schema adds exact short closed-loop profiles; they do not calibrate TLS timing precision under load, HTTP/2, real DNS latency, remote networks, streaming under production load, or a maximum rate. The evidence qualifies only the named short synthetic tuples; dynamic payload distributions and additional transport combinations remain future G0-03 work.

## Repeated end-to-end G0 acceptance

For one decision over the complete local reference set, use the [`damocline-acceptance` workflow](../README.md#run-the-repeated-g0-acceptance-lab). Each repeat launches a fresh calibrator process and independent child-fixture ledger. Acceptance manifest v7 requires the schema-v11 35-profile arrival matrix and six exact closed-loop VU profiles (41 profiles total); it reconciles client iteration/body/status evidence, client TLS handshake counts with server-accepted TLS sockets, and per-profile plus aggregate server request, byte, and request-carrying TCP counters. It also checks all required healthy arrival stages, recomputes the 300-request `VALID_WITHIN_ENVELOPE` soak, exercises bounded overload and induced HTTP failures, and verifies all ten transport-matrix definitions.

CI runs this same two-repeat acceptance lab on Windows and in a Linux container limited to four CPUs, 4 GiB memory, and 256 processes. Each runner archives only its bounded synthetic G0 acceptance directory as a workflow artifact retained for 14 days; this includes the manifest, loopback calibration/transport reports, and bounded process logs, not user scenarios or application data. Failed checks retain partial evidence when the runner produced it. This checks that the bounded local evidence gates remain executable across those runner environments; it is not independent physical-host validation or an external application-capacity test.

The command requires two independent process runs (three at most for the baseline matrix, two for extended), compares fixture fingerprints and calibrator runner identity, reports descriptive per-profile p95 spread, and includes the transport fixture result. A failed process, gate, timeout, or interruption prevents a pass. Its fresh evidence directory contains incremental state JSON, each produced calibration/transport report, hashes, and process logs streamed to disk with a 1 MiB per-stream bound. A missing report stays explicitly inconclusive; a failed report remains available with the gate observations. The p95 range is not a confidence interval, and sequential runs on one host do not establish statistical independence or a hardware-class tolerance. A pass applies only to the specific local binary, runtime, fixture, workload tuples, and observation windows recorded in that manifest.

## Workload-specific self-calibration (ADR 0154)

Fixed reference profiles cannot match an ordinary scenario. To get generator evidence for the exact workload, run this on the same generator host with the same `damocline` executable:

```sh
damocline calibrate --config scenario.json --users 2,4,8 --from-report reports/run.json --out reports/selfcal.json
damocline run --config scenario.json --authorized --calibration reports/selfcal.json --require-calibration
```

The mirror listens only on `127.0.0.1`. It answers each declared route with the recorded (or declared) status, size and median delay, and it keeps its own ledger. Environment-backed cookies, headers and bodies are replaced by synthetic values of equal length. Levels fail on any ledger mismatch, dropped or late starts, scheduler lag above 50 ms p95 or 100 ms max, closed-loop delivery below 85% of the modeled bound, generator overhead above 25 ms p50 or 50 ms p95, think-time inflation, or CPU/RSS guardrail breach. The artifact expires after 24 hours and binds to the executable identity and CPU entitlement.

A matched clean run can become `VALID_WITHIN_ENVELOPE`. That proves only generator-side delivery for this workload on this host. It does not prove target identity, network-path equivalence, application behavior, representativeness, or headroom for durations longer than the calibrated level. Scenarios with response extraction or JSON assertions cannot be self-calibrated.

## Offline evidence freshness and provenance (G0-08 partial)

The `--check-scenario` command also emits `calibration_age`, `runtime_compatibility` and `host_identity` dimensions. A future-dated artifact (beyond five minutes), a calibration older than a **conservative operational 24-hour review threshold**, or a mismatch in OS/architecture/Go version/CPU configuration/known source revision makes the declared shape `NOT_ESTABLISHED`. This 24-hour threshold is a warning policy, **not** a validated statistical expiry.

An unavailable source revision stays `UNVERIFIED`; even matching runtime attributes do not identify the actual machine. `host_identity` therefore remains `UNVERIFIED`. A `MATCHED` age or runtime observation does not change the mandatory overall `INCONCLUSIVE` verdict. An invalid calibration cannot lend any passing rate evidence.

Repeat the healthy matrix three times to inspect short-run variability:

```powershell
go run ./cmd/damocline-calibrate --repeats 3 --out .damocline/calibration/reference-repeats.json
```

This remains a bounded engineering check. Compare every run at each rate; do not choose the fastest repeat or treat three repeats as a sufficient statistical sample for a client recommendation. The optional `--extended` mode adds 15, 20, 50, and 100 arrivals/s and allows at most two repetitions within the hard deadline:

```powershell
go run ./cmd/damocline-calibrate --extended --out .damocline/calibration/reference-extended.json
```

The scheduler also has deterministic tests that fault-inject a callback stall and require late arrivals and lag to be counted. Engine tests ensure insufficient samples cannot pass, admitted work drains within a bounded grace window, cancelled requests cannot pass, and responses above the 2 MiB body limit fail visibly. Schema v4 reports both the configured workload observation window and full elapsed wall-clock time, including drain; throughput uses the observation window. The local reference server tests fixed payload sizes, bounded concurrency, failures, queueing, and recovery. CI runs the extended matrix on Linux and Windows. These checks establish the tested operating envelope through 100 arrivals/s, a 4 KiB response, and a 30-second soak on hosts where the matrix is run. They do not establish the maximum possible generator throughput, precision on untested machines or payload sizes, or a client-capacity recommendation. Repeat the matrix on each deployment host class before using it to interpret a capacity campaign.


## Phantom capacity

On Linux, run the three loopback controls with:

```sh
go test -tags=integration ./internal/arrivalcampaign -run '^TestPhantomCapacity$' -count=3 -v
```

Each control uses one HTTP request per journey, a five-second observation
window, a 500 ms p95 limit, and zero allowed errors. The independent server
ledger counts receipts, service completions, queue rejections, and
cancellations. The server has two service slots, two waiting places, and
100 ms service time, giving a nominal ceiling of 20 requests/s. Totals
include completed work during drain.

| Control | Predeclared acceptance criteria |
| --- | --- |
| 15 scheduled starts/s | All 75 requests arrive and complete. No drops, rejections or cancellations. The phase is `PASS_OBSERVED`. |
| 30 scheduled starts/s | All 150 requests arrive. At least 75 and at most 104 complete, allowing up to four pending requests during drain. Excess receipts are rejected. The phase is `FAIL_OBSERVED`. |
| 30 scheduled starts/s with generator CPU withheld | The parent suspends only the generator process for 4.9 seconds after the second receipt. The target receives between two and 75 requests, completes all of them, and records zero rejections and cancellations. The engine records late starts and reports `INCONCLUSIVE`. The phase is `INVALID_DELIVERY`. |

Each control also requires exact agreement between native request totals and
the independent ledger. The intervention emulates loss of generator CPU time
through Linux process suspension. Its evidence applies to that intervention,
the native engine, and the open-arrival phase classifier. It leaves steady
CPU quotas, full campaign/goal execution, and representative application
capacity outside this protocol.

Three runs in a Linux amd64 Docker container with network mode `none` and
Go 1.26.3 produced the same counts in each control:

| Control | Scheduled | Received | Completed | Rejected | Late starts | Phase |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 15 starts/s | 75 | 75 | 75 | 0 | 0 | `PASS_OBSERVED` |
| 30 starts/s | 150 | 150 | 101 | 49 | 0 | `FAIL_OBSERVED` |
| 30 starts/s, generator suspended | 150 | 4 | 4 | 0 | 146 | `INVALID_DELIVERY` |

All server cancellation counts were zero. This is `VALIDATED_CONTROLLED`
evidence for these phase classifications on this fixture.

### Continuous CPU quota through campaign and goal

Run `python lab/verify-arrival-cpu-quota.py` with Go, Python 3, and an amd64
Linux Docker engine with cgroup v2. The script builds Linux binaries locally.
Each case gets a fresh server and a generator with a shared loopback network
namespace and a separate CPU cgroup. Both generators run the same background
CPU load and use `GOMAXPROCS=4`. Both pass the same workload self-calibration
at 25 starts/s before running through the extended-envelope campaign entry
point. The extended entry point attaches the workload-specific calibration
to phase evidence. After the server records at least ten work completions,
the intervention adds a 5 ms CPU quota per 100 ms period to the generator
alone. Applying the quota during observation lets the experiment exercise
delivery after passing the CPU-entitlement preflight. The script checks the
quota and kernel throttling counters.

The server has fifteen 20 ms service slots, with fifteen total admissions.
The read-only scenario schedules 25 starts/s, with three 30-second
observations and a 10-second recovery per trial, a 500 ms p95 objective,
and zero allowed HTTP errors. Network baseline probes use `/healthz`. The
independent ledger counts completions and rejections on `/work`.

- The control must complete all 3,000 starts with no drops or target
  rejections. The campaign must pass and offline `goal` must credit the
  exact target at 25 starts/s across all three trials.
- The intervention must deliver a positive count below the scheduled
  count for the phases executed, reconcile the shortfall with dropped
  starts, and reconcile native requests with the target ledger. The
  affected phase must be `INVALID_DELIVERY`, the campaign must be
  `INCONCLUSIVE`, and both the embedded assessment and offline `goal`
  must return `INSUFFICIENT_EVIDENCE`.

The script retains the calibration records, campaign artifacts, offline goal
reports, server counters, and CPU quota evidence under the gitignored
`.damocline/` directory. Goal replay runs with networking disabled.

With workload calibration, both controls passed all three trials, delivered
3,000 starts each, and earned
`SUSTAINED_TARGET_PASSED_UNDER_TESTED_CONDITIONS` at 25 starts/s. In both
limited cases, the server and generator counted the same successful HTTP
requests, with zero rejections:

| Calibrated run under quota | Scheduled | Target completions | Dropped starts | Cgroup throttles |
| --- | ---: | ---: | ---: | ---: |
| 1 | 750 | 360 | 390 | 305 |
| 2 | 750 | 488 | 262 | 305 |

Both limited runs were `INVALID_DELIVERY` at the phase, `INCONCLUSIVE` at
the campaign, and `INSUFFICIENT_EVIDENCE` in both demand assessments.

Replaying the first limited phase with a Go overlay that removes only the dropped or
cancelled delivery branch in `arrivalcampaign.ClassifyEvidence` failed the
`INVALID_DELIVERY` assertion. The mutated classifier returned
`INSUFFICIENT_EVIDENCE` because the native run was already inconclusive.
The suspension regression (`TestPhantomCapacity/generator-denied-cpu`)
also failed under the same overlay. A live mutant attempt reached the
CPU abort guardrail before the expected classification, so only the replay
isolates the classifier change on the calibrated quota evidence.

Earlier uncalibrated runs applied the quota before campaign startup and
used `GOMAXPROCS=1`. Two Docker Desktop runs with Linux amd64 and Go
1.26.3 delivered all 3,000 control starts per run. Both control campaigns
passed. Under quota, the first observation produced the following counts
before the campaign stopped:

| Run | Scheduled | Target completions | Dropped starts | Cgroup throttles |
| --- | ---: | ---: | ---: | ---: |
| 1 | 750 | 484 | 266 | 598 |
| 2 | 750 | 510 | 240 | 570 |

The target and generator counts agreed, with zero target rejections and
zero HTTP failures. Both limited phases were `INVALID_DELIVERY`, both
campaigns were `INCONCLUSIVE`, and both embedded and offline assessments
returned `INSUFFICIENT_EVIDENCE`. The offline control assessments also
returned `INSUFFICIENT_EVIDENCE` because measurement quality fell short of
the capacity-assessment threshold. This is `VALIDATED_CONTROLLED` evidence
for rejection of underdelivered load in this setup.
