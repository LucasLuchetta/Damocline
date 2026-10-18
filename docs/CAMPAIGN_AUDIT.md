# Campaign evidence audit (offline)

This feature joins G0 evidence accounting, G2 window/drift screening and G3
interpretation without implying that an external application's user capacity
has been certified. It does **not** change the native Go load generator or
raise any traffic ceilings.

## What is replayed

`damocline audit` reads an existing bounded campaign JSON and **sends no
requests**. It rechecks the supported campaign structure, each recorded
stage's delivery classification, the observation-window drift classification,
and the measurement quality recomputed from the saved report. Stale or altered
stage labels and self-declared positive quality are flagged as
`INVALID_EVIDENCE`. Missing telemetry is not filled in or treated as zero.

The audit distinguishes:

- `OBSERVED_PASS_ONLY`: recorded required stages passed their stated SLOs,
  but this is not an endorsed capacity or generator equivalence conclusion.
- `OBSERVED_FAIL`: a configured objective was observed to fail; it is not
  proof that the application, rather than the generator, was the bottleneck.
- `INCONCLUSIVE`: incomplete phases, invalid delivery, unsupported evidence
  or unstable windows prevent a clean observed limit.
- `INVALID_EVIDENCE`: malformed or internally contradictory persisted
  evidence, including fabricated or stale stage/quality labels.

Recomputed quality `LIMITED` remains visible *separately* from the
observational verdict. Only the separately validated loopback calibration
owns the current positive generator measurement envelope. For a specific
business-demand check, use the independent `damocline plan` command, which
retains its strict generator equivalence and statistical requirements.

## Stricter observation window screen

The G2 coarse screen now inspects **each** complete observation window rather
than only the first and last. For runs with named steps or weighted journeys
it also checks every named step/profile separately. A single minority
workflow can no longer conceal a large transient regression behind a stable
aggregate. Every named series needs at least 20 samples in each window for
this **coarse** screen; otherwise the screen is `INSUFFICIENT_WINDOWS`.

A latency p95 increase of over 20% **and** over 5 ms, or throughput loss
over 20% compared with the first window, triggers `NON_STATIONARY`.
These are provisional heuristic guardrails, not a statistical stationarity
test, confidence interval or guaranteed detection threshold. Short or uneven
workloads may become inconclusive, intentionally.

## Windows PowerShell validation

After checking out the PR branch in a separate worktree or clean checkout:

```powershell
go version
New-Item -ItemType Directory -Force .damocline | Out-Null
go test ./internal/campaign ./internal/capacityplan ./internal/cli
go test -race ./...
go vet ./...
go build -o .damocline/damocline-audit.exe ./cmd/damocline

# Local fixture only; no production traffic:
go test ./internal/campaign -run 'TestStability' -v
go test ./internal/capacityplan -run 'TestAuditCampaign' -v

# Review an existing campaign report without contacting its target:
.\.damocline\damocline-audit.exe audit --campaign .damocline\reports\campaign.json --out .damocline\reports\campaign-audit.json
```

To preview a new bounded campaign without generating traffic:

```powershell
.\.damocline\damocline-audit.exe campaign --config examples/local-smoke.json --warmup 0s --observation 10s --recovery 10s --repeats 2 --dry-run
```

The original `examples/local-smoke.json` is intentionally tiny (2 VUs,
5 max RPS). At that rate, a 2.5-second campaign window cannot reach the
required 20 samples: `INSUFFICIENT_WINDOWS` and an early
`INCONCLUSIVE` result are **expected**, even for 51/51 successful HTTP
requests. The smoke scenario remains useful for proving the transport path.

For a local-only campaign capable of filling observation windows, use the
separate `examples/local-campaign-audit.json` scenario. It uses 8 synthetic
actors with 300ms pauses, 60 max RPS, GET-only traffic to loopback. Its actual
request count is host-dependent and **not guaranteed**; inspect the windows
if an observation is inconclusive. In two PowerShell sessions:

```powershell
# Session A: disposable localhost reference; stop with Ctrl+C.
py -m http.server 18080 --bind 127.0.0.1

# Session B: run from the repository root. No manually named .exe needed.
New-Item -ItemType Directory -Force .damocline/reports | Out-Null
go run ./cmd/damocline campaign --config examples/local-campaign-audit.json --warmup 0s --observation 10s --recovery 10s --repeats 2 --authorized --out .damocline/reports/campaign.json
go run ./cmd/damocline audit --campaign .damocline/reports/campaign.json --out .damocline/reports/audit.json
```

The report's `PASS_OBSERVED` and the audit's `OBSERVED_PASS_ONLY`
describe synthetic localhost observations, **not** verified application
capacity. `LIMITED` remains the expected measurement-quality level for
ordinary runs. If the server has limited capacity or a Windows security tool
interferes, the audit may still be inconclusive.

The local scenario example is a **preview**, not a validated sustained test.
For an actual campaign, use a disposable or staging application that you own,
confirm the read-only routes, set up realistic independent identities, observe
the generator and the server separately, and explicitly pass `--authorized`.
Do not use production as the initial target.

See [G0 status](G0_STATUS.md), [roadmap](ROADMAP.md),
[measurement model](METRICS.md), and [scientific foundations](SCIENTIFIC_FOUNDATIONS.md).
