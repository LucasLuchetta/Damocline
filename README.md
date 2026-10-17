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
> **Experimental HTTP MVP:** bounded, closed-loop HTTP load testing now works in the CLI. Automatic capacity discovery, open-loop traffic, full-browser journeys, and infrastructure collectors are **not implemented**. Never interpret a single passing run as the maximum number of users your server supports.

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
| Native Go load engine | Bounded HTTP requests with closed-loop VUs | **MVP implemented** |
| Workload models | Closed-loop VUs supported; independent arrivals planned | **Partial** |
| Capacity explorer | Automatically bracket, refine, and confirm passing load levels | Planned |
| Per-workflow analysis | Evaluate pages, journeys, and concurrent workload mixes | Planned |
| Infrastructure adapters | Optional host, Docker, database, and cloud telemetry | Planned |
| Reporting | p50/p95/p99, error reasons, per-step results, JSON export; infrastructure correlation planned | **Partial** |
| Local-first CLI | Work without requiring a cloud account | Scaffold only |

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

## Run your first bounded test

```bash
# First run a local HTTP test server on port 18080 in a separate terminal.
python -m http.server 18080 --bind 127.0.0.1

# Validate without generating traffic.
go run ./cmd/damocline run --config examples/local-smoke.json --dry-run

# Explicitly authorize traffic to your local server and save the results.
go run ./cmd/damocline run --config examples/local-smoke.json --authorized --out reports/local.json
```

For Windows/PowerShell, session-cookie authentication, and safely testing your own application, see **[Quick start](docs/QUICKSTART.md)**. Experimental Windows and Linux binaries are attached to successful CI runs as downloadable workflow artifacts (not signed releases).

**Initial safety ceilings:** up to 50 virtual users, 100 requests/second, and three minutes per run; these are upper bounds, not recommendations for production. The example starts with **2 VUs and 5 RPS**. HTTP redirects are not followed, so a 302 to a login page is reported as a failure by default.

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
