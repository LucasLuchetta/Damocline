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

It distinguishes **meeting your p95/error objectives** from **proving user capacity** and **having evidence that a bigger server is needed**. Achieved RPS is not the configured `max_rps` ceiling; HTTP request latency is not complete journey duration. A single PASS cannot certify infrastructure headroom. Controlled comparisons require matching schema-v11 workload fingerprints and declared run conditions; older or mismatched reports are labeled `NOT_COMPARABLE`.

See [Understand your results](docs/REPORT_EXPLAINER.md) for interpretation, sample-size and comparison warnings. The command never sends traffic.

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
damocline plan --campaign reports/campaign.json --peak-users 200 --growth-percent 30 --reserve-percent 30
```

The command calculates the demand to validate and reports
**GOAL_MET_UNDER_TESTED_CONDITIONS** only when a complete, passing, schema-v1
repeated campaign exercised that exact base VU level and every required phase
has `VALID_WITHIN_ENVELOPE` data quality, adequate conditional p95/error
bounds for aggregates, steps and every SLO window, continuous window coverage,
and variability for every independent trial. Missing or insufficient evidence
returns `INSUFFICIENT_EVIDENCE`; 50 error-free requests cannot substantiate a
1% error budget at 95% under the stated binomial assumptions. Current engine
reports remain `LIMITED` because workload-specific generator equivalence is
not implemented. Existing statistical bounds assume independent, stationary
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

To observe a Linux VPS during a test, run the separate native monitor **on the host**, then join samples by UTC timestamps:

```bash
damocline monitor --duration 10m --interval 2s --out reports/host.json
damocline correlate --load reports/capacity.json --host reports/host.json --out reports/correlated.json
```

The monitor does not require an exposed metrics port and does not identify a root cause by itself. See the **[advanced testing guide](docs/ADVANCED_TESTING.md)** for PowerShell, production safety, per-VU sessions, calibration caveats and scenarios.

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

This intentionally produces `INCONCLUSIVE` for application-capacity readiness: the reference validates only specific loopback rates and one 4 KiB GET, not your application's response sizes, TLS/network path, auth or session mix. See [reference validity and coverage](docs/REFERENCE_VALIDATION.md) for exact evidence and limitations.

## Principles

1. **Evidence before estimates.** Capacity is always tied to a specific workload, duration, environment, and acceptance criteria.
2. **Correct measurement.** Report missed arrivals and generator bottlenecks rather than obscuring them.
3. **Safety first.** Authorized targets only; strict traffic bounds, no destructive defaults, and no automatic production stress tests.
4. **Independent engine.** Go owns load scheduling, execution, and metrics; external tools may serve as validation references.
5. **Local-first and extensible.** Core functionality must not depend on a hosted service.
6. **Honest diagnostics.** Distinguish observed measurements from correlated signals and hypotheses.

## Documentation

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
