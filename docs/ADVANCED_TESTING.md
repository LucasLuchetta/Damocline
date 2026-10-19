# Advanced testing — native Damocline engine

Damocline is an open-source **native Go load-testing engine**. This document describes the v0.2 experimental features: native closed-loop and independent arrival-rate execution, isolated HTTP cookie jars, distinct authenticated test identities, repeated capacity-level exploration, Linux host sampling, and UTC timestamp correlation.

**Do not interpret the output as a universal maximum number of people who can log into your web app.** A capacity number applies only to the exact workload mix, data size, infrastructure, period and thresholds tested. Test only systems you own or have explicit permission to assess; use staging wherever possible.

## 1. Compile the engine

Install Go 1.24+ and from the repository root run:

```powershell
go test ./...
go test -race ./...
go build -o damocline.exe ./cmd/damocline
```

Windows preview binaries are available as CI artifacts after the PR is merged. Linux builds:

```bash
go build -o damocline ./cmd/damocline
```

## 2. Configure a scenario

Copy [`examples/advanced-readonly.json`](../examples/advanced-readonly.json) into a gitignored `myapp.local.json` and change the HTTPS origin and paths to your **own** application. Use only read-only endpoints and an unmistakable `must_contain` assertion on pages where an unauthenticated request might still return 200.

Scenario fields:

| Field | Description |
| --- | --- |
| `mode` | `closed` for concurrent virtual users or `arrival` for independent journey starts. |
| `users` | Closed-loop VUs or maximum available journey workers in arrival mode. |
| `arrival_rate` | Desired journey starts/second, only in arrival mode; capped at 100 and constrained by `max_rps` and longest journey length. |
| `max_rps` | Hard global HTTP request-start rate ceiling, maximum 100. |
| `duration` | Run duration, maximum 3 minutes. |
| `min_samples_per_step` | Minimum completed responses required per step for a passing result. |
| `p95_limit_ms` | Latency p95 ceiling applied to aggregate and individual steps. |
| `max_error_rate` | Maximum completed-request error fraction. |
| `think_time` | Delay between steps in a VU journey. |
| `think_time_range` | Optional `{ "min": "10ms", "max": "40ms" }` uniform between-step pause; maximum 30s. Use instead of `think_time` at the same level. |
| `think_time_samples` | Optional list of 2–32 durations between 0 and 30s, such as `["100ms", "100ms", "500ms"]`; repeated values carry higher relative frequency. Use instead of fixed or uniform timing at the same level. |
| `workload_seed` | Optional positive integer for reproducible timing choices; defaults to 1 when a variable policy is used. |
| `per_vu_cookie_env_prefix` | Optional prefix for independent session cookies. |
| `profiles` | Optional replacement for `steps`: 2-10 named weighted workflows, each with `name`, `weight`, `steps`, and an optional fixed, uniform, or discrete timing override. |
| `extract` | Optional map of response values to capture using exactly one `json_pointer` or `header` selector. |
| `json_assertions` | Optional map of JSON Pointer paths to expected scalar values; a mismatch fails the request even when HTTP status is successful. |
| `window_duration` | Optional 1s-60s request-metric window. Every sampled window must meet the request p95 and error SLOs. |

Use either top-level `steps` or `profiles`, never both. [`examples/weighted-profiles-readonly.json`](../examples/weighted-profiles-readonly.json) shows a three-profile read-only mix. Profile weights describe the planned mix. In closed mode, configure at least one VU per profile; in arrival mode, inspect actual profile starts in the report. HTTP redirects are not followed. A redirect to login is a failure unless explicitly listed as expected (not recommended for an authenticated page test).

[`examples/seeded-weighted-readonly.json`](../examples/seeded-weighted-readonly.json) shows three two-step workflows with a scenario timing range, a discrete-sample profile override, and a fixed-pause override. Replace its documentation origin and routes with authorized read-only endpoints before running it. The seed recreates configured pause decisions under the same actor/arrival schedule; actual elapsed timing and achieved throughput can still vary.
Every profile using variable timing needs at least two steps, because the pause occurs only between steps.

Scenario JSON files are limited to 1 MiB including trailing whitespace. Generated JSON report artifacts are limited to 16 MiB; the writer rejects larger output before creating or replacing the file. These byte caps complement the bounded VU, rate, duration, step, and response-body limits.

Optionally declare `profile_mix_basis` as `sanitized_analytics` or `business_estimate` and provide a short safe `profile_mix_reference` identifier. The default basis is `assumption`. This metadata is self-reported provenance, not proof that the mix is representative; do not put domains, paths, credentials, customer identifiers, or raw events in the reference.

A later step may use a value extracted by an earlier one in the same journey. Path variables must occupy a whole path segment; query templates may occur in parameter values and are escaped as query components. Both remain relative to the configured origin. Header and body templates use `{{variable_name}}`. Extracted values and JSON assertion values are not placed in report output.

