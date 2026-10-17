# Contributing

Thanks for helping build Damocline. We welcome documentation fixes, technical discussion, reproducible tests, and focused code contributions.

## Getting started

Read the [README](README.md), [architecture](docs/ARCHITECTURE.md), [metrics](docs/METRICS.md), and [roadmap](docs/ROADMAP.md). Search existing issues and PRs before starting substantial work. Discuss major features in an issue first.

All public-facing project communication, documentation, code comments, and commit messages should be in **English**.

## Local development

Install Go 1.24 or newer:

```bash
go test ./...
go vet ./...
go build ./cmd/damocline
go run ./cmd/damocline help
```

Format with `gofmt`. Tests involving HTTP must use controlled local test servers and fixtures, never arbitrary third-party hosts.

## Pull request checklist

1. Use a descriptive branch and a focused change.
2. Explain why the change matters to users.
3. Add meaningful tests for new behavior.
4. Document any new dependency, API, performance claim, or safety implication.
5. Update `CHANGELOG.md` for externally visible behavior.
6. Add an ADR under `docs/adr/` for important architectural changes.
7. Explain tests performed, limitations, and unverified assumptions.

Scheduling/metrics changes may require dedicated benchmark and failure-injection evidence.

## Responsible use and data protection

Only run authorized tests. Do not commit tokens, production data, logs containing credentials, personal information, or proprietary fixtures. All write-oriented scenarios must be explicit and reversible where practical.

## Licensing

Contributions are accepted under the [Apache License 2.0](LICENSE). Include legally required third-party attribution when applicable. See [SECURITY.md](SECURITY.md) for private vulnerability reports.
