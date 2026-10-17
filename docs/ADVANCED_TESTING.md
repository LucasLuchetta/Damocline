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
| `arrival_rate` | Desired journey starts/second, only in arrival mode; capped at 20. |
| `max_rps` | Hard global HTTP request-start rate ceiling, maximum 100. |
| `duration` | Run duration, maximum 3 minutes. |
| `min_samples_per_step` | Minimum completed responses required per step for a passing result. |
| `p95_limit_ms` | Latency p95 ceiling applied to aggregate and individual steps. |
| `max_error_rate` | Maximum completed-request error fraction. |
| `think_time` | Delay between steps in a VU journey. |
| `per_vu_cookie_env_prefix` | Optional prefix for independent session cookies. |

HTTP redirects are not followed. A redirect to login is a failure unless explicitly listed as expected (not recommended for an authenticated page test).

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

## 7. Monitor the Linux VPS independently

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

## 8. Limitations and quality checks

- A high VU count with a low RPS cap **does not prove** the server can support that count of active people.
- A single p95 is not enough: compare per-step measurements, error categories, and completed journeys.
- Different routes and workloads compete for shared resources. Do not add per-page user counts.
- Closed-loop VUs slow down their request rate when the application slows; use arrival mode to study offered traffic and dropped scheduling.
- Synthetic sessions with shared cookies are not independent users.
- Host CPU saturation correlating with response latency is a hypothesis, not an established bottleneck cause.
- The engine still lacks browser measurement, auto-login/CSRF token extraction, memory/CPU profiling of the load generator, per-container metrics and fully calibrated confidence intervals. Do not use it for published capacity claims without independent validation.
