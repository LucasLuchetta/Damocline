# ADR 0002 — Local scenario history and slash commands

- **Date:** 2026-10-17
- **Status:** Proposed

## Context

The original guided CLI has a single mutable active scenario at `.damocline/scenario.local.json`.
Creating another scenario replaces those settings, requiring the user to re-enter a
previous target, workload and page list to repeat a benchmark. Benchmark reports
are stored separately and are not sufficient to reconstruct authentication settings.

## Decision

Add English command-line aliases `/start` and `/history`:

- `/start` always runs the guided configuration for a new scenario.
- `/history` lists saved configurations, allows numeric selection and then
  continues through the existing credential acquisition, review, and explicit
  traffic authorization flow.
- The original `start` command and unprefixed guided wizard remain compatible.
- Keep the current profile at its existing paths for backward compatibility.
  Before replacing its contents, archive it in `.damocline/history/` unless
  an identical snapshot already exists.
- Store only local JSON configuration. Session cookies and passwords must remain
  environment references or be obtained by a fresh temporary browser login.
  Do not archive a profile containing literal HTTP header values.
- Saving and selecting scenarios never sends HTTP load traffic.
  The existing explicit `AUTHORIZE` and separate start confirmation are mandatory
  on every replay.

## Consequences and limitations

History is scoped to the working directory used by the CLI launcher and is not a
global index of all scenarios or reports. The history directory may include
private application paths: it remains gitignored and should not be shared.
Manually moving, deleting or editing history files is outside the normal flow.
History snapshots preserve configurations, **not** latency results; report
comparison is separate future work.

Scenario switching currently activates the selected profile immediately, even
when the user cancels the subsequent run. This is a local configuration change,
not authorization to send traffic. Snapshot writes are not a transactional store.
