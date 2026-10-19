<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/damocline-horizontal-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="assets/brand/damocline-horizontal-light.png" />
    <img src="assets/brand/damocline-horizontal-light.png" alt="Damocline — Find the limit before it finds you." width="760" />
  </picture>
</p>

[![CI](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml/badge.svg)](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml)
[![Apache License 2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Status: pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange)](docs/ROADMAP.md)

**Damocline** is an open-source, local-first application capacity explorer. It aims to help developers discover how many active users can complete a given workflow within a performance budget, identify where capacity becomes constrained, and understand what the available infrastructure metrics actually show.

The name evokes the **Sword of Damocles**: apparently stable systems can have limits hanging overhead. Damocline aims to find those limits *before users do*.

> [!IMPORTANT]
> **Experimental advanced HTTP engine:** native closed-loop and arrival-rate scheduling, repeated virtual-user capacity exploration, independent cookie sessions, Linux CPU/RAM sampling, and UTC report correlation are available. Manual temporary browser login is experimental. Automatic login replay, full-browser load tests, per-container telemetry, and universal generator-ceiling certification are not implemented. A passing test is **not** a universal maximum-users claim.

The CLI also provides a bounded repeated `arrival-campaign` for open-loop journey-start rates. It preserves scheduled starts/s as a separate unit from worker limits, completed journeys/s, and HTTP requests/s; current external run quality still does not certify sustainable capacity.

## The problem

Developers rarely want only a requests-per-second chart. They want to answer:

- How many **active users** can complete a checkout, dashboard, or approval workflow while meeting latency and error objectives?
- Which page or business journey limits capacity first?
- What happens when workflows share the same database, CPU, memory, and connection pools?
- Does an infrastructure upgrade or application change improve capacity in repeatable tests?

Damocline will seek **scenario-specific sustainable capacity**, never claim one universal "maximum users" number.

## Design goals

| Component | Goal | Status |
| --- | --- | --- |
| Native Go load engine | Bounded HTTP workloads with native Go scheduling | **Experimental** |
| Workload models | Closed-loop virtual users and independent journey arrivals | **Experimental** |
| Capacity explorer | Repeat passing stages, double load and refine failing brackets | **Experimental** |
| Per-workflow analysis | Named HTTP steps, full journey counts and mixed journeys | **Partial** |
| Infrastructure adapters | Native Linux host CPU/RAM monitor; Docker/cloud adapters planned | **Partial** |
| Reporting | p50/p95/p99, per-step, journeys, dropped starts, JSON and host-time correlation | **Partial** |
| Local-first CLI | Run, explore, monitor and correlate offline | **Experimental** |

Damocline should apply to **authorized HTTP/HTTPS applications** hosted on VPSs, dedicated machines, containers, Kubernetes, PaaS, or serverless platforms. Internal resource metrics require optional instrumentation or permissions; an external URL alone cannot reveal a server's CPU or database bottlenecks.

## How it is intended to work

```text
Scenario + goals + permitted target
                 |
         Capacity controller
                 |
     Native scheduler / Go engine
                 |
         Application under test
                 |
         Response measurements
                 +---------------- Optional infrastructure metrics
                 |
         Validated capacity report
```

Idle sessions, active virtual users, requests in flight, and arrivals per second are different concepts. See [Measurement model](docs/METRICS.md) for the intended definitions and reporting requirements.

## Source install: type `damocline` from any terminal

**No downloaded unsigned release executable is required.** Clone the repository with Go 1.24+ and Git installed:

```powershell
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
.\setup.cmd
```

Open a **new** PowerShell or CMD terminal. Then just type:

```powershell
damocline
```

This launches the guided setup. It asks for your authorized HTTPS origin, read-only pages, authentication, and safe load settings. Select **temporary browser login** to sign in through a fresh Edge/Chrome window: Damocline borrows the target session cookies in process memory and proposes visited pages for *manual* selection. It saves route definitions, **not passwords or cookies**. Every run still requires explicit authorization.

