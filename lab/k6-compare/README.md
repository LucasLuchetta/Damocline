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
- `NOT_COMPARABLE`: different VU counts, a non-closed mode, missing
  metrics, or incomplete or unverified business checks.

The generated script labels each check with the workload fingerprint and
step index when it can reproduce the scenario. The comparison can report
`k6_would_pass=true` only when those labels match the Damocline report,
each step has enough checks, and the aggregate and per-step check counts
reconcile to one passing check per HTTP request. Use a k6 summary that
includes both `metrics.checks` and `root_group.checks`. The default
`--summary-export` or an unfiltered `handleSummary(data)` can retain
these fields.

A reported failed check, HTTP error rate above the limit, or p95 above the
limit sets `k6_would_pass=false`. A legacy summary can still report a
failure but cannot establish a passing business verdict without the
matching generated checks. When the evidence is incomplete, the JSON
omits `k6_would_pass` and the Markdown report says it is not assessable.
Check labels identify the generator's declared rules. They do not
authenticate the summary, verify environment values, or establish the
representativeness of the target.

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
