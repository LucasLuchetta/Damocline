# Understand your results — CLI report explainer

Damocline deliberately distinguishes a **passing workload measurement** from a demonstrated **server capacity**. A single fast page test cannot prove your infrastructure has spare resources or needs an upgrade.

## Read an existing report

From your cloned, source-based installation:

```powershell
damocline report --file .damocline/reports/your-run.json
```

Compare two runs without repeating them:

```powershell
damocline report --file .damocline/reports/later-run.json --compare .damocline/reports/earlier-run.json
```

If you independently captured Linux host telemetry using `damocline monitor`, add the optional host JSON:

```powershell
damocline report --file .damocline/reports/your-run.json --host host.json
```

The command is entirely offline: it does **not** call or probe the target application.

## Understand the four answers

```text
--- DAMOCLINE ASSESSMENT ---
PERFORMANCE:    OBJECTIVES MET IN THIS RUN (PASS)
USER CAPACITY:  NOT DETERMINED
SERVER UPGRADE: NOT JUSTIFIED BY THIS RUN ALONE
HOST METRICS:   NOT MEASURED
```

- **Performance** reflects the current run's recorded outcome, latency objectives and error allowance. `PASS` is not a guarantee for the future.
- **User capacity** cannot be inferred from a fixed test with one or a few virtual users. Use repeated load stages and representative workflows; don't equate VUs with distinct authenticated people unless sessions really are distinct.
- **Server upgrade** is **not justified by a single HTTP report**. Even high host CPU, when available, is a reason to investigate utilization and container/database metrics—not automatic proof you must purchase a larger server.
- **Host metrics** indicate whether time-aligned Linux CPU/RAM samples were supplied. Without synchronized host metrics the tool cannot determine resource headroom.

Each assessment also highlights:

1. **HTTP request p95** against the configured request-latency objective, separately from **complete journey p95** (which includes think-time).
2. Errors, number of samples, intermittent long-tail responses, cancelled operations and missed arrivals.
3. Achieved requests per second compared with the **configured maximum** (`max_rps` is a cap, not actual throughput).
4. Differences in duration, rate caps and achieved throughput that prevent treating two results as a controlled comparison.
5. Suggested next experiments to answer the still-open capacity question.

The error-rate insight includes a one-sided 95% upper bound: exact for zero observed failures and a Wilson score approximation otherwise. It assumes independent, stationary outcomes, which may not hold for correlated requests. It does not certify the configured error budget unless the bound itself is below that budget. Request and completed-journey latency percentiles are point estimates without confidence intervals. A journey interrupted at shutdown is not included in completed-journey latency statistics; inspect cancellation counts and treat unfinished work as a limitation.

## What an assessment cannot prove

The report command is a deterministic explanation layer over existing measurements. It does **not** invent CPU, RAM, PostgreSQL, Redis or Docker metrics. It does not use AI, access private credentials, contact hosted services, or automatically upgrade your server.

A passing workload may still have long-tail spikes. A failed workload may be caused by application errors, a misconfigured scenario, exhausted test accounts, dropped arrivals, or generator limits. Interpret these causes using independent evidence.

The local `.damocline/` folder is ignored by Git. Do not commit actual customer targets, scenario files, authentication materials, or private reports to the public repository. Public examples must use reserved documentation domains and synthetic fixtures.
