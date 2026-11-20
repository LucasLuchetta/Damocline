# Closed-loop comparison with k6

From the repository root on Windows, with Go and k6 on PATH, run:

```powershell
./lab/k6/compare.ps1 -OutputDirectory .damocline/k6-fixture-run
```

Create the parent directory first. Each run requires a fresh output directory.
The runner builds both Go commands and starts its own read-only fixture on
`127.0.0.1:18086`. No Docker service or external target is needed.

At each of 1, 4, and 20 VUs, both tools repeatedly send two sequential GETs
with a one-second pause after the first. Both use a three-second request
timeout, a 20-second nominal load interval, a 100 RPS ceiling, and connection
reuse. The fixture has two work slots, six admitted requests and a 200 ms
service delay. Queue overflow produces HTTP 503. Both clients assert status
and response text and continue through the second step after HTTP errors.

Compare request counts, delivered requests per second, p50/p95/p99 and
assertion-aware error fractions. Use k6's `work_duration` and
`work_failures` metrics. The JS wall clock has millisecond resolution and
can include connection or governor waits. Damocline measures its request
interval with Go's duration clock. The native and k6 quantile estimators,
connection pools, rate limiters and drain intervals can differ. Preserve
the recorded denominators and include the counts when discussing differences.
Fast 503s can lower a pooled percentile while successful requests queue.
Both tests are closed-loop, so slower journeys reduce the offered load without
counting the starts that an independent arrival process would have scheduled.

This fixture tests the comparison procedure. A single serial pair per level
cannot establish repeatability or support an application capacity claim.
