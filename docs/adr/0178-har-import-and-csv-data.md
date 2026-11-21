# ADR 0178: HAR scenario drafts and CSV journey data

## Status

Accepted.

## Context

Writing a realistic journey by hand is the main onboarding barrier. Browsers
already record real journeys as HAR files, but a HAR also contains cookies,
bearer tokens, CSRF headers, request bodies, static assets and third-party
calls. The earlier `import-har` wrote only a read-only list of methods and
mostly blank paths, which still left the scenario to be written by hand.

Real journeys also need varied input: a fixed account or search term per
journey hits caches and shared rows, so it misrepresents load. Master already
supports weighted named workflows (`workflows[].weight`), extracted response
variables (`{{name}}`) and generated synthetic identifiers
(`{{synthetic_id}}`), but no operator-supplied input data.

## Decision

### `damocline import-har`

`damocline import-har --har file.har --base-url https://app.example.com --out
scenario.json` writes a complete `scenario_version` 1 draft with one
`imported` workflow. It sends no traffic. The old `--file` and `--origin`
flags remain aliases; `--keep-path` is removed because paths are now kept by
default after redaction.

- Only requests whose scheme and host equal `--base-url` are converted.
- Static assets (by extension or response MIME type) are dropped unless
  `--keep-static` is given.
- POST, PUT, PATCH and DELETE are dropped unless `--include-writes` is
  given. Running a draft with write steps still requires `--allow-writes`.
  Other methods are dropped.
- `Cookie`, `Authorization`, `Proxy-Authorization` and token, session, CSRF
  or API-key headers become `${DAMOCLINE_...}` environment placeholders.
  Only `Accept`, `Content-Type` and `X-Requested-With` are copied literally.
- Request bodies are never written. A write step with a body receives
  `body_env: DAMOCLINE_BODY_NN`, which the operator fills locally.
- Query parameters with sensitive names or token-like values are removed and
  their names listed. Requests whose path contains a token-like or sensitive
  segment are dropped entirely.
- Steps are named `pPP-SS-method-slug` from the HAR page and order, keep the
  recorded status as `expected_status`, and are capped at the scenario-wide
  20-step limit.
- The command prints every dropped request with its reason (cross-origin
  requests show only the host; no query values are printed), the removed
  query parameter names, the required placeholders and the write steps. The
  output is created with mode 0600 and never overwritten.

The draft uses conservative defaults (1 user, 30s, 5 rps ceiling) and is a
starting point for review, not a validated workload.

### `data`: CSV input per journey

A scenario may declare `"data": {"file": "rows.csv", "mode": "sequential"}`.
A relative file resolves against the scenario file directory when the scenario is loaded from disk (otherwise against the working directory). Steps refer to
columns with `${data.column}` in paths (complete segments or query values,
like other variables), header values, and `body_env` content. At validation
time the CSV must have a header of unique `[a-z][a-z0-9_]{0,58}` names, at
least one row, at most 100000 rows, 50 columns and 4 MiB, and no value over
4096 bytes or with a line break. Every placeholder must name an existing
column; placeholders without a data source are rejected.

Modes:

- `sequential` (default): a run-wide journey counter walks the rows and
  wraps around.
- `random`: the row is a deterministic function of `workload_seed` and the
  journey counter. `workload_seed` defaults to 1 and is allowed for this mode
  without variable think time.
- `unique_per_vu`: virtual user N always uses row N; the file must have at
  least `users` rows.

Internally `${data.column}` is rewritten to the `{{data_column}}` variable,
so extraction cannot redefine it. The workload fingerprint records the mode,
the CSV SHA-256 and, for random mode, the seed. Row values never appear in
reports, which keep the template paths.

## Consequences

- Onboarding from a browser capture produces a runnable read-only draft
  without leaking credentials, but the operator must still review paths,
  statuses, think time and pass criteria and supply placeholders.
- With concurrency, `sequential` order across virtual users depends on
  scheduling; the set of rows used is bounded but the exact interleaving is
  not reproducible.
- CSV data is read into memory once per validation and is limited to 4 MiB.
- A data placeholder inside a path must occupy a complete segment.
