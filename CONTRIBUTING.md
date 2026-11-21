# Contributing to Damocline

Damocline is an **open-source, maintainer-governed** project. We welcome
reproducible bug reports, measurement research, useful documentation and
focused improvements. A contribution is a proposal, not permission to modify
the upstream project. Read [GOVERNANCE.md](GOVERNANCE.md) for decision rights
and review policy.

## Before starting

1. Read the [README](README.md), [architecture](docs/ARCHITECTURE.md),
   [measurement definitions](docs/METRICS.md) and
   [roadmap](docs/ROADMAP.md), including applicable ADRs.
2. Search existing issues and PRs. For major changes, open a feature proposal
   **before** implementing it and wait for the maintainer to agree on the
   scope and acceptance evidence.
3. Narrow documentation corrections and clearly reproducible bug fixes can
   be submitted directly as focused PRs. An issue is helpful for changes
   that affect behavior, metrics or compatibility.
4. Keep public issues, PR descriptions, code comments, documentation and
   commit messages in **English**.

An issue, public roadmap item or CI success does not promise a merge.
Please do not submit unsolicited large rewrites, unrelated feature bundles,
generated bulk PRs, or changes that circumvent a review decision.

## Local development

Install Go 1.24 or newer:

```bash
gofmt -w path/to/changed.go
go test ./...
go vet ./...
go build ./cmd/damocline
go run ./cmd/damocline help
```

Run the relevant deterministic fixtures when changing measurements or
scheduling. Tests involving HTTP must use controlled local test servers
and synthetic data, never arbitrary third-party hosts. Some integration
fixtures require specific host resources and are described separately in
the repository.

## Pull request requirements

- Branch from an updated `master` and keep each PR focused.
- Link the bug report or accepted proposal when applicable.
- Describe the problem, approach and user-visible behavior.
- Include tests for correctness, failure cases and compatibility where
  relevant; state what you ran and what you did **not** verify.
- Explain effects on authorization, traffic bounds, performance claims,
  credentials, privacy, telemetry and data quality.
- Update `CHANGELOG.md`, user docs and schema/version documentation when
  externally visible behavior changes. Use an ADR under `docs/adr/` for
  significant design decisions.
- Do not silently change the meaning of published metrics or report
  evidence. Explain any changed assumptions and replay compatibility.
- If an assistant generated or substantially modified code, review every
  change personally, verify its license/provenance and take responsibility
  for the submission.

A maintainer reviews and decides whether to merge. Automated checks and
CODEOWNERS requests do not confer merge rights.

## Responsible testing and data protection

Only run load tests against explicitly authorized targets, within bounded
limits. Do not publish real customer domains, internal routes or IPs,
test identities, private scenarios, benchmarks, tokens, cookies,
production data or logs containing credentials. Public examples must use
reserved documentation domains (such as `app.example.com`) and loopback
addresses with synthetic fixtures.

All write-oriented tests must be deliberately authorized and reversible
where practical. Do not add unsafe traffic defaults, target discovery,
security bypasses, or claims that VUs universally equal real people.

## Licensing and vulnerability reports

Contributions are accepted under the repository's
[Apache License 2.0](LICENSE); preserve required third-party notices.
Do not copy code from other projects without an appropriate license
review and attribution. Report vulnerabilities privately as described
in [SECURITY.md](SECURITY.md).