For structured business outcomes, a step can declare, for example, `"json_assertions": {"/order/state": "ready"}`. The response must contain that scalar value at the JSON Pointer for the step to pass. Keep expected values synthetic and local. Assertion definitions change the workload fingerprint so runs with different business criteria are not treated as the same workload.

[`examples/dependent-business-readonly.json`](../examples/dependent-business-readonly.json) combines a same-journey JSON extraction, an escaped query value, and a business-state assertion. Its origin and API paths are documentation placeholders; replace them only with an authorized staging target.

## 3. Independent test users

Use a **separate test login for each virtual user**. Supply one full Cookie header per VU through local environment variables; do not store cookies in JSON or version control. With `"per_vu_cookie_env_prefix": "DAMOCLINE_VU_COOKIE_"`, 3 VUs require the following environment variables:

```powershell
$env:DAMOCLINE_VU_COOKIE_1 = '<cookie from first authorized test account>'
$env:DAMOCLINE_VU_COOKIE_2 = '<cookie from second authorized test account>'
$env:DAMOCLINE_VU_COOKIE_3 = '<cookie from third authorized test account>'
```

For a staged exploration up to 20 VUs, provide 20 **different** account cookies before starting. The CLI rejects missing or duplicate cookie values. Each worker has its own HTTP cookie jar that can receive new session cookies from the server. An HTTP request can still represent multiple steps in a business workflow; it does not measure browser rendering or WebSocket traffic.

An alternative is one shared session using the legacy `"Cookie": "${DAMOCLINE_SESSION_COOKIE}"` header in steps. This is useful for preliminary performance inspection, **not** for testing independent users. The report warns when identities may be shared.

## 4. First run: one fixed load level

```powershell
.\damocline.exe run --config .\myapp.local.json --dry-run
.\damocline.exe run --config .\myapp.local.json --authorized --out reports\baseline.json
```

When moving beyond 2 VUs, verify accounts, permissions and datasets. Keep traffic caps conservative on production. Failures, 302 redirects, dropped starts and insufficient step coverage must not be ignored.

## 5. Find repeatedly passing virtual-user levels

**Staging-first example:**

```powershell
.\damocline.exe explore --config .\myapp.local.json --min-users 2 --max-users 20 --repeats 2 --stage-duration 30s --budget 10m --authorized --out reports\capacity.json
```

The engine uses progressive doubling and bracket refinement to find a repeatedly passing tested level. Each candidate is assessed using per-step p95, error budget and sample coverage. It stops if runs fail, the local rate ceiling is binding, a user interrupts, or the time budget expires. At most 50 VUs, 100 request starts/second and 15 minutes overall are allowed by these experimental safeguards; these are *not recommended production settings*. Start with 2–5 VUs, and increase after reviewing system health.

If a passing run approaches the configured `max_rps` cap, exploration is marked **INCONCLUSIVE**, because the traffic governor may be limiting the test instead of the application. No CPU/RAM root cause is inferred from an HTTP response alone.

## 6. Independent arrival-rate experiment

Set `"mode": "arrival"`, `"arrival_rate": 5`, `"users": 10`, and `"max_rps": 20`. The scheduler attempts five journey starts per second, independently of response completion; only ten workers are available to handle them. If all workers are busy, arrivals are counted as dropped and the run fails instead of silently reporting a lower effective workload.

This is distinct from 10 concurrent users. It answers a different capacity question. `explore` is deliberately restricted to closed-loop user counts; arrival-rate tests use `run`.

## 7. Repeated phased campaign

Use a campaign when a short exploratory probe is insufficient. Each trial runs warm-up, an evaluated observation window, and a recovery window. Warm-up establishes workload state but cannot make a campaign pass; both observation and recovery must pass in every repeat.

```powershell
.\damocline.exe campaign --config .\myapp.local.json --warmup 30s --observation 5m --spike-users 20 --spike-duration 30s --recovery 30s --repeats 2 --authorized --out reports\campaign.json
```

The whole campaign is bounded to 15 minutes. Observation can last up to 10 minutes; warm-up, spike, and recovery are bounded to 3 minutes. An optional spike must use more VUs than the base scenario and stays inside the existing 50-VU safety ceiling. The campaign stops at the first non-passing trial. The default wall-time cap includes up to 30 seconds of drain per scheduled phase and repeat; an explicit `--budget` may be shorter, in which case it can abort during drain. An aborted trial records whether the campaign deadline expired or its caller interrupted it. The dry run prints the effective cap and every planned trial/phase with its duration and VU level.

Configure `window_duration` in the scenario to control request p95 and error windows. When omitted, the campaign creates at least four windows per non-warm-up phase, each no longer than 30 seconds. Schema-v2 reports distinguish observed PASS/FAIL from invalid delivery and incomplete evidence. Observation requires at least three complete windows with 20 requests each: a first-to-last p95 rise above both 20% and 5 ms, or throughput loss above 20%, is labeled `NON_STATIONARY` and makes the campaign inconclusive. A passing screen means only that this large drift was not detected. It does not establish stationarity or sustainable capacity.

