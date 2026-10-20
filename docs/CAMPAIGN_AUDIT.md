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
business-demand check, pass `--peak-users` (and optional growth and reserve
percentages) to `audit`. The combined JSON assessment contains the observed
audit and demand plan in one artifact, including the original peak/growth/reserve
inputs; `plan` remains available for a standalone demand check. `--report-out FILE.md` writes executive and technical sections from that same assessment and
records an offline replay command pinned to each source campaign's SHA-256
evidence ID. You can pin an audit explicitly with repeated
`--expect-evidence-id sha256:<64-hex>` flags, one per `--campaign` in argument
order. The audit stops before writing assessment outputs if any source digest
differs. A digest identifies normalized evidence fields; it is not a signature or proof of who
produced them.

Each assessment includes a SHA-256 digest of the normalized, schema-decoded
campaign evidence and each reviewed phase report, the workload fingerprint
recorded by each run, target, timestamps and observation durations, the sum of required phase durations,
observed levels, workflow failures visible in observation evidence, and known
unknowns. These digests identify the evidence fields interpreted by this
auditor; they do not authenticate who produced it or protect it from
replacement. Confidence
is explicitly `NOT_ESTABLISHED`: current conditional intervals assume IID
observations and do not establish dependence-aware confidence. Demand is not
extrapolated into untested workload levels.

`workflow_breaches` identifies each trial, required phase, tested VU level,
workflow, breached p95/error objective, and observed values. A failure count
inside the configured error budget is not labeled a breach. These observations
describe which workflow metric crossed an SLO; they do not establish a root
cause. Invalid evidence suppresses workflow-breach details.

The Markdown report records phase-level controls, source and phase evidence
IDs, observation statistics, confidence limits and a replay command. It includes
the target origin and may contain private scenario labels, so keep it with local
reports. It does not embed the full scenario or credentials. Preserve the private
profile and use `campaign profile verify` to check that profile against the saved
campaign before rerunning the workload. The generated replay command verifies
the campaign evidence ID before it recomputes the assessment; this catches
changes to interpreted source fields but does not authenticate their origin.

To inspect separately recorded campaigns at multiple active-workload levels,
repeat `--campaign`:

```powershell
damocline audit --campaign reports/campaign-2-vus.json --campaign reports/campaign-4-vus.json --campaign reports/campaign-8-vus.json --peak-users 4 --out reports/level-assessment.json
```

The collection retains a full audit and evidence digest for each level. It
aggregates pass/fail observations only when all campaigns have the same
non-empty workload fingerprint, target, environment and application/workload
revisions, SLOs, timing, request controls, phase schedule and repeat count;
the configured spike proportion must also match. This is a match of declared
conditions, not proof of physical host identity or actual demand equivalence.
Missing or pre-schema-v14 fingerprints/connection policy, absent workload,
application or environment revisions, or any changed control yield
`NOT_COMPARABLE`, while the individual level audits remain available. Patterns
such as `PASS_BELOW_FAIL`, `NO_FAILURE_OBSERVED`, and `NON_MONOTONIC` describe
only recorded outcomes. A pass at a higher level after a failure at a lower
level is labeled `NON_MONOTONIC`; the tool will not infer a capacity threshold.
A demand target still requires a campaign at its exact
calculated level and is never interpolated from neighboring campaigns.

A campaign that stops at its first failing required phase can still contribute
that observed failure. The audit replays the terminal trial against the exact
planned phase prefix; unrun recovery or later phases are not filled in. An
incomplete or aborted level is inconclusive unless its saved terminal evidence
supports a specific observed SLO failure.

Collection input is bounded to 20 files, 16 MiB per campaign, and 64 MiB total.
This command is offline and never sends traffic.

The machine-readable assessment also keeps three dimensions separate:
measurement evidence (`LIMITED`, `INVALID`, or `CONTROLLED_ENVELOPE_ONLY`),
workload representativeness (`NOT_ASSESSED`, `DECLARED_UNVERIFIED`, or
`INCONSISTENT_DECLARATION`), and decision confidence (`NOT_ESTABLISHED` unless
the evidence itself is invalid). A profile-mix label is only a declaration;
it does not independently verify real demand. `CONTROLLED_ENVELOPE_ONLY` is a
measurement-envelope status, not a statistical confidence interval. It does
not make an unrepresentative workload representative or create decision
confidence.

## Stricter observation window screen

The G2 coarse screen now inspects **each** complete observation window rather
than only the first and last. For runs with named steps or weighted journeys
it also checks every named step/profile separately. A single minority
workflow can no longer conceal a large transient regression behind a stable
aggregate. Every named series needs the configured minimum number of samples
in each window for this **coarse** screen; schema v5 records that minimum and
bounds it to 20-100000, while historical campaign schemas use 20. Otherwise
the screen is `INSUFFICIENT_WINDOWS`.

A latency p95 increase over both the saved percentage and absolute thresholds,
or throughput loss over the saved percentage threshold compared with the first
window, triggers `NON_STATIONARY`. Schema-v5 policies bound the p95 percentage
to (0,100], the absolute threshold to 0-10000 ms and throughput decrease to
(0,100]. Defaults remain 20% and 5 ms for p95 and 20% for throughput. The
offline reader recomputes classifications using the saved policy. These are
provisional heuristic guardrails, not a statistical stationarity test,
confidence interval or guaranteed detection threshold. Short or uneven
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
.\.damocline\damocline-audit.exe audit --campaign .damocline\reports\campaign.json --peak-users 8 --out .damocline\reports\campaign-assessment.json
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
