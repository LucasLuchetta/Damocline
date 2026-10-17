# Local reference validation

Damocline includes a bounded HTTP fixture with known response delay, concurrency, response codes, and counters. The calibration command runs that fixture in a separate child process over loopback and compares server-side counters with the engine report. Both processes still share the same host.

Run the default smoke matrix from the repository root:

```powershell
go run ./cmd/damocline-calibrate --out .damocline/calibration/reference.json
```

The command has a 160-second hard deadline and includes a 30-second sustained stage at 10 journey arrivals per second. It runs three five-second healthy stages at 2, 5, and 10 arrivals per second, then the soak, a deliberately slow single-worker stage, and a known-error/recovery stage. In the healthy matrix, each stage has exactly `rate * 5` scheduled arrivals: 10, 25, and 50 (85 total). The soak must reconcile all 300 arrivals and retain at least 100 generator/process samples. The controlled error stage returns two HTTP 503 responses followed by eight successes. The overload stage must report worker-busy drops, refuse PASS, and reconcile requests that were received, completed, or cancelled at the run boundary.

The reference response delay is 20 ms with a 4 KiB body. The check accepts observed request and journey p50/p95 values between 15 and 150 ms, scheduler p95 lag below half of the arrival interval, and maximum lag below one interval. It rejects sampled generator CPU at or above 90%, heap above 256 MiB, or process RSS above 512 MiB. These broad bounds catch gross timing/accounting mistakes and an obviously saturated generator; they are not calibrated precision tolerances or universal performance guarantees. Go-runtime and OS process CPU/RSS samples must be present on Windows/Linux. Their readings describe only the generator; CPU is normalized to its GOMAXPROCS value, not the host or target.

`REFERENCE_SMOKE_PASSED` means only that these bounded local cases reconciled on this host. It does not establish maximum generator throughput, cross-platform accuracy, sustained capacity, or application capacity. Keep the JSON private under the gitignored `.damocline/` directory; it records local environment metadata and timing evidence.

## Versioned local preflight metadata (G0-01)

The same calibration report embeds a `preflight` object (schema version `1`), preserving the existing top-level calibration schema and traffic limits. It records the generator OS/arch, logical CPUs, GOMAXPROCS, Go version and runner source revision; UTC test start/end; same-host, separate-process loopback fixture mode; GET-only arrivals, 4 KiB healthy response, 20 ms delay, 5-second stages, 30-second soak, configured rates/repeats; and each healthy stage's scheduled, started, completed, dropped and outcome counts.

The `scenario_fingerprint_sha256` hashes **only** this fixed, non-secret fixture configuration. It is not a tamper-proof signature or a fingerprint of a customer workload. The `highest_passing_tested_arrival_rate` appears as a positive value only when **the entire** reference check passed, and describes the highest successful **tested** rate—not the generator's maximum capacity.

The new metadata excludes temporary loopback ports, customer hostnames, request paths, credentials, headers, tokens, environment-variable values, and bodies. The local artifact must remain private: other sections of the existing report still include host measurements. Regression tests cover schema round-trips, unknown/incompatible versions, invalid fixture fingerprints, unsafe version metadata, failed-stage handling, and secret-marker absence.

**Not a client-workload readiness certificate:** a passing 4 KiB loopback reference cannot verify unrelated HTTPS traffic, payload sizes, multi-step sessions, or real client networks. Workload equivalence, artifact freshness and host matching remain explicitly open under G0-02/G0-08.

Repeat the healthy matrix three times to inspect short-run variability:

```powershell
go run ./cmd/damocline-calibrate --repeats 3 --out .damocline/calibration/reference-repeats.json
```

This remains a bounded engineering check. Compare every run at each rate; do not choose the fastest repeat or treat three repeats as a sufficient statistical sample for a client recommendation. The optional `--extended` mode adds 15, 20, 50, and 100 arrivals/s and allows at most two repetitions within the hard deadline:

```powershell
go run ./cmd/damocline-calibrate --extended --out .damocline/calibration/reference-extended.json
```

The scheduler also has deterministic tests that fault-inject a callback stall and require late arrivals and lag to be counted. Engine tests ensure insufficient samples cannot pass, admitted work drains within a bounded grace window, cancelled requests cannot pass, and responses above the 2 MiB body limit fail visibly. Schema v4 reports both the configured workload observation window and full elapsed wall-clock time, including drain; throughput uses the observation window. The local reference server tests fixed payload sizes, bounded concurrency, failures, queueing, and recovery. CI runs the extended matrix on Linux and Windows. These checks establish the tested operating envelope through 100 arrivals/s, a 4 KiB response, and a 30-second soak on hosts where the matrix is run. They do not establish the maximum possible generator throughput, precision on untested machines or payload sizes, or a client-capacity recommendation. Repeat the matrix on each deployment host class before using it to interpret a capacity campaign.
