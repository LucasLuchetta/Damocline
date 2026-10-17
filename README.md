<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/damocline-logo-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="assets/brand/damocline-logo-light.png" />
    <img src="assets/brand/damocline-logo-light.png" alt="Damocline — Find the limit before it finds you." width="380" />
  </picture>
</p>

[![CI](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml/badge.svg)](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml)
[![Apache License 2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Status: pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange)](docs/ROADMAP.md)

**Damocline** is an open-source, local-first application capacity explorer. It aims to help developers discover how many active users can complete a given workflow within a performance budget, identify where capacity becomes constrained, and understand what the available infrastructure metrics actually show.

The name evokes the **Sword of Damocles**: apparently stable systems can have limits hanging overhead. Damocline aims to find those limits *before users do*.

> [!IMPORTANT]
> **Experimental advanced HTTP engine:** native closed-loop and arrival-rate scheduling, repeated virtual-user capacity exploration, independent cookie sessions, Linux CPU/RAM sampling, and UTC report correlation are available. Manual temporary browser login is experimental. Automatic login replay, full-browser load tests, per-container telemetry, and rigorous generator-capacity calibration are not implemented. A passing test is **not** a universal maximum-users claim.

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

Browser login session reuse **does not replay login forms**, create distinct user accounts, or measure browser rendering. The load engine remains Damocline's own Go implementation, not k6.

See [Source installation and browser login](docs/SOURCE_INSTALL.md) and [Guided CLI](docs/INTERACTIVE_START.md).

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

Damocline has its **own native Go engine**. It does not invoke k6 or external load-testing engines.

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
- [MVP quick start](docs/QUICKSTART.md)
- [Decision records](docs/adr/0001-go-core.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Agent guidelines](AGENTS.md)
- [Changelog](CHANGELOG.md)

## Contributing

Damocline is at the foundation stage. Design feedback, reproducibility research, documentation, and focused contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and discuss major changes in an issue before coding them.

## License

Licensed under the [Apache License 2.0](LICENSE).
