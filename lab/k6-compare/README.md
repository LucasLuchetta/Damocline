# Independent comparison kit (k6 and access logs)

This kit lets you falsify a Damocline conclusion on any server you are
authorized to test, using evidence Damocline did not produce. It has two
offline checks; neither one sends traffic.

## 1. Access-log cross-check

Enable an access log on the target (or its reverse proxy). For nginx, append
`$request_time` to the combined format so server-side duration is available:

```nginx
log_format damocline '$remote_addr - $remote_user [$time_local] "$request" '
                     '$status $body_bytes_sent "$http_referer" "$http_user_agent" $request_time';
access_log /var/log/nginx/damocline.log damocline;
```

Run Damocline as usual, copy the log slice to the generator machine, then:

```sh
damocline crosscheck --run run.json --access-log damocline.log --format nginx \
  --path-map /app/=/ --out crosscheck.json --report-out crosscheck.md
```

Formats: `nginx` (combined + trailing `$request_time` seconds), `combined`
(no duration), `json` (one object per line: `time`, `method`, `path`/`uri`,
`status`, optional `duration_ms` or `request_time`). `--path-map` rewrites
a log path prefix to the scenario prefix when a proxy strips or adds one.

Per step and path it reports request count, 2xx/3xx/4xx/5xx counts and p95
duration as `MATCH`, `MISMATCH` (with an explanation) or `NOT_ASSESSABLE`.
Damocline requests that never got a response widen only the upper bound. A
server p95 above the client p95 is a mismatch: a client cannot see less time
than the server spent. Matching entries outside the run window flag clock
skew. Keep the generator and server on NTP; `--clock-tolerance` (default 2s)
covers one-second log resolution. Exit code 1 means a mismatch.

## 2. k6 comparison

Convert a single-workflow closed-loop scenario into an equivalent k6 script:

```sh
go run ./lab/k6-compare/scenario-to-k6 scenario.json > compare.k6.js
```

The script uses the same methods, paths, VUs, duration, request timeout,
think time between steps and success rule (`expected_status`, else 2xx, plus
`must_contain`). It does not follow redirects. The base URL comes from
`BASE_URL`. Header values never appear in the script: a value of the form
`${NAME}` reads env `NAME`, and any other header reads
`K6_H_<step index>_<HEADER>`. Profiles, arrival mode, extraction, bodies,
JSON assertions, CSV data and variable think time are refused, not
approximated. `ramp_up` is not reproduced, so set it to `0s` for comparisons.

```sh
BASE_URL=https://app.example.com k6 run --summary-export k6.json compare.k6.js
damocline compare-external --damocline run.json --k6-summary k6.json --report-out compare.md
```

Results:

- `CONSISTENT`: within tolerance (throughput 10%, error rate 0.01, p95 15%).
- `EXPLAINABLE_DIFFERENCE`: expected from definitions. For example, k6
  `http_req_duration` excludes blocked, DNS, connect and TLS time.
- `RED_FLAG`: Damocline approved a level where k6 fails the same criteria,
  or k6 delivered materially more throughput at the same VUs. That points to
  a Damocline generator limit. Exit code 1.
- `NOT_COMPARABLE`: different VU counts, a non-closed mode or missing metrics.

## Generator on a separate, monitored machine

1. Put the generator on a host that is not the target, on the same network
   path real users or the proxy use. Do not share CPU with the application.
2. Run Damocline and k6 one after the other, never at the same time, against
   the same target state. Reset or warm data the same way before each run.
3. Monitor the generator host while each run executes (for example
   `vmstat 1` or `pidstat 1` on Linux). Damocline also records its own
   generator health. A generator above about 80% CPU or with dropped starts
   invalidates the comparison.
4. Keep the generator and server clocks synchronized with NTP, then run
   `crosscheck` against the server log for each run.
5. Treat any `MISMATCH` or `RED_FLAG` as a blocker for reporting capacity
   until it is explained.

Limitations: these tools compare counters and percentiles. They do not prove
a root cause. Pooled percentiles from different estimators can differ by a
few percent even with identical traffic.

## WordPress published-post baseline

Run `python lab/k6-compare/verify-wordpress-baseline.py` from the repository
root with Python 3, Go and an amd64 Linux Docker engine with Compose. The
script starts a disposable WordPress and MariaDB project, inserts one
synthetic published post, and uses the WordPress CLI to verify persisted
content before and after the experiment.

The two generators run sequentially with five identical warm-up reads each.
Both use one closed-loop VU, 30 seconds, a 10 requests/s ceiling and a
two-second request timeout. A successful read requires HTTP 200 and the
published post marker in the response body. Damocline baseline probes use
`/wp-login.php`. Each work route ledger is sliced from independent Apache
logs between snapshots taken immediately before and after the measured run.

The baseline requires exact agreement between each client's request count
and its Apache slice, at least 250 work requests per client, and at most 30
requests difference between clients. It requires zero HTTP or business
check failures, a 500 ms p95 objective, and no red flag from the external
comparator. Each client p95 must be within the larger of 10 ms or 25% of the
larger p95 compared with its Apache slice. Client and server p95 use
different timing boundaries.

Evidence remains in gitignored `.damocline/rq3-wordpress-*/`, including the
scenario, generated k6 script, client reports, Apache slices, persisted
post check, and offline comparisons. The run checks one read operation
under an imposed request ceiling on one host. This qualifies a comparison
procedure for RQ-3 and leaves sustainable capacity, representative
multi-journey traffic and independent network environments to later phases.

### Observations

WordPress 7.1.3 with MariaDB 11.8.9 ran on a single Linux amd64 Docker
Desktop engine. Damocline ran first, followed by k6 2.3.0, in each of two
fresh Compose projects.

| Run | Client | Client requests | Apache requests | Client p95 | Apache p95 |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Damocline | 278 | 278 | 162.20 ms | 157.83 ms |
| 1 | k6 | 289 | 289 | 79.08 ms | 79.58 ms |
| 2 | Damocline | 292 | 292 | 61.29 ms | 60.42 ms |
| 2 | k6 | 297 | 297 | 40.08 ms | 39.82 ms |

Apache recorded only HTTP 200 responses on the work route. Both clients
passed the marker check on every measured request, and the persisted post
was unchanged. The Damocline access-log cross-check was `MATCH`. The
external comparator classified delivered throughput and error rate as
`CONSISTENT` and the overall comparison as `EXPLAINABLE_DIFFERENCE`.
Apache itself recorded a lower p95 in the second window, so the difference
between client p95 values cannot be attributed to timing definitions alone.
Local Go verification overlapped the runs. The source of that
window-to-window difference remains unresolved.

This is a two-run baseline for the comparison procedure. Damocline
reported limited measurement quality near the configured client ceiling
and without workload calibration. Repeated campaigns, order effects,
separate hosts, and capacity conclusions remain open.
