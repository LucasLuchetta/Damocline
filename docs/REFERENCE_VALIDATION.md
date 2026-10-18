# Local reference validation

Damocline includes a bounded HTTP fixture with known response delay, concurrency, response codes, and counters. The calibration command runs that fixture in a separate child process over loopback and compares server-side counters with the engine report. Both processes still share the same host.

Run the default smoke matrix from the repository root:

```powershell
go run ./cmd/damocline-calibrate --out .damocline/calibration/reference.json
```

The command has a 220-second hard deadline (the child fixture lifetime is capped at 210 seconds) and includes a 30-second sustained stage at 10 journey arrivals per second. It runs three five-second healthy stages at 2, 5, and 10 arrivals per second, then 30 exact shape/connection-policy/rate profiles through the native engine: GET responses of 0, 1, 4, and 64 KiB, plus POST with a 4 KiB synthetic body and 4 KiB response, each at exactly 2, 5, and 10 arrivals/s under both `reuse` and `new_per_request`. Each profile uses 10 workers, `max_rps=50`, and a one-second request timeout. Reuse profiles run for five seconds; new-per-request profiles run for two seconds to remain within the fixed overall deadline. Client body observations and an independent child-server request/body-byte ledger must reconcile exactly. The healthy rate matrix has exactly `rate * 5` scheduled arrivals: 10, 25, and 50 (85 total); the workload matrix adds 595 planned arrivals (425 reuse and 170 new-per-request). Repeated and extended modes remain bounded by the same 220-second hard deadline. The soak must reconcile all 300 arrivals and retain at least 100 generator/process samples. The controlled error stage returns two HTTP 503 responses followed by eight successes. The overload stage must report worker-busy drops, refuse PASS, and reconcile requests that were received, completed, or cancelled at the run boundary.

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

The same calibration report embeds a versioned `preflight` object, preserving the existing top-level calibration schema and traffic limits. Schema v6 records generator OS/arch, logical CPUs, GOMAXPROCS, Go version and runner source revision or executable digest; UTC test start/end; same-host, separate-process loopback fixture mode; the healthy GET reference, exact rate stages, and a fixed workload-shape/rate/connection-policy matrix with client/server counter observations. Schema-v1 through v5 artifacts remain readable with their original evidence scope.

The `scenario_fingerprint_sha256` hashes **only** this fixed, non-secret fixture configuration. It is not a tamper-proof signature or a fingerprint of a customer workload. The `highest_passing_tested_arrival_rate` appears as a positive value only when **the entire** reference check passed, and describes the highest successful **tested** rate—not the generator's maximum capacity.

The new metadata excludes temporary loopback ports, customer hostnames, request paths, credentials, headers, tokens, environment-variable values, and bodies. The local artifact must remain private: other sections of the existing report still include host measurements. Regression tests cover schema round-trips, unknown/incompatible versions, invalid fixture fingerprints, unsafe version metadata, failed-stage handling, and secret-marker absence.

**Not a client-workload readiness certificate:** a passing 4 KiB loopback reference cannot verify unrelated HTTPS traffic, payload sizes, multi-step sessions, or real client networks. Workload equivalence, artifact freshness and host matching remain explicitly open under G0-02/G0-08.

## Compare a requested scenario with the measured reference (G0-02)

Run this **offline** command after generating a local reference JSON. It reads a bounded calibration file and a scenario file, but does not contact the application's URL, launch test traffic, or access its credentials:

```powershell
go run ./cmd/damocline-calibrate --check-scenario examples/weighted-profiles-readonly.json --calibration .damocline/calibration/reference.json
```

The output is structured JSON with `status: "INCONCLUSIVE"`, a `request_shape` summary, exact `tested_arrival_rates`, and per-dimension `MATCHED`, `OUTSIDE_REFERENCE`, or `UNVERIFIED` findings. The CLI exits with code 2 when inconclusive (note that `go run` itself may report this as `exit status 2` and exit with code 1). Malformed scenarios produce an input error, not synthetic traffic.

This is **not a capacity-pass command**. It checks explicitly whether the declared VU/worker count, exact arrival rate and rate ceiling, workload model, request shape, duration, timeout, connection policy, and URL transport match a tested profile. Schema v6 covers GET `/work` (4 KiB), GET `/matrix/get-empty` (0 bytes), GET `/matrix/get-1k` (1 KiB), GET `/matrix/get-64k` (64 KiB), and POST `/matrix/post-4k` (4 KiB request and response), each at exactly 2, 5, and 10 arrivals/s, 10 workers, `max_rps=50`, and 1 second timeout for both reusable HTTP connections (5 seconds) and new connections per request (2 seconds). The independent child server records distinct accepted TCP connections that carried each profile route: one for a serialized reusable profile and one per request for `new_per_request`; request and body-byte ledgers remain separate. Other rates or ceilings do not interpolate. Schema-v3, v4, and v5 artifacts remain readable with their original evidence scope; old records do not gain TCP connection evidence. A POST body source may be read locally only to compare its byte length; body content is never copied into the fingerprint, calibration artifact, or report. Closed-loop, TLS workload calibration, multi-step workflows, variable response distributions, other body sizes, and real target identity remain unqualified. A closed-loop workload cannot be equated to open-loop arrivals.

Reports and scenarios also record the HTTP `connection_policy`. Schema v6 calibrates `reuse` and `new_per_request` against each fixed request shape and exact rate, with an exact duration/control-tuple check and a separately observed per-profile TCP connection count. A policy declaration does not establish which behavior a remote endpoint or intermediary used.