You can alternatively run `.\start.cmd` directly in the cloned directory, or `go run ./cmd/damocline start`. The `damocline` command registered by setup is a tiny `.cmd` launcher running the checked-out Go sources, so `git pull` updates the tool without downloading release binaries. **Go still compiles a temporary native program locally**, and Windows security software may inspect it.

Browser login session reuse **does not replay login forms**, create distinct user accounts, or measure browser rendering. Load generation uses Damocline's native Go engine.

See [Source installation and browser login](docs/SOURCE_INSTALL.md) and [Guided CLI](docs/INTERACTIVE_START.md).

## Understand what your result means

Damocline prints a human-readable **assessment** after every `run` and can explain previously saved reports offline:

```powershell
damocline report --file .damocline/reports/your-run.json
damocline report --file .damocline/reports/later-run.json --compare .damocline/reports/earlier-run.json
damocline report --file .damocline/reports/your-run.json --host host.json
```

It distinguishes **meeting your p95/error objectives** from **proving user capacity** and **having evidence that a bigger server is needed**. Achieved request throughput is not the configured `max_rps` ceiling; successful configured journeys/s are shown separately from requests/s and cancelled journeys. HTTP request latency is not complete journey duration. A single PASS cannot certify infrastructure headroom. Controlled comparisons require matching schema-v15 workload fingerprints, recorded connection policy, and declared run conditions; older or mismatched reports are labeled `NOT_COMPARABLE` because their connection policy is unknown.

See [Understand your results](docs/REPORT_EXPLAINER.md) for interpretation, sample-size and comparison warnings. The command never sends traffic.

To share aggregate evidence, use `damocline report export --file run-campaign-or-exploration.json --out redacted.json`.
This opt-in export aliases workflow and step names, removes destinations, routes,
free text, fingerprints and absolute timestamps, and emits a separate descriptive
format that is not accepted for native capacity assessment. External metric
names and values are omitted because custom telemetry may contain identifiers.

Saved run and campaign artifacts can be inspected with the loopback-only
`dashboard --file` view. For each phase it overlays HTTP-window p95 at each
window's UTC midpoint with timestamped external metric observations on a shared
UTC axis. The two series use independent value scales; points are not
interpolated and alignment does not establish causality or correct clock skew.

## Check a customer's demand goal (offline preview)

For a guided one-command capacity campaign, add an explicit demand goal to
`explore`. The explorer will attempt the **exact** required workload level
when its bounded search reaches that point, then print a cautious assessment:

```powershell
damocline explore --config myapp.local.json --min-users 2 --max-users 20 --peak-users 12 --growth-percent 25 --reserve-percent 0 --repeats 2 --authorized --out reports/capacity.json
```

Use `--dry-run` first. The required load never overrides the configured
`--max-users` safety ceiling. Neither passing short tests nor a predicted
user goal proves that the deployment can sustainably serve that many humans.

Alternatively, use a previously recorded `explore --out` report to compare an explicit
peak-demand goal, projected growth and operational reserve to the exact
synthetic load levels tested:

```powershell
damocline goal --file reports/capacity.json --peak-users 20 --growth-percent 25 --reserve-percent 20
```

This command **never sends traffic**. It returns a target load to validate and
cautious evidence labels: a short directly tested passing goal, a missed SLO
at that load, insufficient evidence, or a goal outside the search range.
It does **not** certify human-user capacity, sustained availability or a need
for new infrastructure. The synthetic VU model, realistic journey mix and
generator accuracy must still be independently validated.

## Assess a sustained campaign against the demand goal

After a repeated `campaign` has recorded its warm-up, observation, and
recovery evidence, assess the same business target without generating more
traffic:

```powershell
damocline audit --campaign reports/campaign.json --peak-users 200 --growth-percent 30 --reserve-percent 30 --out reports/assessment.json
```

`audit` replays each saved phase and writes one assessment that includes the
campaign verdict, demand calculation, content digests, workload fingerprints,
tested duration, visible workflow breaches and known unknowns. `plan` remains
available when only the demand calculation is needed. The assessment reports
measurement confidence, workload representativeness and decision confidence
separately; a declared workload mix remains unverified without independent
evidence.

