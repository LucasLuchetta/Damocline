# Agent guidelines — Damocline

This is the canonical engineering instructions file for human contributors and coding assistants.

## Project mission

Build a local-first, open-source system to measure sustainable application capacity for defined workflows and correlate potential bottlenecks with actual evidence.

## Rules

- Keep code, documentation, comments, commits, issues, and PRs in **English**.
- Read `README.md`, `docs/ARCHITECTURE.md`, `docs/METRICS.md`, and relevant ADRs before major changes.
- This is pre-alpha. Never imply planned testing or monitoring features are implemented.
- Prefer Go's standard library and focused, testable packages.
- Public commands live in `cmd/`; internal implementation lives in `internal/`.
- Never treat virtual users, connected sessions, requests in flight, and requests per second as synonyms.
- Report sustainable capacity only with scenario, environment, pass criteria, observation window, and uncertainty.
- Separate measured facts from hypotheses about root causes.
- Verify generator health, dropped starts, and scheduling accuracy. Avoid coordinated omission.
- Never run unbounded or destructive tests, scan arbitrary hosts, or leak target credentials.
- Source updates must not overwrite local edits, reset branches or automatically update non-interactive benchmark commands. Only safe fast-forward updates from an explicit Git remote may run after opt-in.
- Never embed real user-specific or customer application domains, production IP addresses, internal paths, test identities, or infrastructure details in public tests, documentation, commits, issues, or PR descriptions. Use reserved documentation domains (for example `app.example.com`), loopback addresses, and synthetic fixtures. Keep private scenarios and reports local and gitignored.
- Do not copy implementation code from other tools without license review and attribution.
- For PR validation, run the relevant checks locally and do not wait for GitHub-hosted CI unless the user explicitly asks for hosted checks.

## Before opening a PR

1. Format code with `gofmt`.
2. Run `go test ./...`, `go vet ./...`, and `go build ./cmd/damocline`.
3. Add deterministic unit tests for changed behavior. Use local test servers, never third-party targets.
4. Update docs and `CHANGELOG.md` for user-visible changes.
5. Record architectural decisions in `docs/adr/`.
6. Explain validation, safety impact, and known limitations.

Start with a correct bounded HTTP engine, not a web dashboard.