Assess a completed campaign against an owner demand target without making HTTP
requests:

```powershell
.\damocline.exe plan --campaign reports\campaign.json --peak-users 200 --growth-percent 30 --reserve-percent 30
```

`plan` accepts only a complete repeated campaign report with passing
observation and recovery phases. It calculates the required synthetic
workload-equivalent actor count and reports whether the recorded base VU level
exactly matched that target **under the saved test conditions**. It is not a prediction of
server capacity, a diagnosis of the limiting component, or a recommendation to
buy infrastructure. Schema-v2 evidence also requires the exact planned phase order, load, duration, target, classification and recomputed drift screen. Older schema-v1 reports remain readable but cannot support a positive demand conclusion.

## Synthetic write data and explicit cleanup

For an authorized write workflow, declare an environment-backed data prefix and
use `{{synthetic_id}}` in complete path segments, headers, or `body_env`
content. Damocline generates a fresh identifier for every journey and never
writes that identifier to a report.

```powershell
$env:DAMOCLINE_TEST_DATA_PREFIX = "damocline-check"
.\damocline.exe run --config .\write-workflow.json --authorized --allow-writes
```

```json
{
  "synthetic_data_prefix_env": "DAMOCLINE_TEST_DATA_PREFIX",
  "steps": [
    {"name": "create", "method": "POST", "path": "/records/{{synthetic_id}}"},
    {"name": "cleanup", "method": "DELETE", "path": "/records/{{synthetic_id}}"}
  ]
}
```

Cleanup is an explicit workflow step, so a failed or cancelled journey can
leave test data behind. Use an isolated test namespace, validate cleanup in
staging, and retain an application-specific cleanup procedure; Damocline does
not delete resources outside the configured, authorized journey.

## 8. Monitor the Linux VPS independently

Run Damocline natively on your Linux host (not inside a container whose procfs could refer to other scopes). The following collects host-wide CPU and available-memory estimates without opening any network port, launching Docker or requiring elevated privileges:

```bash
./damocline monitor --duration 10m --interval 2s --out reports/host.json
```

At the same time, execute the load from an **external machine**, not the VPS. Keep host and generator clocks synchronized with NTP. Copy the host report to your development machine over your existing authenticated SSH connection (do not expose a public metrics endpoint).

To correlate host samples against staged loads:

```powershell
.\damocline.exe correlate --load reports\capacity.json --host reports\host.json --out reports\correlated.json
```

The correlation report shows mean/peak CPU and peak memory use during individual trials. It does **not** measure per-container usage, PostgreSQL waits, Redis, application traces or prove causality. Consider collecting `docker stats` and database metrics independently for further diagnosis.

## 9. Collect application metrics explicitly

For an application that exposes a permitted Prometheus-text metric endpoint, take a bounded snapshot with explicit authorization:

```powershell
.\damocline.exe metrics --endpoint https://app.example.com/metrics --authorized --out reports\application-metrics.json
```

The collector accepts only HTTP(S), follows no redirects, caps the response body, aggregates labeled samples by metric name, and does not preserve labels or credentials. It is a point-in-time observation. Correlate it with a named campaign manually; a high queue, error, or latency metric does not by itself prove which component caused a bottleneck.

For local Docker container snapshots, run `damocline docker --authorized --out reports/docker.json`. It invokes one bounded `docker stats --no-stream` call and records CPU and memory observations. Docker availability, access permissions, database waits, queue state, and application traces remain separate concerns.

## 10. Inspect a saved report locally

```powershell
.\damocline.exe dashboard --file reports\run.json
```

The dashboard binds only to `127.0.0.1` (or `localhost`/`::1`), reads one existing report, and displays the saved outcome, request metrics, steps, and observation windows. When the report has observation windows, it draws aggregate request p95/p99 by window and lists request and journey success/failure counts. Dropped starts and generator resource peaks are shown only as run-wide context because the report does not retain them per window. It makes no load requests and does not expose the report to the network.

## 8. Limitations and quality checks

- A high VU count with a low RPS cap **does not prove** the server can support that count of active people.
- A single p95 is not enough: compare per-step measurements, error categories, and completed journeys.
- Different routes and workloads compete for shared resources. Do not add per-page user counts.
- Closed-loop VUs slow down their request rate when the application slows; use arrival mode to study offered traffic and dropped scheduling.
- Synthetic sessions with shared cookies are not independent users.
- Host CPU saturation correlating with response latency is a hypothesis, not an established bottleneck cause.
- The engine still lacks browser measurement, automatic login/CSRF handling, synthetic-data lifecycle controls, continuous per-container metrics, and calibrated capacity or generator limits. Weights are a declared synthetic assumption until validated against sanitized usage evidence. Go runtime and supported Windows/Linux process health samples are available, but the error-rate confidence bound assumes independent, stationary outcomes and does not describe percentile or run-to-run uncertainty. Read the [scientific foundations and validation contract](SCIENTIFIC_FOUNDATIONS.md) before interpreting a capacity result.