To audit multiple tested levels together, repeat `--campaign` with one saved
campaign per level. The offline collection shows each level's verdict and
aggregates only matching declared workload/control signatures; an exact demand
verdict still requires a campaign at that precise VU level. It never fills in
untested levels or turns observed passes/failures into a capacity curve. See
[Campaign evidence audit](docs/CAMPAIGN_AUDIT.md).

Campaigns can include a bounded final soak with `--soak 30s`. It runs once at
the configured base workload only after every independent trial passes, uses
the existing journey and observation-window gates, and counts toward the
15-minute campaign limit. This short soak is not a long-duration stability
test or a sustainable-capacity certification.

Campaigns can also use `--stabilization 30s` to require a bounded stability
screen at the base workload before each trial's decision-bearing observation.
It runs once, does not automatically retry or wait indefinitely, and an
incomplete or drifting screen stops the trial without starting observation.
The default maximum is 3 minutes; explicitly enabled staging long-run mode
allows up to 30 minutes. A passing coarse screen is not proof of stationarity,
memory-leak absence, or sustainable capacity.

To retain external application metrics beside each phase, campaigns and single
runs accept `--app-metrics-endpoint URL --app-metrics-interval 10s`. The
timestamped samples are available in the phase's run report for manual
alignment. Scrapes add traffic outside Damocline's load counters and rate
governor; labels are discarded, credentials in URL query strings are rejected,
and the observations do not identify a bottleneck. See
[advanced testing](docs/ADVANCED_TESTING.md#9-collect-application-metrics-explicitly)
for bounds and limitations.

Closed-loop campaigns accept `--calibration FILE` to retain local generator
preflight evidence. `--require-calibration` fails before any phase unless every
planned base and spike VU level has exact closed-loop coverage. The current
schema-v9 reference matrix includes six exact short profiles at 1, 2, 5, 10, 20,
and 50 VUs. Coverage remains blocked for other VU levels, request ceilings,
durations, timeouts, connection policies, workload shapes, ramp-up, or think
time; arrival-rate profiles never stand in for VU evidence.

Reusable closed-loop campaign settings can be kept as private local profiles:

```powershell
damocline campaign profile save --file examples/campaign-profile-v1.json
damocline campaign profile list
damocline campaign profile show --name local-readonly
damocline campaign profile run --name local-readonly --dry-run
damocline campaign profile run --name local-readonly --authorized
damocline campaign profile verify --name local-readonly --campaign .damocline/reports/campaign.json --out verification.json
```

Profiles are stored under `.damocline/profiles/`; schema-less/v0 profiles are
migrated in memory and can be rewritten with `campaign profile migrate`. Updating
an existing profile requires `--replace`, and removal uses the explicit
`campaign profile delete` command. Profile execution requires fresh authorization
and any required write, insecure HTTP, long-run staging, or calibration opt-ins.
Application-metrics endpoints are also supplied per execution. Non-dry runs
write the full trial and phase report to a unique file under
`.damocline/reports/` unless `--out` is provided. Profiles capture settings, not
target state or credentials; environment variables and body data must be supplied
at run time. Do not commit local profiles or reports.

`campaign profile verify` performs an offline comparison of the saved profile's
workload and campaign controls against the phase configurations recorded in a
campaign report. It generates no traffic and can save a machine-readable result
without overwriting an existing file. A match is a consistency check, not a
signature: it does not authenticate either file, attest the host or application
revision, or verify credentials, target data state, and application-metrics
endpoint settings. Environment-backed body contents must be present to reproduce
the workload fingerprint.

Open a saved run or campaign without generating traffic:

```powershell
.\damocline.exe dashboard --file reports\campaign.json
```

Campaign views keep each trial and phase separate, show HTTP window outcomes,
and plot timestamped application-metric observations as points without
interpolation or causal interpretation. The local dashboard accepts only
loopback bind addresses.

The campaign drift screen is configurable with bounded `--stability-*` options
for minimum windows and samples, p95 increase, and throughput loss. Defaults
retain the existing coarse 3-window, 20-sample, 20%/5 ms p95, and 20% throughput
screen. Campaign schema v5 saves the effective policy and the offline `plan`
reader recomputes phase stability from it; schemas v1-v4 retain their historical
defaults. A screen pass means only that the selected large-drift limits were not
crossed. It does not prove stationarity or sustained capacity.

The command calculates the demand to validate and reports
**GOAL_MET_UNDER_TESTED_CONDITIONS** only when a complete, passing, supported-schema
repeated campaign exercised that exact base VU level and every required phase
has `VALID_WITHIN_ENVELOPE` data quality, adequate conditional p95/error
bounds for aggregates, steps and every SLO window, continuous window coverage,
and variability for every independent trial. Schema-v2 through v5 campaigns
retain phase classification and a coarse observation-window drift screen, then
require the exact saved schedule to reconcile. Schema v3 records successful
journey throughput spread, schema v4 adds the final bounded soak, and schema v5
persists the configurable drift policy. Missing or insufficient evidence
returns `INSUFFICIENT_EVIDENCE`; 50 error-free requests cannot substantiate a
1% error budget at 95% under the stated binomial assumptions. Ordinary external run reports remain `LIMITED` while workload-specific generator equivalence and controlled endpoint identity are unavailable. The local calibrator can qualify only its own fixed loopback soak; that does not certify application capacity. Existing statistical bounds assume independent, stationary
observations; that assumption is not established, so the result is not a
confidence-certified capacity claim. The command never makes a hardware
prediction, identifies a bottleneck, or recommends an infrastructure purchase.

## Terminal home screen

Run `damocline` or `.\\start.cmd` to open the **Eclipse** pixel-art splash (the Damocline sword crossing an eclipse-like horizon in Unicode block characters) and a numbered home menu. Select **New load test**, **Saved scenarios**, **Continue current**, **Help**, or **Exit** without remembering CLI flags. The interface uses optional ANSI colors and a no-color fallback for older terminals and redirected output. On a supported interactive Windows console, use **↑/↓ and Enter** (or number shortcuts) to navigate; redirected input and older terminals retain numbered input. The optional source update check now happens **before the splash and menu**, not after choosing a scenario.

This is a presentation layer only: it does not change authorization, workload configuration, authentication handling, or the native Go load engine. For scripting, use explicit commands such as `damocline run`, `damocline /start`, or `damocline /history` as before.

See [Terminal and guided CLI](docs/INTERACTIVE_START.md).

## Repeat a saved scenario

The guided CLI now supports English slash commands:

```powershell
damocline /start    # create a new scenario; keep the previous one in local history
damocline /history  # select a saved scenario by number and review it before rerun
damocline           # show the guided setup and current-profile choices
```

Scenario configurations are stored only in `.damocline/` alongside your source checkout. The current profile stays in `.damocline/scenario.local.json`, older distinct configurations are stored in `.damocline/history/`, and result reports remain in `.damocline/reports/`. These are scenario snapshots, not a report-history viewer. Do not commit private scenarios or reports. Browser authentication must be repeated for each rerun; traffic still requires explicit `AUTHORIZE` and a separate start confirmation.

See [Guided CLI](docs/INTERACTIVE_START.md) for selection, persistence, and security details.

## Run your first bounded test

```bash
# First run a local HTTP test server on port 18080 in a separate terminal.
python -m http.server 18080 --bind 127.0.0.1

# Validate without generating traffic.
go run ./cmd/damocline run --config examples/local-smoke.json --dry-run

# Explicitly authorize traffic to your local server and save the results.
go run ./cmd/damocline run --config examples/local-smoke.json --authorized --out reports/local.json
```

For Windows/PowerShell, session-cookie authentication, and safely testing your own application, see **[Quick start](docs/QUICKSTART.md)**. GitHub Actions tests the source code but no longer publishes downloadable release binaries.

**Initial safety ceilings:** up to 50 virtual users, 100 requests/second, and three minutes per run; these are upper bounds, not recommendations for production. The example starts with **2 VUs and 5 RPS**. HTTP redirects are not followed, so a 302 to a login page is reported as a failure by default.

## Advanced capacity exploration (experimental)

Damocline uses its **own native Go engine** for request scheduling and measurement.

```bash
# Copy and edit the example for a system you own or are authorized to test.
cp examples/advanced-readonly.json myapp.local.json

# Validation sends no traffic.
go run ./cmd/damocline explore --config myapp.local.json --min-users 2 --max-users 10 --repeats 2 --stage-duration 20s --dry-run

# Controlled, repeated closed-loop capacity experiments.
go run ./cmd/damocline explore --config myapp.local.json --min-users 2 --max-users 10 --repeats 2 --stage-duration 20s --budget 5m --authorized --out reports/capacity.json
```

In addition to fixed closed-loop load, `mode: "arrival"` runs independently scheduled journey starts and exposes dropped arrivals. For independent authenticated identities, configure `per_vu_cookie_env_prefix` and supply **distinct, locally managed** session cookies (never commit them).

Weighted workflow profiles are selected at each journey start in both closed-loop and open-arrival runs. Different workflow lengths or response times do not change the selected-start mix. A virtual user keeps its own cookie jar while moving between workflows; this does not provide a separate login identity per workflow. Saved schema-v26 reports reconcile selected workflow starts, worker-started journeys, and pre-worker drops.

Scenario files may use the versionless legacy `steps`/`profiles` fields or scenario version 1 with a named `workflows` list. Convert an existing file without replacing it with `damocline migrate-scenario --config old.json --out versioned.json`; the command refuses an existing destination. [`examples/versioned-weighted-workflows.json`](examples/versioned-weighted-workflows.json) shows the version 1 form. Scenario versioning is independent of the HTTP report schema.

To observe a Linux VPS during a test, run the separate native monitor **on the host**, then join samples by UTC timestamps:

```bash
damocline monitor --duration 10m --interval 2s --out reports/host.json
damocline correlate --load reports/capacity.json --host reports/host.json --out reports/correlated.json
```

The monitor does not require an exposed metrics port and does not identify a root cause by itself. See the **[advanced testing guide](docs/ADVANCED_TESTING.md)** for PowerShell, production safety, per-VU sessions, calibration caveats and scenarios. For open-arrival exploration, optional `--calibration` restricts the search to exact compatible local generator profiles and records the requested and effective rate bounds; this does not qualify the application target or sustained phases.

## Developer quick start

**Requires Go 1.24+.** `help` and `version` do not generate traffic; the `run` command does.

```bash
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
go test ./...
go run ./cmd/damocline help
go run ./cmd/damocline version
```

For safe local testing, real authenticated application testing, and Windows instructions, see the [MVP quick start](docs/QUICKSTART.md). Use `--dry-run` to validate first, then `--authorized` to actually generate bounded traffic.

**Optional local HTTP transport reference:** Run `go run ./cmd/damocline-calibrate --transport-matrix --out .damocline/calibration/transport.json` to verify bounded response-size, TLS, keep-alive, streaming, controlled delay and failure fixtures. The matrix never calls a customer application and does **not** certify the native generator scheduler or application capacity.

**Offline reference suitability check:** After running the separate-process calibrator, you can check how your requested workload differs from its *actual* measured envelope without sending a single request to your application:

```powershell
go run ./cmd/damocline-calibrate --check-scenario examples/weighted-profiles-readonly.json --calibration .damocline/calibration/reference.json
```

This intentionally produces `INCONCLUSIVE` for application-capacity readiness: the reference validates only exact synthetic loopback profiles and rates, not endpoint identity, your application's response distributions, TLS/network path, auth, or session mix. See [reference validity and coverage](docs/REFERENCE_VALIDATION.md) for exact evidence and limitations.

## Run the repeated G0 acceptance lab

The acceptance lab runs bounded, independent calibrator processes against fresh loopback reference servers, then runs the local HTTP transport fixture matrix. Acceptance manifest v5 checks 30 exact HTTP arrival shape/connection/rate profiles, three trusted HTTPS reuse profiles, and six closed-loop VU profiles at 1/2/5/10/20/50 users. It reconciles mode-specific client iterations and response sizes with independent server request/body/TCP counters, then checks the 30-second controlled soak, overload and HTTP-failure controls, and persisted quality recomputation. These short local generator profiles do not establish application capacity or longer campaign headroom. Two repeats are required; three are allowed for the baseline matrix. Each repeat has a 240-second default deadline, and every requested run is recorded.

Build both local commands first:

```powershell
New-Item -ItemType Directory -Force .damocline/local-validation | Out-Null
go build -o .damocline/local-validation/damocline-calibrate.exe ./cmd/damocline-calibrate
go build -o .damocline/local-validation/damocline-acceptance.exe ./cmd/damocline-acceptance
```

Then run a fresh evidence directory (it must not already exist):

```powershell
.\.damocline\local-validation\damocline-acceptance.exe --calibrator .\.damocline\local-validation\damocline-calibrate.exe --out-dir .damocline\acceptance\windows-run-01 --repeats 2
```

Linux commands use the same bounded workflow:

```bash
mkdir -p .damocline/local-validation
go build -o .damocline/local-validation/damocline-calibrate ./cmd/damocline-calibrate
go build -o .damocline/local-validation/damocline-acceptance ./cmd/damocline-acceptance
./.damocline/local-validation/damocline-acceptance --calibrator ./.damocline/local-validation/damocline-calibrate --out-dir .damocline/acceptance/linux-run-01 --repeats 2
```

An `--extended` run adds the exact 15/20/50/100 arrivals-per-second loopback stages and permits at most two independent repeats. The lab returns nonzero when any required criterion fails or evidence is incomplete. It preserves a state manifest after each transition, each produced calibration and transport JSON artifact, and bounded stdout/stderr logs (up to 1 MiB per stream per process) in the requested directory. The directory is local and gitignored; use a new name for each attempt. The final manifest reports exact gate expectations and observations, artifact hashes, generator/runtime identity, and per-profile p95 spread. That spread is descriptive only, not a confidence interval or a causal comparison. A passing result qualifies only the recorded local fixture, binary, runtime, and windows; it does not certify an external application or a universal generator ceiling.

## Principles

1. **Evidence before estimates.** Capacity is always tied to a specific workload, duration, environment, and acceptance criteria.
2. **Correct measurement.** Report missed arrivals and generator bottlenecks rather than obscuring them.
3. **Safety first.** Authorized targets only; strict traffic bounds, no destructive defaults, and no automatic production stress tests.
4. **Independent engine.** Go owns load scheduling, execution, and metrics; external tools may serve as validation references.
5. **Local-first and extensible.** Core functionality must not depend on a hosted service.
6. **Honest diagnostics.** Distinguish observed measurements from correlated signals and hypotheses.

## Documentation

- [Offline campaign evidence audit (G0/G2/G3)](docs/CAMPAIGN_AUDIT.md) — replay saved campaign stages, measurement quality and per-workflow drift without generating traffic.

- [Architecture](docs/ARCHITECTURE.md)
- [Roadmap](docs/ROADMAP.md)
- [Measurement model](docs/METRICS.md)
- [Scientific foundations and validation contract](docs/SCIENTIFIC_FOUNDATIONS.md)
- [Local reference validation](docs/REFERENCE_VALIDATION.md)
- [MVP quick start](docs/QUICKSTART.md)
- [Source updates](docs/UPDATES.md)
- [Decision records](docs/adr/0001-go-core.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Agent guidelines](AGENTS.md)
- [Changelog](CHANGELOG.md)

## Contributing

Damocline is at the foundation stage. Design feedback, reproducibility research, documentation, and focused contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and discuss major changes in an issue before coding them.

## License

Licensed under the [Apache License 2.0](LICENSE).