Schema-v6 profile observations are produced by the actual engine and compared with independent child-process request, byte, and per-profile TCP connection counters; shape assessment matches only a precisely measured method/path/body-length/connection shape and exact profile controls. A fingerprint or shape match establishes only equality of declared request shape and the generator's observed behavior against this local fixture. It does not establish endpoint identity, response timing distribution, representative payload behavior, resolved secret or synthetic values, or generator headroom on another host. Therefore an ordinary target run remains **INCONCLUSIVE**; HTTPS/remote/authenticated/multi-workflow scenarios retain explicit mismatches or unknowns. Schema-v1/v2 records remain readable without profile matrix evidence; schema-v3 through v5 retain their historical evidence. No passing fixture rate, including 100 arrivals/s, authorizes an unmeasured target or implies a universal maximum. Other matrix dimensions and the G0-08 host/evidence-quality gates remain open.

A missing, failed, inconsistent, oversized, or unsupported calibration produces an inconclusive comparison, never acceptance. The output deliberately omits scenario names, URLs, routes, headers, credentials and extracted values. Treat the input and local calibration files as private.

## Attach local preflight evidence to a run

An authorized run may attach the locally saved calibration record:

```powershell
go run ./cmd/damocline run --config scenario.local.json --authorized --calibration .damocline/calibration/reference.json --out .damocline/reports/run.json
```

The run command revalidates the bounded artifact, its stage ledgers and all nine scheduler-lag observations, then compares only declared workload controls with the fixed reference. It records the sanitized calibration metadata and per-dimension findings in the report. A mismatch or unusable artifact is not attached and is reported generically; the run itself remains subject to its normal authorization and bounds.

This association is an evidence improvement, **not acceptance**. Calibration schema v2 stores an opaque fingerprint for the one-step GET `/work` journey exercised by healthy stages. A matching declared workload-shape dimension proves only that the run declares the same step shape; the JSON fixture artifact is not cryptographically signed, runtime metadata does not authenticate a physical host, and a scenario file cannot prove the endpoint is the reference server or that live responses match the fixture's 4 KiB/20 ms shape. Those dimensions remain `UNVERIFIED`, as does workload-specific generator headroom, so this command cannot produce `VALID_WITHIN_ENVELOPE` or certify application capacity. Legacy schema-v1 artifacts remain readable but their workload shape is unverified. Do not treat an attached artifact as a certificate.

## Expanded transport-only reference matrix (G0-03 partial)

Use the optional bounded fixture matrix to exercise Go HTTP transports with an **independent** client/server ledger, without contacting an application or using actual user data:

```powershell
go run ./cmd/damocline-calibrate --transport-matrix --out .damocline/calibration/transport.json
```

It executes ten small local HTTP(S) fixture variants (30 total HTTP GETs): 0/1/4/64 KiB responses, normal keep-alive, explicitly disabled connection reuse, 40 ms delayed responses, streaming with flush, known HTTP 503 errors, and ephemeral **fixture-only** TLS. All requests use `httptest` loopback servers with no outbound DNS or proxies. Each case independently reconciles the request count and response status/size. The JSON report uses a separate schema (`http_transport_reference` version 1) and does not contain dynamic endpoint URLs, certs, customer paths or secrets. CI runs this matrix on Linux and Windows.

**Native-engine regression coverage:** the automated Go test suite additionally runs the actual Damocline engine/scheduler against independently counted 1/4/64 KiB HTTP fixtures, a controlled latency-tail fixture and a connection-close fixture, with a bounded 5 journey starts/s for two seconds each. These are regression cases and are not an independently calibrated throughput envelope. The transport matrix CLI remains an independent check and does not run the scheduler.

**Scientific boundary:** the CLI transport matrix checks the Go transport and fixture semantics; it **does not run the Damocline engine's scheduler**, nor quantify TLS setup precision, real DNS latency, HTTP/2, a remote network, streaming under production load, or a maximum rate. It does not expand the baseline `preflight` fingerprint or certify any additional application workload. Dynamic payload distributions, connect/DNS faults and validated generator delivery for these new combinations are future G0-03 work.

## Repeated end-to-end G0 acceptance

For one decision over the complete local reference set, use the [`damocline-acceptance` workflow](../README.md#run-the-repeated-g0-acceptance-lab). Each repeat launches a fresh calibrator process and independent child-fixture ledger. The acceptance rules require the exact schema-v6 five-shape, two-policy, three-rate matrix, its reported client arrival/body/status ledgers, per-profile independent server request, byte, and accepted TCP connection counters, aggregate healthy counters, all 2/5/10 arrivals/s stages (plus the exact extended rates when selected), a 300-request `VALID_WITHIN_ENVELOPE` soak whose persisted quality recomputes exactly, a bounded worker-exhaustion case, the induced HTTP-failure case, and all ten exact transport-matrix definitions.

The command requires two independent process runs (three at most for the baseline matrix, two for extended), compares fixture fingerprints and calibrator runner identity, reports descriptive per-profile p95 spread, and includes the transport fixture result. A failed process, gate, timeout, or interruption prevents a pass. Its fresh evidence directory contains incremental state JSON, each produced calibration/transport report, hashes, and process logs streamed to disk with a 1 MiB per-stream bound. A missing report stays explicitly inconclusive; a failed report remains available with the gate observations. The p95 range is not a confidence interval, and sequential runs on one host do not establish statistical independence or a hardware-class tolerance. A pass applies only to the specific local binary, runtime, fixture, workload tuples, and observation windows recorded in that manifest.

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
