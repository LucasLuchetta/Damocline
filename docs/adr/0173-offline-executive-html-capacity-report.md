# ADR 0173: Offline executive HTML capacity report

## Status

Accepted.

## Context

The Markdown capacity report (ADR 0165) is complete but written for
specialists. Decision makers need to see, without interpreting tables, what
demand was requested, which level (if any) was approved, how latency,
throughput and errors behaved as load increased, and what to do next. The
report must stay scientifically honest: an unvalidated number must never
look like a capacity claim, and an INSUFFICIENT_EVIDENCE state must be as
visible as a passing one.

## Decision

`damocline audit` gains `--html-out FILE.html` for a single campaign. It
renders a self-contained HTML page from the same offline assessment used
for `--out` and `--report-out`; no traffic is generated. Optional inputs:

- `--explore-file` adds the saved exploration probes of the same scenario
  and target to the charts (`--expect-explore-file-sha256` pins its bytes);
- `--actions-per-user-minute` shows a declared VU-to-people conversion,
  always labeled as an assumption and never part of the decision.

The page shows, in order: a decision banner, requested demand versus the
highest approved level in modeled active users, a plain-language
conclusion, the next decision, inline SVG charts (request p95 against the
objective, successful requests/s, error rate against the maximum) with a
data table, then separately styled MEASURED FACT, HYPOTHESIS and UNKNOWN
blocks, evidence quality (calibration, network baseline, generator CPU,
drift, repeats), decision blockers with remediation, limitations, and the
exact replay command with evidence digests.

A level is shown as approved only when the assessment state is
`GOAL_MET_UNDER_TESTED_CONDITIONS`. A measured breach at or below the
requested demand is shown as DEMAND NOT MET. Every other state is shown as
INSUFFICIENT EVIDENCE or INVALID EVIDENCE with "no number in this report is
an approved capacity". Exploration probes are labeled exploratory.

The page has no scripts and no external resources, and declares a
restrictive Content-Security-Policy. It uses semantic landmarks, captioned
tables, SVG titles and descriptions, light and dark color schemes and print
styles. Output is deterministic for identical inputs. It is created with
`O_EXCL` and mode 0600, never overwriting an existing file, within the
existing 1 MiB report limit.

`damocline capacity` writes `capacity.html` next to `capacity.md` by default,
including its exploration and, when declared, the usage intensity.

## Consequences

- Multi-campaign collections are not yet rendered as HTML; `--html-out` with
  several `--campaign` flags is refused.
- Charts plot request p95 per phase or probe; journey and per-workflow
  latency stay in the Markdown report.
- Resumed capacity directories completed before this change keep only their
  Markdown report.
